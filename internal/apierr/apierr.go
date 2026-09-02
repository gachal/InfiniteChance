// Package apierr writes the admin APIs' uniform JSON error body:
// {"error":{"code":"…","message":"…"}}. Codes are stable machine
// identifiers for clients to branch on; messages are user-facing.
package apierr

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type errorObject struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type body struct {
	Error errorObject `json:"error"`
}

// Write answers with status and the standard error body.
func Write(c *gin.Context, status int, code, message string) {
	c.JSON(status, body{Error: errorObject{Code: code, Message: message}})
}

// Unauthorized answers 401 with the Bearer WWW-Authenticate challenge.
func Unauthorized(c *gin.Context, message string) {
	c.Header("WWW-Authenticate", `Bearer realm="infinitechance-admin"`)
	Write(c, http.StatusUnauthorized, "unauthorized", message)
}

// InvalidRequest answers 400 for a malformed or policy-violating request.
func InvalidRequest(c *gin.Context, message string) {
	Write(c, http.StatusBadRequest, "invalid_request", message)
}

// InvalidCredentials answers 401 without revealing which half was wrong.
func InvalidCredentials(c *gin.Context, message string) {
	Write(c, http.StatusUnauthorized, "invalid_credentials", message)
}

// Conflict answers 409 for a request the current state forbids.
func Conflict(c *gin.Context, code, message string) {
	Write(c, http.StatusConflict, code, message)
}

// Internal answers 500 with a message safe to show users; log the details
// at the call site before calling.
func Internal(c *gin.Context, message string) {
	Write(c, http.StatusInternalServerError, "internal_error", message)
}

// NotFound answers 404 for an id that has no row.
func NotFound(c *gin.Context, message string) {
	Write(c, http.StatusNotFound, "not_found", message)
}
