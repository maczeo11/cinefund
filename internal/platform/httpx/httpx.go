package httpx

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/maczeo11/cinefund/internal/platform/errs"
)

// ErrorResponse is the JSON shape for errors.
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details any    `json:"details,omitempty"`
	} `json:"error"`
}

// Abort writes a JSON error and aborts.
func Abort(c *gin.Context, err error) {
	code := errs.HTTPStatus(err)
	resp := ErrorResponse{}
	resp.Error.Code = errs.Code(err)

	var e *errs.Error
	if errors.As(err, &e) {
		resp.Error.Message = e.Message
		resp.Error.Details = e.Details
	} else {
		resp.Error.Message = "internal error"
	}
	c.JSON(code, resp)
	c.Abort()
}

// BindJSON decodes and returns false on bad JSON.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		Abort(c, errs.Invalid("INVALID_BODY", "request body is not valid JSON"))
		return false
	}
	return true
}

func OK(c *gin.Context, body any)      { c.JSON(http.StatusOK, body) }
func Created(c *gin.Context, body any) { c.JSON(http.StatusCreated, body) }

// Middleware catches errors left by handlers and writes a JSON response.
func Middleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 {
			return
		}
		err := c.Errors.Last().Err
		log.Error("request failed", "path", c.Request.URL.Path, "error", err)
		Abort(c, err)
	}
}
