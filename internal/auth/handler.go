package auth

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

const (
	maxUsernameRunes = 64
	minPasswordRunes = 8
)

// Handlers serves the /auth endpoints. The gateway mounts the full set;
// canvas/server only needs RegisterMeRoute for cross-service validation.
type Handlers struct {
	Store  Store
	Issuer *Issuer
}

// RegisterRoutes mounts the gateway's auth surface:
//
//	GET  /auth/status  — has the admin been initialized?
//	POST /auth/init    — create the first admin (once), returns a session
//	POST /auth/login   — verify credentials, returns a session
//	GET  /auth/me      — protected: who holds this token?
func RegisterRoutes(r *gin.Engine, h *Handlers) {
	group := r.Group("/auth")
	group.GET("/status", h.Status)
	group.POST("/init", h.Init)
	group.POST("/login", h.Login)
	group.GET("/me", RequireAuth(h.Issuer), me)
}

// RegisterMeRoute mounts the protected identity probe for services that
// validate gateway-issued tokens without issuing any themselves.
func RegisterMeRoute(r *gin.Engine, issuer *Issuer) {
	r.GET("/auth/me", RequireAuth(issuer), me)
}

type statusResponse struct {
	Initialized bool `json:"initialized"`
}

type sessionResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Username  string    `json:"username"`
}

type meResponse struct {
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
}

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// dummyHash backs the unknown-user login path so its timing matches the
// wrong-password path instead of skipping bcrypt entirely.
var dummyHash = func() string {
	hash, err := HashPassword("infinitechance-timing-equalizer")
	if err != nil {
		panic("auth: cannot derive dummy bcrypt hash: " + err.Error())
	}
	return hash
}()

// Status reports whether the first admin exists yet.
func (h *Handlers) Status(c *gin.Context) {
	initialized, err := h.Store.Initialized(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, statusResponse{Initialized: initialized})
}

// Init creates the one and only admin account, then answers with a fresh
// session so the setup wizard flows straight into the app.
func (h *Handlers) Init(c *gin.Context) {
	creds, ok := bindCredentials(c)
	if !ok {
		return
	}
	if utf8.RuneCountInString(creds.Username) > maxUsernameRunes {
		apierr.InvalidRequest(c, "用户名最多 64 个字符")
		return
	}
	if utf8.RuneCountInString(creds.Password) < minPasswordRunes {
		apierr.InvalidRequest(c, "密码至少 8 个字符")
		return
	}

	hash, err := HashPassword(creds.Password)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if err := h.Store.CreateFirstAdmin(c.Request.Context(), creds.Username, hash); err != nil {
		if errors.Is(err, ErrAdminExists) {
			apierr.Conflict(c, "already_initialized", "管理员账号已存在,请直接登录")
			return
		}
		h.failInternal(c, err)
		return
	}
	respondSession(c, h.Issuer, http.StatusCreated, creds.Username)
}

// Login verifies credentials and answers with a fresh session.
func (h *Handlers) Login(c *gin.Context) {
	creds, ok := bindCredentials(c)
	if !ok {
		return
	}

	account, err := h.Store.AccountByUsername(c.Request.Context(), creds.Username)
	if err != nil {
		if errors.Is(err, ErrAdminNotFound) {
			CheckPassword(dummyHash, creds.Password)
			apierr.InvalidCredentials(c, "用户名或密码错误")
			return
		}
		h.failInternal(c, err)
		return
	}
	if !CheckPassword(account.PasswordHash, creds.Password) {
		apierr.InvalidCredentials(c, "用户名或密码错误")
		return
	}
	respondSession(c, h.Issuer, http.StatusOK, account.Username)
}

// me answers the identity probe for the token RequireAuth validated.
func me(c *gin.Context) {
	claims := ClaimsFrom(c)
	if claims == nil {
		apierr.Unauthorized(c, "无效的访问令牌")
		return
	}
	c.JSON(http.StatusOK, meResponse{Username: claims.Username, ExpiresAt: claims.ExpiresAt.Time})
}

// bindCredentials decodes {username, password} and rejects structurally
// invalid bodies with a standard 400; policy checks stay with Init.
func bindCredentials(c *gin.Context) (credentialsRequest, bool) {
	var creds credentialsRequest
	if err := c.ShouldBindJSON(&creds); err != nil {
		apierr.InvalidRequest(c, "请求体必须是包含 username 与 password 的 JSON")
		return creds, false
	}
	creds.Username = strings.TrimSpace(creds.Username)
	if creds.Username == "" || creds.Password == "" {
		apierr.InvalidRequest(c, "username 与 password 不能为空")
		return creds, false
	}
	return creds, true
}

func respondSession(c *gin.Context, issuer *Issuer, status int, username string) {
	token, expiresAt, err := issuer.Issue(username, time.Now())
	if err != nil {
		log.Printf("auth: issue token: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.JSON(status, sessionResponse{Token: token, ExpiresAt: expiresAt, Username: username})
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("auth: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}
