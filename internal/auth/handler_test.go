package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/auth"
)

// fakeStore is the in-memory Store double; it lets handler tests run at the
// HTTP seam without MySQL.
type fakeStore struct {
	accounts map[string]string // username -> password hash
}

func newFakeStore() *fakeStore { return &fakeStore{accounts: map[string]string{}} }

func (f *fakeStore) Initialized(context.Context) (bool, error) {
	return len(f.accounts) > 0, nil
}

func (f *fakeStore) CreateFirstAdmin(_ context.Context, username, passwordHash string) error {
	if len(f.accounts) > 0 {
		return auth.ErrAdminExists
	}
	f.accounts[username] = passwordHash
	return nil
}

func (f *fakeStore) AccountByUsername(_ context.Context, username string) (auth.Account, error) {
	hash, ok := f.accounts[username]
	if !ok {
		return auth.Account{}, auth.ErrAdminNotFound
	}
	return auth.Account{Username: username, PasswordHash: hash}, nil
}

const testSecret = "test-secret"

func newAuthServer(store auth.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth.RegisterRoutes(r, &auth.Handlers{Store: store, Issuer: auth.NewIssuer(testSecret, auth.SessionTTL)})
	return r
}

func doJSON(r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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

type sessionBody struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Username  string    `json:"username"`
}

func TestStatusOnFreshStore(t *testing.T) {
	w := doJSON(newAuthServer(newFakeStore()), http.MethodGet, "/auth/status", "", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Initialized bool `json:"initialized"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if body.Initialized {
		t.Error("fresh store should report initialized=false")
	}
}

func TestInitCreatesAdminThenLoginWorks(t *testing.T) {
	store := newFakeStore()
	r := newAuthServer(store)

	w := doJSON(r, http.MethodPost, "/auth/init", "", map[string]string{
		"username": "admin",
		"password": "s3cret-password",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("init status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var session sessionBody
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("init response is not a session: %v", err)
	}
	if session.Username != "admin" || session.Token == "" {
		t.Fatalf("init session = %+v, want username and token", session)
	}
	if !session.ExpiresAt.After(time.Now()) {
		t.Errorf("expires_at = %v, want a future time", session.ExpiresAt)
	}
	if _, err := auth.NewIssuer(testSecret, auth.SessionTTL).Parse(session.Token); err != nil {
		t.Errorf("issued token should parse with the shared secret: %v", err)
	}

	// 密码只允许以哈希形态落库。
	account, err := store.AccountByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatalf("AccountByUsername: %v", err)
	}
	if account.PasswordHash == "s3cret-password" {
		t.Fatal("password was stored in plaintext")
	}
	if !auth.CheckPassword(account.PasswordHash, "s3cret-password") {
		t.Error("stored hash should verify against the original password")
	}

	// 初始化后 status 翻转,引导不再出现。
	w = doJSON(r, http.MethodGet, "/auth/status", "", nil)
	var status struct {
		Initialized bool `json:"initialized"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if !status.Initialized {
		t.Error("after init, status should report initialized=true")
	}

	// 同一组凭据可以登录。
	w = doJSON(r, http.MethodPost, "/auth/login", "", map[string]string{
		"username": "admin",
		"password": "s3cret-password",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var login sessionBody
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil {
		t.Fatalf("login response is not a session: %v", err)
	}
	if login.Token == "" || login.Username != "admin" {
		t.Fatalf("login session = %+v, want username and token", login)
	}
}

func TestInitRejectedOnceInitialized(t *testing.T) {
	store := newFakeStore()
	r := newAuthServer(store)
	initBody := map[string]string{"username": "admin", "password": "s3cret-password"}
	if w := doJSON(r, http.MethodPost, "/auth/init", "", initBody); w.Code != http.StatusCreated {
		t.Fatalf("first init status = %d, want 201", w.Code)
	}

	w := doJSON(r, http.MethodPost, "/auth/init", "", initBody)
	if w.Code != http.StatusConflict {
		t.Fatalf("second init status = %d, want 409", w.Code)
	}
	if got := decodeError(t, w).Error.Code; got != "already_initialized" {
		t.Errorf("code = %q, want %q", got, "already_initialized")
	}
}

func TestInitValidatesInput(t *testing.T) {
	r := newAuthServer(newFakeStore())

	cases := []struct {
		name string
		body map[string]string
	}{
		{"empty username", map[string]string{"username": "", "password": "long-enough-pw"}},
		{"short password", map[string]string{"username": "admin", "password": "short"}},
		{"missing password", map[string]string{"username": "admin"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(r, http.MethodPost, "/auth/init", "", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			if got := decodeError(t, w).Error.Code; got != "invalid_request" {
				t.Errorf("code = %q, want %q", got, "invalid_request")
			}
		})
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	r := newAuthServer(newFakeStore())
	if w := doJSON(r, http.MethodPost, "/auth/init", "", map[string]string{
		"username": "admin", "password": "s3cret-password",
	}); w.Code != http.StatusCreated {
		t.Fatalf("init status = %d, want 201", w.Code)
	}

	cases := []struct {
		name     string
		username string
		password string
	}{
		{"wrong password", "admin", "wrong-password"},
		{"unknown user", "nobody", "s3cret-password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(r, http.MethodPost, "/auth/login", "", map[string]string{
				"username": tc.username, "password": tc.password,
			})
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if got := decodeError(t, w).Error.Code; got != "invalid_credentials" {
				t.Errorf("code = %q, want %q", got, "invalid_credentials")
			}
		})
	}
}

func TestMeRequiresValidToken(t *testing.T) {
	r := newAuthServer(newFakeStore())
	init := doJSON(r, http.MethodPost, "/auth/init", "", map[string]string{
		"username": "admin", "password": "s3cret-password",
	})
	var session sessionBody
	if err := json.Unmarshal(init.Body.Bytes(), &session); err != nil {
		t.Fatalf("init response is not a session: %v", err)
	}

	t.Run("valid token returns the identity", func(t *testing.T) {
		w := doJSON(r, http.MethodGet, "/auth/me", session.Token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var me struct {
			Username  string    `json:"username"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
			t.Fatalf("bad JSON: %v", err)
		}
		if me.Username != "admin" {
			t.Errorf("username = %q, want %q", me.Username, "admin")
		}
		if !me.ExpiresAt.Equal(session.ExpiresAt) {
			t.Errorf("expires_at = %v, want %v", me.ExpiresAt, session.ExpiresAt)
		}
	})

	t.Run("missing token is a standard 401", func(t *testing.T) {
		w := doJSON(r, http.MethodGet, "/auth/me", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if got := decodeError(t, w).Error.Code; got != "unauthorized" {
			t.Errorf("code = %q, want %q", got, "unauthorized")
		}
		if w.Header().Get("WWW-Authenticate") == "" {
			t.Error("401 must carry WWW-Authenticate")
		}
	})

	t.Run("garbage token is a standard 401", func(t *testing.T) {
		w := doJSON(r, http.MethodGet, "/auth/me", "garbage.token.value", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if got := decodeError(t, w).Error.Code; got != "unauthorized" {
			t.Errorf("code = %q, want %q", got, "unauthorized")
		}
	})

	t.Run("expired token is a standard 401", func(t *testing.T) {
		expiredIssuer := auth.NewIssuer(testSecret, -time.Minute)
		expired, _, err := expiredIssuer.Issue("admin", time.Now())
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		w := doJSON(r, http.MethodGet, "/auth/me", expired, nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if got := decodeError(t, w).Error.Code; got != "unauthorized" {
			t.Errorf("code = %q, want %q", got, "unauthorized")
		}
	})
}

func TestCanvasServerValidatesGatewayToken(t *testing.T) {
	// 网关与画布共享同一签名密钥,但各自持有独立的 Issuer 实例:
	// 网关签发,画布校验,同一账号体系。
	gateway := auth.NewIssuer(testSecret, auth.SessionTTL)
	canvas := auth.NewIssuer(testSecret, auth.SessionTTL)
	token, _, err := gateway.Issue("admin", time.Now())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	gin.SetMode(gin.TestMode)
	canvasEngine := gin.New()
	auth.RegisterMeRoute(canvasEngine, canvas)

	w := doJSON(canvasEngine, http.MethodGet, "/auth/me", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("canvas /auth/me status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	stranger := auth.NewIssuer("another-secret", auth.SessionTTL)
	foreign, _, err := stranger.Issue("admin", time.Now())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	w = doJSON(canvasEngine, http.MethodGet, "/auth/me", foreign, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("canvas /auth/me with foreign token status = %d, want 401", w.Code)
	}
}

var errStoreBroken = errors.New("store broken")

type brokenStore struct{ *fakeStore }

func (brokenStore) Initialized(context.Context) (bool, error) { return false, errStoreBroken }

func TestStatusMapsStoreFailureTo500(t *testing.T) {
	w := doJSON(newAuthServer(brokenStore{newFakeStore()}), http.MethodGet, "/auth/status", "", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if got := decodeError(t, w).Error.Code; got != "internal_error" {
		t.Errorf("code = %q, want %q", got, "internal_error")
	}
}
