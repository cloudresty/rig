// Package requestid provides middleware for generating and propagating request IDs.
//
// Request IDs are essential for debugging and tracing requests across distributed systems.
// This package uses ULID (Universally Unique Lexicographically Sortable Identifier) for
// generating request IDs, which provides both uniqueness and time-based sortability.
//
// # Basic Usage
//
//	r := rig.New()
//	r.Use(requestid.New())
//
// # With Custom Configuration
//
//	r.Use(requestid.New(requestid.Config{
//	    Header:     "X-Correlation-ID",
//	    TrustProxy: true,  // Use incoming header if present
//	}))
//
// # Security Considerations
//
// When TrustProxy is enabled, the middleware will use an existing request ID from the
// incoming request header if present. This is useful for distributed tracing where
// request IDs need to be propagated across services.
//
// However, trusting incoming headers can be a security risk if your service is directly
// exposed to the internet without a trusted proxy. An attacker could:
//   - Inject misleading request IDs to confuse log analysis
//   - Use predictable IDs to correlate requests they shouldn't be able to link
//
// Only enable TrustProxy when:
//   - Your service is behind a trusted reverse proxy (nginx, Envoy, etc.)
//   - You need to preserve request IDs across service boundaries
//   - You trust the upstream services setting the header
package requestid

import (
	"github.com/cloudresty/rig"
	"github.com/cloudresty/ulid"
)

// Default values for the middleware configuration.
const (
	// DefaultHeader is the default HTTP header name for the request ID.
	DefaultHeader = "X-Request-ID"

	// ContextKey is the key used to store the request ID in the context.
	ContextKey = "request_id"
)

// Config defines the configuration for the request ID middleware.
type Config struct {
	// Header is the HTTP header name to use for the request ID.
	// Default: "X-Request-ID".
	Header string

	// Generator is a custom function to generate request IDs.
	// Default: uses cloudresty/ulid.New().
	// If the generator returns an error, it will be logged and a fallback ID will be used.
	Generator func() (string, error)

	// TrustProxy, when true, will use the request ID from the incoming request header
	// if present, instead of generating a new one.
	//
	// WARNING: Only enable this if your service is behind a trusted proxy.
	// See package documentation for security considerations.
	//
	// Default: false (always generate new IDs).
	TrustProxy bool
}

// New creates request ID middleware that assigns a unique ID to each request.
//
// The request ID is:
//   - Set as a response header (so clients can reference it)
//   - Stored in the request context (accessible via requestid.Get(c) or c.Get(requestid.ContextKey))
//
// By default, IDs are generated using ULID, which provides:
//   - 128-bit uniqueness (same as UUID)
//   - Lexicographically sortable (time-ordered)
//   - URL-safe (no special characters)
//   - 26 characters (shorter than UUID's 36)
func New(config ...Config) rig.MiddlewareFunc {
	// Apply defaults
	cfg := Config{}
	if len(config) > 0 {
		cfg = config[0]
	}

	if cfg.Header == "" {
		cfg.Header = DefaultHeader
	}

	if cfg.Generator == nil {
		cfg.Generator = ulid.New
	}

	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			var requestID string

			// If TrustProxy is enabled, check for existing request ID
			if cfg.TrustProxy {
				requestID = c.GetHeader(cfg.Header)
			}

			// Generate new ID if not trusting proxy or no ID present
			if requestID == "" {
				var err error
				requestID, err = cfg.Generator()
				if err != nil {
					// Fallback: try generating again, or use a simple fallback
					requestID, err = ulid.New()
					if err != nil {
						// Last resort fallback - this should rarely happen
						requestID = "fallback-request-id"
					}
				}
			}

			// Set request ID in response header and context
			c.SetHeader(cfg.Header, requestID)
			c.Set(ContextKey, requestID)

			return next(c)
		}
	}
}

// Get retrieves the request ID from the context.
// Returns an empty string if no request ID is present.
func Get(c *rig.Context) string {
	if id, ok := c.Get(ContextKey); ok {
		if s, ok := id.(string); ok {
			return s
		}
	}
	return ""
}

