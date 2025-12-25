// Package logger provides HTTP request logging middleware for the rig framework.
//
// The logger middleware records information about each HTTP request including:
//   - HTTP method and path
//   - Response status code
//   - Request latency
//   - Client IP address
//   - Request ID (if available from requestid middleware)
//
// # Basic Usage
//
//	r := rig.New()
//	r.Use(logger.New())
//
// # With Custom Configuration
//
//	r.Use(logger.New(logger.Config{
//	    Format:    logger.FormatJSON,
//	    SkipPaths: []string{"/health", "/ready"},
//	    Output:    os.Stderr,
//	}))
//
// # Output Formats
//
// The middleware supports two output formats:
//   - FormatText (default): Human-readable text format
//   - FormatJSON: Structured JSON format for log aggregation systems
//
// # Status Code Tracking
//
// Note: Due to the design of the rig framework, the logger cannot capture
// the exact HTTP status code. It infers the status based on whether an
// error was returned from the handler:
//   - No error: 200 OK
//   - Error returned: 500 Internal Server Error
//
// For more accurate status tracking, consider using a custom response writer
// wrapper in your application.
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/requestid"
)

// Format represents the log output format.
type Format string

const (
	// FormatText outputs logs in human-readable text format.
	// Example: 2024-01-15 10:30:45 | 200 |   1.234ms | 192.168.1.1 | GET /api/users
	FormatText Format = "text"

	// FormatJSON outputs logs in structured JSON format.
	// Useful for log aggregation systems like ELK, Splunk, or CloudWatch.
	FormatJSON Format = "json"
)

// Config defines the configuration for the logger middleware.
type Config struct {
	// Format specifies the log output format.
	// Default: FormatText
	Format Format

	// Output is the writer where logs will be written.
	// Default: os.Stdout
	Output io.Writer

	// SkipPaths is a list of URL paths that should not be logged.
	// Useful for health check endpoints that are called frequently.
	// Example: []string{"/health", "/ready", "/metrics"}
	SkipPaths []string

	// TimeFormat specifies the format for timestamps.
	// Default: "2006-01-02 15:04:05"
	TimeFormat string
}

// LogEntry represents a single log entry in JSON format.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Status    int    `json:"status"`
	Latency   string `json:"latency"`
	LatencyMs int64  `json:"latency_ms"`
	ClientIP  string `json:"client_ip"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	RequestID string `json:"request_id,omitempty"`
	Error     string `json:"error,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

// New creates a new logger middleware with the given configuration.
//
// The middleware logs each request after it completes, including:
//   - Timestamp
//   - HTTP status code (inferred from error)
//   - Request latency
//   - Client IP address
//   - HTTP method and path
//   - Request ID (if requestid middleware is used)
func New(config ...Config) rig.MiddlewareFunc {
	// Apply defaults
	cfg := Config{}
	if len(config) > 0 {
		cfg = config[0]
	}

	if cfg.Format == "" {
		cfg.Format = FormatText
	}

	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}

	if cfg.TimeFormat == "" {
		cfg.TimeFormat = "2006-01-02 15:04:05"
	}

	// Build skip paths map for O(1) lookup
	skipPaths := make(map[string]bool)
	for _, path := range cfg.SkipPaths {
		skipPaths[path] = true
	}

	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			// Check if path should be skipped
			if skipPaths[c.Path()] {
				return next(c)
			}

			start := time.Now()

			// Execute the handler
			err := next(c)

			// Calculate latency
			latency := time.Since(start)

			// Get request ID if available
			reqID := requestid.Get(c)

			// Get client IP
			clientIP := getClientIP(c)

			// Infer status code from error
			status := 200
			if err != nil {
				status = 500
			}

			// Build log entry
			entry := LogEntry{
				Timestamp: time.Now().Format(cfg.TimeFormat),
				Status:    status,
				Latency:   formatLatency(latency),
				LatencyMs: latency.Milliseconds(),
				ClientIP:  clientIP,
				Method:    c.Method(),
				Path:      c.Path(),
				RequestID: reqID,
				UserAgent: c.GetHeader("User-Agent"),
			}

			if err != nil {
				entry.Error = err.Error()
			}

			// Write log
			switch cfg.Format {
			case FormatJSON:
				writeJSON(cfg.Output, entry)
			default:
				writeText(cfg.Output, entry)
			}

			return err
		}
	}
}

// writeText writes a log entry in text format.
func writeText(w io.Writer, entry LogEntry) {
	// Format: timestamp | status | latency | client_ip | method path [request_id]
	line := fmt.Sprintf("%s | %3d | %10s | %15s | %s %s",
		entry.Timestamp,
		entry.Status,
		entry.Latency,
		entry.ClientIP,
		entry.Method,
		entry.Path,
	)

	if entry.RequestID != "" {
		line += fmt.Sprintf(" [%s]", entry.RequestID)
	}

	if entry.Error != "" {
		line += fmt.Sprintf(" | error: %s", entry.Error)
	}

	_, _ = fmt.Fprintln(w, line)
}

// writeJSON writes a log entry in JSON format.
func writeJSON(w io.Writer, entry LogEntry) {
	_ = json.NewEncoder(w).Encode(entry)
}

// formatLatency formats a duration for display.
func formatLatency(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1000000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// getClientIP extracts the client IP address from the request.
// It checks common proxy headers first, then falls back to RemoteAddr.
func getClientIP(c *rig.Context) string {
	// Check X-Forwarded-For header (common for proxies)
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP header (nginx)
	if xri := c.GetHeader("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	addr := c.Request().RemoteAddr
	// Remove port if present
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
