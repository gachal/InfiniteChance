package apikey

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newSQLiteStore opens a throwaway desktop-shaped store.
func newSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewSQLiteStore(db)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

// 计费核心语义(与 MySQL 套件互为镜像):预扣的条件更新不超扣、
// 吊销即刻拒付、充值与流水原子。
func TestSQLiteReserveAndGuards(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	k, err := s.Create(ctx, Key{
		Name:        "billing",
		Prefix:      "sk-pref",
		KeyHash:     Hash("sk-full"),
		QuotaMicros: 1_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	balance, err := s.Reserve(ctx, k.ID, 300, ReasonEstimate)
	if err != nil {
		t.Fatalf("Reserve 300: %v", err)
	}
	if balance != 700 {
		t.Fatalf("balance after reserve = %d, want 700", balance)
	}

	if _, err := s.Reserve(ctx, k.ID, 800, ReasonEstimate); !errors.Is(err, ErrInsufficientQuota) {
		t.Fatalf("oversized reserve err = %v, want ErrInsufficientQuota", err)
	}

	if _, err := s.Reserve(ctx, 999, 1, ReasonEstimate); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("unknown key reserve err = %v, want ErrKeyNotFound", err)
	}

	revoked, err := s.Revoke(ctx, k.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Status(time.Now()) != StatusRevoked {
		t.Fatalf("status after revoke = %q", revoked.Status(time.Now()))
	}
	if _, err := s.Reserve(ctx, k.ID, 1, ReasonEstimate); !errors.Is(err, ErrKeyNotActive) {
		t.Fatalf("revoked reserve err = %v, want ErrKeyNotActive", err)
	}
	if _, err := s.TopUp(ctx, k.ID, 100, ReasonManualTopUp); !errors.Is(err, ErrKeyNotActive) {
		t.Fatalf("revoked topup err = %v, want ErrKeyNotActive", err)
	}
}

func TestSQLiteTopUpAndLedger(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	k, err := s.Create(ctx, Key{
		Name:        "ledger",
		Prefix:      "sk-pref",
		KeyHash:     Hash("sk-full-2"),
		QuotaMicros: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, k.ID, 200, ReasonEstimate); err != nil {
		t.Fatal(err)
	}
	updated, err := s.TopUp(ctx, k.ID, 1_000, ReasonManualTopUp)
	if err != nil {
		t.Fatal(err)
	}
	if updated.QuotaMicros != 1_300 { // 500 - 200(预扣) + 1_000
		t.Fatalf("quota after topup = %d, want 1300", updated.QuotaMicros)
	}

	log, err := s.QuotaLog(ctx, k.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 3 { // initial + estimate + manual_topup
		t.Fatalf("ledger entries = %d, want 3", len(log))
	}
}
