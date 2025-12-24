package rig

import (
	"net/http"
	"strings"
)

// Recover creates middleware that recovers from panics and returns a 500 error.
// This ensures the server never crashes from unhandled panics in handlers.
//
// Example:
//
//	r := rig.New()
//	r.Use(rig.Recover())
func Recover() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			defer func() {
				if err := recover(); err != nil {
					// Log the stack trace here (optional)
					// log.Printf("PANIC: %v\n%s", err, debug.Stack())

					// Return a generic error to the client
					_ = c.JSON(http.StatusInternalServerError, map[string]string{
						"error": "Internal Server Error",
					})
				}
			}()
			return next(c)
		}
	}
}

// CORSConfig defines the configuration for CORS middleware.
type CORSConfig struct {
	// AllowOrigins is a list of origins that are allowed to access the resource.
	// Use "*" to allow all origins.
	AllowOrigins []string

	// AllowMethods is a list of methods allowed when accessing the resource.
	AllowMethods []string

	// AllowHeaders is a list of headers that can be used during the request.
	AllowHeaders []string
}

// DefaultCORS creates CORS middleware with a permissive default configuration.
// It allows all origins and common HTTP methods and headers.
//
// Example:
//
//	r := rig.New()
//	r.Use(rig.DefaultCORS())
func DefaultCORS() MiddlewareFunc {
	return CORS(CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
	})
}

// CORS creates middleware that sets the necessary headers for Cross-Origin requests.
//
// Example:
//
//	r := rig.New()
//	r.Use(rig.CORS(rig.CORSConfig{
//	    AllowOrigins: []string{"https://example.com"},
//	    AllowMethods: []string{"GET", "POST"},
//	    AllowHeaders: []string{"Content-Type", "Authorization"},
//	}))
func CORS(config CORSConfig) MiddlewareFunc {
	// Pre-compute joined strings at middleware creation time
	allowMethods := strings.Join(config.AllowMethods, ", ")
	allowHeaders := strings.Join(config.AllowHeaders, ", ")

	// Build a set for O(1) origin lookup
	allowAllOrigins := false
	originSet := make(map[string]struct{}, len(config.AllowOrigins))
	for _, o := range config.AllowOrigins {
		if o == "*" {
			allowAllOrigins = true
			break
		}
		originSet[o] = struct{}{}
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			origin := c.GetHeader("Origin")
			allowOrigin := ""

			if allowAllOrigins {
				allowOrigin = "*"
			} else if _, ok := originSet[origin]; ok {
				allowOrigin = origin
			}

			if allowOrigin != "" {
				c.SetHeader("Access-Control-Allow-Origin", allowOrigin)
			}

			// Handle Preflight OPTIONS request
			if c.Method() == http.MethodOptions {
				c.SetHeader("Access-Control-Allow-Methods", allowMethods)
				c.SetHeader("Access-Control-Allow-Headers", allowHeaders)
				c.Status(http.StatusNoContent)
				return nil
			}

			return next(c)
		}
	}
}
