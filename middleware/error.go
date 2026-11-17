package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorHandler is a middleware for centralizing error handling
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Recover from panics and handle errors
				if !c.IsAborted() {
					handleError(c, err)
				}
			}
		}()
		// Continue processing the request
		c.Next()
	}
}

// handleError handles different types of errors and returns a standardized response
func handleError(c *gin.Context, err any) {
	var status int
	var message string

	switch e := err.(type) {
	case error:
		status = http.StatusInternalServerError
		message = e.Error()
	default:
		status = http.StatusInternalServerError
		message = "Internal Server Error"
	}

	c.JSON(status, gin.H{
		"error": message,
	})
	c.Abort()
}
