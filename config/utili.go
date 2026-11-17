package config

import (
	"github.com/didip/tollbooth"
	"github.com/gin-gonic/gin"
)

// TrustedProxies are the peers allowed to set X-Forwarded-For / X-Real-IP.
// The host nginx reaches the app container through Docker, so the peer is the
// bridge gateway (172.16.0.0/12), not 127.0.0.1. Anything outside these
// ranges is a direct client and its forwarding headers are ignored.
var TrustedProxies = []string{
	"127.0.0.1", "::1",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
}

func Ratelimit(max float64) gin.HandlerFunc {
	// Create a new limiter with the specified rate limit (max)
	lmt := tollbooth.NewLimiter(max, nil)
	// Return the Gin middleware function
	return func(c *gin.Context) {
		// Key on c.ClientIP(), which only honours forwarding headers from
		// TrustedProxies. tollbooth's own lookup checks RemoteAddr first, so
		// behind the proxy every client shared a single bucket.
		httpError := tollbooth.LimitByKeys(lmt, []string{c.ClientIP()})
		if httpError != nil {
			// If the request exceeds the limit, send an error response and abort
			c.Data(httpError.StatusCode, lmt.GetMessageContentType(), []byte(httpError.Message))
			c.Abort()
		} else {
			// If the request is within the limit, proceed to the next middleware
			c.Next()
		}
	}
}
