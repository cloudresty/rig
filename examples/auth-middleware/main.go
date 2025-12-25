// Example demonstrates how to use the auth package for API key authentication with rig.
//
// This example shows:
//   - Using the built-in auth.APIKey middleware
//   - Protecting routes with API key authentication
//   - Accessing authentication info from the context
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/auth"
)

// Response is a generic API response wrapper.
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

func main() {
	// In production, load these from environment variables or a secrets manager
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "my-secret-api-key-12345" // Default for demo only
	}

	r := rig.New()

	// Public routes (no authentication required)
	r.GET("/", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "Welcome! This is a public endpoint.",
		})
	})

	r.GET("/health", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "OK",
		})
	})

	// Protected routes group - requires API key authentication
	api := r.Group("/api")
	api.Use(auth.APIKeySimple(apiKey)) // Simple API key validation

	api.GET("/profile", func(c *rig.Context) error {
		// Get the authenticated identity from context (set by auth middleware)
		identity := auth.GetIdentity(c)
		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "You have access to the protected profile endpoint!",
			Data: map[string]any{
				"user":     identity,
				"auth_via": auth.GetMethod(c),
			},
		})
	})

	api.GET("/secrets", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "Here are your secrets!",
			Data: map[string]string{
				"secret1": "The cake is a lie",
				"secret2": "42 is the answer",
			},
		})
	})

	// Start the server
	addr := ":8080"
	log.Printf("Starting server on %s", addr)
	log.Println()
	log.Println("Public endpoints (no auth required):")
	log.Printf("  curl http://localhost%s/", addr)
	log.Printf("  curl http://localhost%s/health", addr)
	log.Println()
	log.Println("Protected endpoints (require X-API-Key header):")
	log.Printf("  curl http://localhost%s/api/profile", addr)
	log.Printf("  curl -H 'X-API-Key: %s' http://localhost%s/api/profile", apiKey, addr)
	log.Printf("  curl -H 'X-API-Key: %s' http://localhost%s/api/secrets", apiKey, addr)

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
