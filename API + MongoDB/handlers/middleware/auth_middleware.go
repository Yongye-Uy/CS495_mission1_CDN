package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireAdminKey checks that the incoming request contains a valid X-Admin-Key header.
func RequireAdminKey(expectedKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if expectedKey == "" {
			c.Next()
			return
		}

		key := c.GetHeader("X-Admin-Key")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Unauthorized: missing X-Admin-Key header",
			})
			return
		}

		// Constant time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Unauthorized: invalid X-Admin-Key",
			})
			return
		}

		c.Next()
	}
}
