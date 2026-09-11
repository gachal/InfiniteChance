package settings

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

func TestSQLiteGetMissingRow(t *testing.T) {
	s := newSQLiteStore(t)
	if _, err := s.Get(context.Background(), NameStorage); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing row err = %v, want ErrNotFound", err)
	}
}

func TestSQLitePutUpsertsAndRoundTrips(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	row, err := s.Put(ctx, NameStorage, []byte(`{"driver":"local"}`))
	if err != nil {
		t.Fatal(err)
	}
	first := row.UpdatedAt
	if first.IsZero() {
		t.Fatal("Put should stamp updated_at")
	}

	got, err := s.Get(ctx, NameStorage)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Value) != `{"driver":"local"}` {
		t.Fatalf("value = %s", got.Value)
	}
	if !got.UpdatedAt.Equal(first) {
		t.Fatalf("updated_at drifted: %v vs %v", got.UpdatedAt, first)
	}

	// 第二次写同名行 = upsert:值替换、时间戳前进(定宽 TEXT 可比较)。
	time.Sleep(2 * time.Millisecond)
	row, err = s.Put(ctx, NameStorage, []byte(`{"driver":"oss"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !row.UpdatedAt.After(first) {
		t.Fatalf("updated_at = %v, want after %v", row.UpdatedAt, first)
	}
	got, err = s.Get(ctx, NameStorage)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Value) != `{"driver":"oss"}` {
		t.Fatalf("upserted value = %s", got.Value)
	}
}

// 读取器直连真库:缺行 → local 缺省,写行后即时读到新值(按请求读表、
// 即时生效的画布侧语义)。
func TestSQLiteStorageReaderSeesUpdatesImmediately(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()
	read := NewStorageReader(s)

	if cfg, err := read(ctx); err != nil || cfg.Driver != DriverLocal {
		t.Fatalf("before put: cfg %+v err %v", cfg, err)
	}
	if _, err := s.Put(ctx, NameStorage,
		[]byte(`{"driver":"oss","oss":{"endpoint":"e","bucket":"b","access_key":"AK","secret_key":"SK"}}`)); err != nil {
		t.Fatal(err)
	}
	cfg, err := read(ctx)
	if err != nil || cfg.EffectiveDriver() != DriverOSS {
		t.Fatalf("after put: cfg %+v err %v, want oss", cfg, err)
	}
}
