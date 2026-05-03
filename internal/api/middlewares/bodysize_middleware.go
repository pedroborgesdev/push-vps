package middlewares

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxRequestBodyBytes is the enforced limit for POST /prompt request bodies.
const MaxRequestBodyBytes int64 = 10 * 1024

// MaxBodySize rejects requests whose body exceeds the given byte limit.
func MaxBodySize(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "request body too large",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
