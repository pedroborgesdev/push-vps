package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func Respond(c *gin.Context, status int, body interface{}) {
	c.JSON(status, body)
}

func Success(c *gin.Context, body interface{}) {
	Respond(c, http.StatusOK, body)
}

func BadRequest(c *gin.Context, body interface{}) {
	Respond(c, http.StatusBadRequest, body)
}

func Unauthorized(c *gin.Context, body interface{}) {
	Respond(c, http.StatusUnauthorized, body)
}

func Forbidden(c *gin.Context, body interface{}) {
	Respond(c, http.StatusForbidden, body)
}

func NotFound(c *gin.Context, body interface{}) {
	Respond(c, http.StatusNotFound, body)
}

func Conflict(c *gin.Context, body interface{}) {
	Respond(c, http.StatusConflict, body)
}

func TooManyRequests(c *gin.Context, body interface{}) {
	Respond(c, http.StatusTooManyRequests, body)
}

func InternalError(c *gin.Context, body interface{}) {
	Respond(c, http.StatusInternalServerError, body)
}
