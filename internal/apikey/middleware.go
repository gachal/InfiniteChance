package apikey

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

const ctxKeyAPIKey = "apikey.key"

const bearerScheme = "Bearer "

// Relay error codes for the unified OpenAI-shaped 401. Same shape and status
// for every rejection — clients branch on code, never on body quirks.
const (
	CodeMissingAPIKey = "missing_api_key"
	CodeInvalidAPIKey = "invalid_api_key"
	CodeKeyRevoked    = "key_revoked"
	CodeKeyExpired    = "key_expired"
)

// RequireKey guards the relay (/v1) surface: it demands a Bearer API key
// that exists, is neither revoked nor expired, and answers a unified
// OpenAI-shaped 401 otherwise, so SDK clients parse gateway rejections the
// same way they parse vendor ones. On success the validated key rides along
// in the request context (KeyFrom) for billing attribution (ticket 04
// mounts this on the /v1 route group).
func RequireKey(store Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		presented := bearerKey(c.Request.Header.Get("Authorization"))
		if presented == "" {
			reject(c, CodeMissingAPIKey,
				"You didn't provide an API key. Include it as 'Authorization: Bearer sk-…'.")
			return
		}
		k, err := store.ByHash(c.Request.Context(), Hash(presented))
		if errors.Is(err, ErrKeyNotFound) {
			reject(c, CodeInvalidAPIKey, "Incorrect API key provided.")
			return
		}
		if err != nil {
			apierr.OpenAI(c, http.StatusInternalServerError, "internal_error",
				"server_error", "The gateway hit an internal error while checking the API key.")
			c.Abort()
			return
		}
		switch k.Status(time.Now()) {
		case StatusRevoked:
			reject(c, CodeKeyRevoked, "This API key has been revoked.")
		case StatusExpired:
			reject(c, CodeKeyExpired, "This API key has expired.")
		default:
			c.Set(ctxKeyAPIKey, k)
			c.Next()
		}
	}
}

// KeyFrom returns the validated key RequireKey stored in the context.
func KeyFrom(c *gin.Context) (Key, bool) {
	key, ok := c.Get(ctxKeyAPIKey)
	typed, _ := key.(Key)
	return typed, ok
}

// reject answers the unified 401 and stops the chain.
func reject(c *gin.Context, code, message string) {
	apierr.OpenAI(c, http.StatusUnauthorized, code, "invalid_request_error", message)
	c.Abort()
}

// bearerKey extracts the key part of an Authorization header, accepting any
// case for the scheme.
func bearerKey(header string) string {
	if len(header) > len(bearerScheme) && strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
		return strings.TrimSpace(header[len(bearerScheme):])
	}
	return ""
}
