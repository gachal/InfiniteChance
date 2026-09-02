package apikey_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
)

// fakeStore is the in-memory Store double; handler and middleware tests run
// at the HTTP seam without MySQL.
type fakeStore struct {
	keys    map[int64]apikey.Key
	ledger  map[int64][]apikey.QuotaEntry // key id → append-only entries
	nextID  int64
	nextLog int64
	broken  bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		keys:   map[int64]apikey.Key{},
		ledger: map[int64][]apikey.QuotaEntry{},
	}
}

func (f *fakeStore) appendLog(keyID, delta, balance int64, reason string) {
	f.nextLog++
	f.ledger[keyID] = append(f.ledger[keyID], apikey.QuotaEntry{
		ID: f.nextLog, DeltaMicros: delta, BalanceMicros: balance, Reason: reason,
	})
}

func (f *fakeStore) Create(_ context.Context, k apikey.Key) (apikey.Key, error) {
	if f.broken {
		return apikey.Key{}, errStoreBroken
	}
	f.nextID++
	k.ID = f.nextID
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	k.CreatedAt, k.UpdatedAt = now, now
	f.keys[k.ID] = k
	if k.QuotaMicros > 0 {
		f.appendLog(k.ID, k.QuotaMicros, k.QuotaMicros, apikey.ReasonInitial)
	}
	return k, nil
}

func (f *fakeStore) List(context.Context) ([]apikey.Key, error) {
	if f.broken {
		return nil, errStoreBroken
	}
	out := make([]apikey.Key, 0, len(f.keys))
	for _, k := range f.keys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// ByID returns the key or ErrKeyNotFound.
func (f *fakeStore) ByID(_ context.Context, id int64) (apikey.Key, error) {
	k, ok := f.keys[id]
	if !ok {
		return apikey.Key{}, apikey.ErrKeyNotFound
	}
	return k, nil
}

func (f *fakeStore) ByHash(_ context.Context, hash string) (apikey.Key, error) {
	for _, k := range f.keys {
		if k.KeyHash == hash {
			return k, nil
		}
	}
	return apikey.Key{}, apikey.ErrKeyNotFound
}

func (f *fakeStore) Revoke(_ context.Context, id int64, at time.Time) (apikey.Key, error) {
	k, ok := f.keys[id]
	if !ok {
		return apikey.Key{}, apikey.ErrKeyNotFound
	}
	if k.RevokedAt == nil {
		k.RevokedAt = &at
		k.UpdatedAt = at
		f.keys[id] = k
	}
	return k, nil
}

func (f *fakeStore) TopUp(_ context.Context, id, delta int64, reason string) (apikey.Key, error) {
	k, ok := f.keys[id]
	if !ok {
		return apikey.Key{}, apikey.ErrKeyNotFound
	}
	// 与 MySQL 实现同语义:活跃守卫与加钱一体,拒绝死 key。
	if k.Status(time.Now()) != apikey.StatusActive {
		return apikey.Key{}, apikey.ErrKeyNotActive
	}
	k.QuotaMicros += delta
	f.keys[id] = k
	f.appendLog(id, delta, k.QuotaMicros, reason)
	return k, nil
}

func (f *fakeStore) Reserve(_ context.Context, id, amount int64, reason string) (int64, error) {
	k, ok := f.keys[id]
	if !ok {
		return 0, apikey.ErrKeyNotFound
	}
	if k.Status(time.Now()) != apikey.StatusActive {
		return 0, apikey.ErrKeyNotActive
	}
	if k.QuotaMicros < amount {
		return 0, apikey.ErrInsufficientQuota
	}
	k.QuotaMicros -= amount
	f.keys[id] = k
	f.appendLog(id, -amount, k.QuotaMicros, reason)
	return k.QuotaMicros, nil
}

func (f *fakeStore) Adjust(_ context.Context, id, delta int64, reason string) (int64, error) {
	k, ok := f.keys[id]
	if !ok {
		return 0, apikey.ErrKeyNotFound
	}
	if delta == 0 {
		return k.QuotaMicros, nil
	}
	k.QuotaMicros += delta
	f.keys[id] = k
	f.appendLog(id, delta, k.QuotaMicros, reason)
	return k.QuotaMicros, nil
}

func (f *fakeStore) QuotaLog(_ context.Context, keyID int64, limit int) ([]apikey.QuotaEntry, error) {
	entries := append([]apikey.QuotaEntry(nil), f.ledger[keyID]...)
	// newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

var errStoreBroken = errors.New("store broken")

func newKeyServer(store apikey.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/admin")
	apikey.RegisterAdminRoutes(group, &apikey.Handlers{Store: store})
	return r
}

func doJSON(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the standard error shape: %v\nbody: %s", err, w.Body.String())
	}
	if body.Error.Code == "" {
		t.Fatalf("error body missing code: %s", w.Body.String())
	}
	return body
}

type keyBody struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	QuotaUSD  float64    `json:"quota_usd"`
	Status    string     `json:"status"`
	Key       string     `json:"key"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

// seedKey inserts a key directly, bypassing Generate, so tests control the
// hash/status exactly.
func seedKey(t *testing.T, store *fakeStore, name string, mut func(*apikey.Key)) apikey.Key {
	t.Helper()
	k := apikey.Key{
		Name:    name,
		Prefix:  "sk-seeded00",
		KeyHash: apikey.Hash("sk-seeded-full-" + name),
	}
	if mut != nil {
		mut(&k)
	}
	created, err := store.Create(t.Context(), k)
	if err != nil {
		t.Fatalf("seed key: %v", err)
	}
	return created
}

func TestCreateKeyReturnsFullValueExactlyOnce(t *testing.T) {
	store := newFakeStore()
	r := newKeyServer(store)

	w := doJSON(r, http.MethodPost, "/admin/keys", map[string]any{
		"name":              "canvas-service",
		"initial_quota_usd": 10,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var created keyBody
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if !strings.HasPrefix(created.Key, "sk-") || len(created.Key) != 43 {
		t.Errorf("full key = %q, want a 43-char sk- value", created.Key)
	}
	if created.Prefix != created.Key[:11] {
		t.Errorf("prefix = %q, want the key's leading slice %q", created.Prefix, created.Key[:11])
	}
	if created.QuotaUSD != 10 {
		t.Errorf("quota_usd = %v, want 10", created.QuotaUSD)
	}
	if created.Status != apikey.StatusActive {
		t.Errorf("status = %q, want active", created.Status)
	}

	// 存库只有哈希,原文不落库。
	if store.keys[created.ID].KeyHash == created.Key {
		t.Fatal("full key value was stored as its own hash")
	}
	if store.keys[created.ID].KeyHash != apikey.Hash(created.Key) {
		t.Error("stored hash does not verify the issued key")
	}

	// 列表只露前缀,且不再出现完整值。
	w = doJSON(r, http.MethodGet, "/admin/keys", nil)
	if strings.Contains(w.Body.String(), created.Key) {
		t.Fatal("list response leaked the full key value")
	}
	var list struct {
		Keys []keyBody `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("bad list JSON: %v", err)
	}
	if len(list.Keys) != 1 {
		t.Fatalf("list = %d keys, want 1", len(list.Keys))
	}
	if list.Keys[0].Prefix != created.Prefix || list.Keys[0].Key != "" {
		t.Errorf("list key = %+v, want prefix only", list.Keys[0])
	}
}

func TestCreateKeyValidatesInput(t *testing.T) {
	r := newKeyServer(newFakeStore())

	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty name", map[string]any{"name": "   "}},
		{"past expiry", map[string]any{"name": "x", "expires_at": "2020-01-01T00:00:00Z"}},
		{"zero quota", map[string]any{"name": "x", "initial_quota_usd": 0}},
		{"negative quota", map[string]any{"name": "x", "initial_quota_usd": -5}},
		{"over cap quota", map[string]any{"name": "x", "initial_quota_usd": 2_000_000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(r, http.MethodPost, "/admin/keys", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			if got := decodeError(t, w).Error.Code; got != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", got)
			}
		})
	}
}

func TestCreateKeyWithExpiry(t *testing.T) {
	r := newKeyServer(newFakeStore())

	w := doJSON(r, http.MethodPost, "/admin/keys", map[string]any{
		"name":       "expiring",
		"expires_at": "2030-01-01T00:00:00Z",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var created keyBody
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if created.ExpiresAt == nil || created.ExpiresAt.UTC().Year() != 2030 {
		t.Errorf("expires_at = %v, want the submitted future time", created.ExpiresAt)
	}
	if created.Status != apikey.StatusActive {
		t.Errorf("status = %q, want active for a future expiry", created.Status)
	}
}

func TestRevokeIsIdempotentAndEffective(t *testing.T) {
	store := newFakeStore()
	r := newKeyServer(store)
	seeded := seedKey(t, store, "revoke-me", nil)

	w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/keys/%d/revoke", seeded.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var revoked keyBody
	if err := json.Unmarshal(w.Body.Bytes(), &revoked); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if revoked.Status != apikey.StatusRevoked {
		t.Errorf("status = %q, want revoked", revoked.Status)
	}
	if revoked.RevokedAt == nil {
		t.Error("revoked_at should be stamped")
	}

	// 幂等:第二次吊销仍然 200,且不改变首次吊销时间。
	w = doJSON(r, http.MethodPost, fmt.Sprintf("/admin/keys/%d/revoke", seeded.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second revoke status = %d, want 200", w.Code)
	}
	var again keyBody
	if err := json.Unmarshal(w.Body.Bytes(), &again); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if again.RevokedAt == nil || revoked.RevokedAt == nil || !again.RevokedAt.Equal(*revoked.RevokedAt) {
		t.Errorf("revoked_at drifted on idempotent revoke: %v vs %v", again.RevokedAt, revoked.RevokedAt)
	}

	w = doJSON(r, http.MethodPost, "/admin/keys/999/revoke", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("revoke missing status = %d, want 404", w.Code)
	}
}

func TestTopUpUpdatesBalanceImmediatelyAndLogs(t *testing.T) {
	store := newFakeStore()
	r := newKeyServer(store)
	seeded := seedKey(t, store, "canvas-service", func(k *apikey.Key) {
		k.QuotaMicros = apikey.USDToMicros(10)
	})

	fetchLedger := func(t *testing.T) []apikey.QuotaEntry {
		t.Helper()
		w := doJSON(r, http.MethodGet, fmt.Sprintf("/admin/keys/%d/quota-log", seeded.ID), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("quota-log status = %d, want 200", w.Code)
		}
		var body struct {
			Entries []struct {
				ID         int64   `json:"id"`
				DeltaUSD   float64 `json:"delta_usd"`
				BalanceUSD float64 `json:"balance_usd"`
				Reason     string  `json:"reason"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("bad quota-log JSON: %v", err)
		}
		out := make([]apikey.QuotaEntry, 0, len(body.Entries))
		for _, e := range body.Entries {
			out = append(out, apikey.QuotaEntry{
				ID: e.ID, DeltaMicros: apikey.USDToMicros(e.DeltaUSD),
				BalanceMicros: apikey.USDToMicros(e.BalanceUSD), Reason: e.Reason,
			})
		}
		return out
	}

	// 初始额度也留下了流水。
	entries := fetchLedger(t)
	if len(entries) != 1 || entries[0].Reason != apikey.ReasonInitial || entries[0].BalanceMicros != apikey.USDToMicros(10) {
		t.Fatalf("initial ledger = %+v, want one initial entry at 10 USD", entries)
	}

	// 手工充值后余额即时变化。
	w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/keys/%d/topup", seeded.ID), map[string]any{
		"amount_usd": 2.5,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("topup status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var topped keyBody
	if err := json.Unmarshal(w.Body.Bytes(), &topped); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if topped.QuotaUSD != 12.5 {
		t.Errorf("quota_usd after topup = %v, want 12.5", topped.QuotaUSD)
	}

	entries = fetchLedger(t)
	if len(entries) != 2 {
		t.Fatalf("ledger = %d entries, want 2", len(entries))
	}
	if entries[0].Reason != apikey.ReasonManualTopUp || entries[0].BalanceMicros != apikey.USDToMicros(12.5) {
		t.Errorf("newest entry = %+v, want the manual topup at 12.5 USD", entries[0])
	}

	// 非法金额拒绝,余额不动。
	for _, amount := range []any{0, -1, 5_000_000} {
		w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/keys/%d/topup", seeded.ID), map[string]any{"amount_usd": amount})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("topup %v status = %d, want 400", amount, w.Code)
		}
	}
	if w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/keys/%d/topup", seeded.ID), nil); w.Code != http.StatusBadRequest {
		t.Fatalf("topup missing amount status = %d, want 400", w.Code)
	}
	k, err := store.ByHash(t.Context(), seeded.KeyHash)
	if err != nil || k.QuotaMicros != apikey.USDToMicros(12.5) {
		t.Errorf("balance after rejected topups = %v (err %v), want unchanged 12.5", k.QuotaMicros, err)
	}

	w = doJSON(r, http.MethodPost, "/admin/keys/999/topup", map[string]any{"amount_usd": 1})
	if w.Code != http.StatusNotFound {
		t.Fatalf("topup missing key status = %d, want 404", w.Code)
	}
}

func TestTopUpRejectsInactiveKeys(t *testing.T) {
	store := newFakeStore()
	r := newKeyServer(store)
	revoked := seedKey(t, store, "revoked", func(k *apikey.Key) {
		now := time.Now()
		k.RevokedAt = &now
	})
	expired := seedKey(t, store, "expired", func(k *apikey.Key) {
		past := time.Now().Add(-time.Hour)
		k.ExpiresAt = &past
	})

	for _, id := range []int64{revoked.ID, expired.ID} {
		w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/keys/%d/topup", id), map[string]any{"amount_usd": 1})
		if w.Code != http.StatusConflict {
			t.Fatalf("topup on inactive key %d status = %d, want 409; body: %s", id, w.Code, w.Body.String())
		}
		if got := decodeError(t, w).Error.Code; got != "key_not_active" {
			t.Errorf("code = %q, want key_not_active", got)
		}
	}
}

func TestQuotaLogOnMissingKeyIs404(t *testing.T) {
	r := newKeyServer(newFakeStore())

	w := doJSON(r, http.MethodGet, "/admin/keys/999/quota-log", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("quota-log missing key status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if got := decodeError(t, w).Error.Code; got != "not_found" {
		t.Errorf("code = %q, want not_found", got)
	}
}

func TestKeyCreateMapsStoreFailureTo500(t *testing.T) {
	store := newFakeStore()
	store.broken = true
	w := doJSON(newKeyServer(store), http.MethodPost, "/admin/keys", map[string]any{"name": "x"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if got := decodeError(t, w).Error.Code; got != "internal_error" {
		t.Errorf("code = %q, want internal_error", got)
	}
}
