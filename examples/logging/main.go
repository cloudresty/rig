// Example demonstrates how to use the logger and requestid middleware with rig.
//
// This example shows:
//   - Using the requestid middleware to generate unique request IDs
//   - Using the logger middleware to log HTTP requests
//   - Configuring JSON logging format
//   - Skipping logging for health check endpoints
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/logger"
	"github.com/cloudresty/rig/requestid"
)

// Response is a generic API response wrapper.
type Response struct {
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func main() {
	r := rig.New()

	// Add request ID middleware first - it generates unique IDs for each request
	r.Use(requestid.New())

	// Add logger middleware - it logs each request with timing info
	// The logger will include the request ID in its output
	r.Use(logger.New(logger.Config{
		Format:    logger.FormatJSON, // Use JSON format for structured logging
		Output:    os.Stdout,
		SkipPaths: []string{"/health", "/ready"}, // Don't log health checks
	}))

	// Health check endpoint (not logged due to SkipPaths)
	r.GET("/health", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "OK",
		})
	})

	// API endpoints (logged with request ID)
	r.GET("/", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{
			Success:   true,
			Message:   "Welcome to the API!",
			RequestID: requestid.Get(c), // Include request ID in response
		})
	})

	r.GET("/users", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{
			Success:   true,
			Message:   "User list endpoint",
			RequestID: requestid.Get(c),
		})
	})

	r.POST("/users", func(c *rig.Context) error {
		return c.JSON(http.StatusCreated, Response{
			Success:   true,
			Message:   "User created",
			RequestID: requestid.Get(c),
		})
	})

	// Start the server
	addr := ":8080"
	log.Printf("Starting server on %s", addr)
	log.Println()
	log.Println("Try these endpoints:")
	log.Printf("  curl http://localhost%s/", addr)
	log.Printf("  curl http://localhost%s/users", addr)
	log.Printf("  curl -X POST http://localhost%s/users", addr)
	log.Printf("  curl http://localhost%s/health  (not logged)", addr)
	log.Println()
	log.Println("Watch the console for JSON-formatted request logs!")

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
