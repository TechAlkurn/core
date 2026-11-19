package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func ConcurrencyLimiter(maxConcurrent int) gin.HandlerFunc {
	sem := make(chan struct{}, maxConcurrent)

	return func(c *gin.Context) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			c.Next()
		case <-time.After(1 * time.Second):
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "server busy",
			})
		}
	}
}

func ErrorHandlerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Check if any error was set but not handled
		if len(c.Errors) > 0 && !c.Writer.Written() {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
	}
}
