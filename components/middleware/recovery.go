package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

func EnhancedRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				if strings.Contains(fmt.Sprint(err), "closed writer") {
					logrus.Error("Writer closed panic recovered",
						zap.Any("error", err),
						zap.String("path", c.Request.URL.Path),
					)
					c.AbortWithStatus(499) // Client Closed Request
					return
				}
				// Existing recovery logic
			}
		}()
		c.Next()
	}
}
