// Package auth provides authentication middleware for the rig HTTP library.
//
// It supports API Key authentication (via header or query parameter) and
// Bearer Token authentication. Both middleware types are configurable and
// store authentication results in the request context for downstream handlers.
//
// Example usage:
//
//	r := rig.New()
//
//	// Simple API key authentication
//	api := r.Group("/api")
//	api.Use(auth.APIKeySimple("my-secret-key"))
//
//	// Or with full configuration
//	api.Use(auth.APIKey(auth.APIKeyConfig{
//	    Name: "X-API-Key",
//	    Validator: func(key string) (string, bool) {
//	        if key == os.Getenv("API_KEY") {
//	            return "my-service", true
//	        }
//	        return "", false
//	    },
//	}))
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/cloudresty/rig"
)

// Context keys for accessing authentication information in handlers.
const (
	// ContextKeyIdentity holds the authenticated identity (e.g., user ID, service name).
	ContextKeyIdentity = "auth.identity"

	// ContextKeyMethod holds the authentication method used (e.g., "api_key", "bearer").
	ContextKeyMethod = "auth.method"
)

// ErrorResponse is the default error response structure.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ErrorHandler is a function that handles authentication errors.
// It receives the context and should write an appropriate error response.
type ErrorHandler func(c *rig.Context) error

// defaultErrorHandler returns a JSON error response with 401 status.
func defaultErrorHandler(message string) ErrorHandler {
	return func(c *rig.Context) error {
		return c.JSON(http.StatusUnauthorized, ErrorResponse{Error: message})
	}
}

// --- API Key Authentication ---

// APIKeyConfig defines the configuration for API Key authentication.
type APIKeyConfig struct {
	// Source specifies where to look for the API key.
	// Valid values: "header" (default), "query".
	Source string

	// Name is the header name or query parameter key.
	// Default: "X-API-Key".
	Name string

	// Validator is called to validate the API key.
	// It should return the identity (e.g., user ID, service name) and whether the key is valid.
	// The identity is stored in the context under ContextKeyIdentity.
	Validator func(key string) (identity string, valid bool)

	// OnError is called when authentication fails.
	// If nil, a default JSON error response is returned.
	OnError ErrorHandler
}

// APIKey creates middleware that authenticates requests using an API key.
// The key can be provided via header or query parameter based on configuration.
//
// On successful authentication, the identity is stored in the context
// and can be retrieved using auth.GetIdentity(c) or c.Get(auth.ContextKeyIdentity).
func APIKey(config APIKeyConfig) rig.MiddlewareFunc {
	// Apply defaults
	if config.Source == "" {
		config.Source = "header"
	}
	if config.Name == "" {
		config.Name = "X-API-Key"
	}
	if config.OnError == nil {
		config.OnError = defaultErrorHandler("Invalid or missing API key")
	}

	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			var key string

			switch strings.ToLower(config.Source) {
			case "query":
				key = c.Query(config.Name)
			default: // "header" or any unknown value defaults to header
				key = c.GetHeader(config.Name)
			}

			if key == "" {
				return config.OnError(c)
			}

			identity, valid := config.Validator(key)
			if !valid {
				return config.OnError(c)
			}

			// Store auth info in context for downstream handlers
			c.Set(ContextKeyIdentity, identity)
			c.Set(ContextKeyMethod, "api_key")

			return next(c)
		}
	}
}

// APIKeySimple creates a simple API Key middleware that validates against a list of keys.
// It uses constant-time comparison to prevent timing attacks.
//
// This is a convenience function for simple use cases. For more control,
// use APIKey with a custom Validator.
func APIKeySimple(validKeys ...string) rig.MiddlewareFunc {
	// Pre-build key set for the validator
	keySet := make(map[string]struct{}, len(validKeys))
	for _, k := range validKeys {
		keySet[k] = struct{}{}
	}

	return APIKey(APIKeyConfig{
		Validator: func(key string) (string, bool) {
			// Use constant-time comparison to prevent timing attacks
			for k := range keySet {
				if subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
					return "authenticated", true
				}
			}
			return "", false
		},
	})
}

// --- Bearer Token Authentication ---

// BearerConfig defines the configuration for Bearer Token authentication.
type BearerConfig struct {
	// Validator is called to validate the bearer token.
	// It should return the identity (e.g., user ID) and whether the token is valid.
	// The identity is stored in the context under ContextKeyIdentity.
	//
	// The token passed to Validator has already been extracted from the
	// "Authorization: Bearer <token>" header.
	Validator func(token string) (identity string, valid bool)

	// Realm is used in the WWW-Authenticate header on authentication failure.
	// Default: "API".
	Realm string

	// OnError is called when authentication fails.
	// If nil, a default JSON error response is returned with WWW-Authenticate header.
	OnError ErrorHandler
}

// Bearer creates middleware that authenticates requests using Bearer tokens.
// It extracts the token from the "Authorization: Bearer <token>" header.
//
// On successful authentication, the identity is stored in the context
// and can be retrieved using auth.GetIdentity(c) or c.Get(auth.ContextKeyIdentity).
//
// On failure, it sets the WWW-Authenticate header as per RFC 6750.
func Bearer(config BearerConfig) rig.MiddlewareFunc {
	// Apply defaults
	if config.Realm == "" {
		config.Realm = "API"
	}
	if config.OnError == nil {
		config.OnError = defaultErrorHandler("Invalid or missing bearer token")
	}

	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			auth := c.GetHeader("Authorization")

			// Check for "Bearer " prefix (case-insensitive as per RFC 6750)
			if len(auth) < 7 || !strings.EqualFold(auth[:7], "Bearer ") {
				c.SetHeader("WWW-Authenticate", `Bearer realm="`+config.Realm+`"`)
				return config.OnError(c)
			}

			token := strings.TrimSpace(auth[7:])
			if token == "" {
				c.SetHeader("WWW-Authenticate", `Bearer realm="`+config.Realm+`"`)
				return config.OnError(c)
			}

			identity, valid := config.Validator(token)
			if !valid {
				c.SetHeader("WWW-Authenticate", `Bearer realm="`+config.Realm+`", error="invalid_token"`)
				return config.OnError(c)
			}

			// Store auth info in context for downstream handlers
			c.Set(ContextKeyIdentity, identity)
			c.Set(ContextKeyMethod, "bearer")

			return next(c)
		}
	}
}

// --- Helper Functions ---

// GetIdentity retrieves the authenticated identity from the context.
// Returns empty string if not authenticated.
func GetIdentity(c *rig.Context) string {
	if identity, ok := c.Get(ContextKeyIdentity); ok {
		if s, ok := identity.(string); ok {
			return s
		}
	}
	return ""
}

// GetMethod retrieves the authentication method from the context.
// Returns empty string if not authenticated.
// Possible values: "api_key", "bearer".
func GetMethod(c *rig.Context) string {
	if method, ok := c.Get(ContextKeyMethod); ok {
		if s, ok := method.(string); ok {
			return s
		}
	}
	return ""
}

// IsAuthenticated returns true if the request has been authenticated.
func IsAuthenticated(c *rig.Context) bool {
	_, ok := c.Get(ContextKeyIdentity)
	return ok
}
