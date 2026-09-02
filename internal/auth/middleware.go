package auth

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

const ctxKeyClaims = "auth.claims"

const bearerPrefix = "Bearer "

// RequireAuth is the gin middleware guarding admin endpoints: it demands a
// Bearer token signed with the shared secret and unexpired, answering a
// standard 401 otherwise. On success the parsed claims ride along in the
// request context (see ClaimsFrom).
func RequireAuth(issuer *Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.Request.Header.Get("Authorization"))
		if token == "" {
			apierr.Unauthorized(c, "缺少访问令牌")
			c.Abort()
			return
		}
		claims, err := issuer.Parse(token)
		if err != nil {
			message := "无效的访问令牌"
			if IsExpiredError(err) {
				message = "会话已过期,请重新登录"
			}
			apierr.Unauthorized(c, message)
			c.Abort()
			return
		}
		c.Set(ctxKeyClaims, claims)
		c.Next()
	}
}

// ClaimsFrom returns the validated claims RequireAuth stored in the context.
func ClaimsFrom(c *gin.Context) *Claims {
	claims, _ := c.Get(ctxKeyClaims)
	typed, _ := claims.(*Claims)
	return typed
}

// bearerToken extracts the token part of an Authorization header, accepting
// any case for the scheme.
func bearerToken(header string) string {
	if len(header) > len(bearerPrefix) && strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(header[len(bearerPrefix):])
	}
	return ""
}
