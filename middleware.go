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
	// Supports three formats:
	//   - "*" to allow all origins
	//   - Exact match: "https://example.com"
	//   - Wildcard subdomain: "https://*.example.com" (matches any subdomain)
	AllowOrigins []string

	// AllowMethods is a list of methods allowed when accessing the resource.
	AllowMethods []string

	// AllowHeaders is a list of headers that can be used during the request.
	AllowHeaders []string
}

// wildcardPattern represents a parsed wildcard origin pattern.
type wildcardPattern struct {
	prefix string // e.g., "https://"
	suffix string // e.g., ".example.com"
}

// matches checks if the origin matches this wildcard pattern.
func (w wildcardPattern) matches(origin string) bool {
	return len(origin) > len(w.prefix)+len(w.suffix) &&
		strings.HasPrefix(origin, w.prefix) &&
		strings.HasSuffix(origin, w.suffix)
}

// parseWildcardPattern parses an origin pattern containing a wildcard.
// Returns the pattern and true if valid, or zero value and false if invalid.
// Valid format: "scheme://*.domain" (e.g., "https://*.example.com")
func parseWildcardPattern(pattern string) (wildcardPattern, bool) {
	// Find the wildcard position
	wildcardIdx := strings.Index(pattern, "*")
	if wildcardIdx == -1 {
		return wildcardPattern{}, false
	}

	// Ensure wildcard is followed by a dot (*.domain format)
	if wildcardIdx+1 >= len(pattern) || pattern[wildcardIdx+1] != '.' {
		return wildcardPattern{}, false
	}

	prefix := pattern[:wildcardIdx]
	suffix := pattern[wildcardIdx+1:] // includes the dot

	// Validate prefix ends with "://" (scheme separator)
	if !strings.HasSuffix(prefix, "://") {
		return wildcardPattern{}, false
	}

	return wildcardPattern{prefix: prefix, suffix: suffix}, true
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
// Supports wildcard subdomains in AllowOrigins:
//
//	r.Use(rig.CORS(rig.CORSConfig{
//	    AllowOrigins: []string{
//	        "https://*.example.com",  // Matches any subdomain
//	        "https://api.other.com",  // Exact match
//	    },
//	    AllowMethods: []string{"GET", "POST"},
//	    AllowHeaders: []string{"Content-Type", "Authorization"},
//	}))
func CORS(config CORSConfig) MiddlewareFunc {
	// Pre-compute joined strings at middleware creation time
	allowMethods := strings.Join(config.AllowMethods, ", ")
	allowHeaders := strings.Join(config.AllowHeaders, ", ")

	// Categorize origins: all, exact matches, or wildcard patterns
	allowAllOrigins := false
	originSet := make(map[string]struct{})
	var wildcardPatterns []wildcardPattern

	for _, o := range config.AllowOrigins {
		if o == "*" {
			allowAllOrigins = true
			break
		}
		if strings.Contains(o, "*") {
			if wp, ok := parseWildcardPattern(o); ok {
				wildcardPatterns = append(wildcardPatterns, wp)
			}
			// Invalid wildcard patterns are silently ignored
		} else {
			originSet[o] = struct{}{}
		}
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			origin := c.GetHeader("Origin")
			allowOrigin := ""

			if allowAllOrigins {
				allowOrigin = "*"
			} else if _, ok := originSet[origin]; ok {
				// Exact match (O(1) lookup)
				allowOrigin = origin
			} else {
				// Check wildcard patterns (O(n) where n = number of patterns)
				for _, wp := range wildcardPatterns {
					if wp.matches(origin) {
						allowOrigin = origin
						break
					}
				}
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
