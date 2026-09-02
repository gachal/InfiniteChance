package apikey_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
)

// newRelayServer mounts RequireKey on a probe route the way ticket 04's
// /v1 group will, so the rejection contract is verified at the HTTP seam.
func newRelayServer(store apikey.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/models", apikey.RequireKey(store), func(c *gin.Context) {
		k, ok := apikey.KeyFrom(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "key missing from context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"key_id": k.ID, "prefix": k.Prefix})
	})
	return r
}

type openaiErrorBody struct {
	Error struct {
		Message string  `json:"message"`
		Type    string  `json:"type"`
		Param   *string `json:"param"`
		Code    string  `json:"code"`
	} `json:"error"`
}

func doGet(r http.Handler, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeOpenAIError(t *testing.T, w *httptest.ResponseRecorder) openaiErrorBody {
	t.Helper()
	var body openaiErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the OpenAI error shape: %v\nbody: %s", err, w.Body.String())
	}
	return body
}

func TestRequireKeyAcceptsValidKey(t *testing.T) {
	store := newFakeStore()
	full := "sk-full-valid-key-for-relay-000000000"
	seedKey(t, store, "valid", func(k *apikey.Key) {
		k.KeyHash = apikey.Hash(full)
		k.Prefix = apikey.PrefixOf(full)
	})
	r := newRelayServer(store)

	w := doGet(r, "/v1/models", full)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		KeyID  int64  `json:"key_id"`
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if body.Prefix != apikey.PrefixOf(full) {
		t.Errorf("handler saw prefix %q, want the authenticated key", body.Prefix)
	}
}

func TestRequireKeyRejectsUniformly(t *testing.T) {
	store := newFakeStore()
	seedKey(t, store, "revoked", func(k *apikey.Key) {
		now := time.Now()
		k.RevokedAt = &now
	})
	seedKey(t, store, "expired", func(k *apikey.Key) {
		past := time.Now().Add(-time.Hour)
		k.ExpiresAt = &past
	})
	r := newRelayServer(store)

	cases := []struct {
		name     string
		bearer   string
		wantCode string
	}{
		{"missing header", "", apikey.CodeMissingAPIKey},
		{"unknown key", "sk-totally-unknown-key-aaaaaaaaaaaaaa", apikey.CodeInvalidAPIKey},
		{"revoked key", "sk-seeded-full-revoked", apikey.CodeKeyRevoked},
		{"expired key", "sk-seeded-full-expired", apikey.CodeKeyExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doGet(r, "/v1/models", tc.bearer)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
			}
			body := decodeOpenAIError(t, w)
			if body.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.wantCode)
			}
			if body.Error.Type != "invalid_request_error" {
				t.Errorf("type = %q, want invalid_request_error", body.Error.Type)
			}
			if body.Error.Param != nil {
				t.Errorf("param = %v, want null", body.Error.Param)
			}
			if body.Error.Message == "" {
				t.Error("message must not be empty")
			}
		})
	}
}

func TestRequireKeyCaseInsensitiveScheme(t *testing.T) {
	store := newFakeStore()
	full := "sk-full-valid-key-for-scheme-00000000"
	seedKey(t, store, "valid", func(k *apikey.Key) {
		k.KeyHash = apikey.Hash(full)
	})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/models", apikey.RequireKey(store), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "bearer "+full)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for lowercase bearer scheme", w.Code)
	}
}
