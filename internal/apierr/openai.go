package apierr

import (
	"github.com/gin-gonic/gin"
)

// OpenAIError mirrors the OpenAI error object the relay (/v1) surface must
// answer with, so OpenAI SDK clients can parse gateway failures the same way
// they parse vendor failures. Param stays null; there is no request field
// to point at from the auth layer.
type OpenAIError struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    string  `json:"code"`
}

type openaiBody struct {
	Error OpenAIError `json:"error"`
}

// OpenAI writes the relay-facing error body:
// {"error":{"message","type","param":null,"code"}}.
func OpenAI(c *gin.Context, status int, code, typ, message string) {
	c.JSON(status, openaiBody{Error: OpenAIError{
		Message: message,
		Type:    typ,
		Param:   nil,
		Code:    code,
	}})
}
