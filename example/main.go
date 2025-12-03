// Example demonstrates how to use the rig HTTP library.
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/cloudresty/rig"
)

// User represents a simple user struct for demonstration.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Response is a generic API response wrapper.
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Database represents a simulated database connection.
// In a real application, this would be your actual database client.
type Database struct {
	ConnectionString string
	connected        bool
}

// NewDatabase creates a new Database instance.
func NewDatabase(connStr string) *Database {
	return &Database{
		ConnectionString: connStr,
		connected:        true,
	}
}

// Alive checks if the database connection is healthy.
func (db *Database) Alive() bool {
	return db.connected
}

// FindUser simulates finding a user by ID.
func (db *Database) FindUser(id string) (*User, error) {
	// Simulated database lookup
	return &User{
		ID:    1,
		Name:  "John Doe (from DB)",
		Email: "john@example.com",
	}, nil
}

// WithDatabase is a middleware that injects a Database into the context.
// This demonstrates the dependency injection pattern.
func WithDatabase(db *Database) rig.MiddlewareFunc {
	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			c.Set("db", db)
			return next(c)
		}
	}
}

// Logger is a middleware that logs request information.
func Logger() rig.MiddlewareFunc {
	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			start := time.Now()

			// Call the next handler
			err := next(c)

			// Log after the request is complete
			log.Printf("[%s] %s - %v", c.Method(), c.Path(), time.Since(start))

			return err
		}
	}
}

// RequestID is a middleware that adds a request ID to the context.
func RequestID() rig.MiddlewareFunc {
	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			// Generate a simple request ID (in production, use UUID)
			requestID := fmt.Sprintf("req-%d", time.Now().UnixNano())
			c.Set("requestID", requestID)
			c.SetHeader("X-Request-ID", requestID)
			return next(c)
		}
	}
}

func main() {
	// Create a simulated database connection
	db := NewDatabase("postgres://localhost:5432/myapp")

	// Create a new rig router
	r := rig.New()

	// Register global middleware (applied to all routes)
	r.Use(Logger())         // Log all requests
	r.Use(RequestID())      // Add request ID to all requests
	r.Use(WithDatabase(db)) // Inject database into all requests

	// Set a custom error handler (optional)
	r.SetErrorHandler(func(c *rig.Context, err error) {
		// Access request ID from context for error tracking
		requestID, _ := c.Get("requestID")
		log.Printf("[ERROR] RequestID=%v: %v", requestID, err)

		c.JSON(http.StatusInternalServerError, Response{
			Success: false,
			Message: err.Error(),
		})
	})

	// GET /health - Health check using injected database
	r.GET("/health", func(c *rig.Context) error {
		// Retrieve the database from context using MustGet
		database := c.MustGet("db").(*Database)

		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "OK",
			Data: map[string]any{
				"database_alive": database.Alive(),
				"request_id":     c.MustGet("requestID"),
			},
		})
	})

	// GET /users/{id} - Get user by ID using database from context
	r.GET("/users/{id}", func(c *rig.Context) error {
		id := c.Param("id")

		// Get database from context (injected by middleware)
		database := c.MustGet("db").(*Database)

		// Use the database to find the user
		user, err := database.FindUser(id)
		if err != nil {
			return err
		}

		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: fmt.Sprintf("User found with ID: %s", id),
			Data:    user,
		})
	})

	// POST /users - Create a new user (demonstrates Bind and JSON)
	r.POST("/users", func(c *rig.Context) error {
		var user User

		// Bind the request body to the User struct
		if err := c.Bind(&user); err != nil {
			return c.JSON(http.StatusBadRequest, Response{
				Success: false,
				Message: "Invalid request body: " + err.Error(),
			})
		}

		// Validate required fields
		if user.Name == "" {
			return c.JSON(http.StatusBadRequest, Response{
				Success: false,
				Message: "Name is required",
			})
		}

		// Simulate assigning an ID
		user.ID = 42

		return c.JSON(http.StatusCreated, Response{
			Success: true,
			Message: "User created successfully",
			Data:    user,
		})
	})

	// PUT /users/{id} - Update a user
	r.PUT("/users/{id}", func(c *rig.Context) error {
		id := c.Param("id")

		var user User
		if err := c.Bind(&user); err != nil {
			return c.JSON(http.StatusBadRequest, Response{
				Success: false,
				Message: "Invalid request body",
			})
		}

		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: fmt.Sprintf("User %s updated", id),
			Data:    user,
		})
	})

	// DELETE /users/{id} - Delete a user
	r.DELETE("/users/{id}", func(c *rig.Context) error {
		id := c.Param("id")

		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: fmt.Sprintf("User %s deleted", id),
		})
	})

	// Example of returning an error (will trigger error handler)
	r.GET("/error", func(c *rig.Context) error {
		return errors.New("something went wrong")
	})

	// Example using route groups with group-specific middleware
	api := r.Group("/api/v1")

	// Add group-specific middleware (in addition to global middleware)
	api.Use(func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			c.Set("api_version", "v1")
			return next(c)
		}
	})

	api.GET("/status", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "API v1 is running",
			Data: map[string]any{
				"version":    c.MustGet("api_version"),
				"request_id": c.MustGet("requestID"),
			},
		})
	})

	// Start the server
	addr := ":8080"
	log.Printf("Starting rig server on %s", addr)
	log.Printf("Try: curl http://localhost%s/health", addr)
	log.Printf("Try: curl http://localhost%s/users/123", addr)
	log.Printf("Try: curl http://localhost%s/api/v1/status", addr)

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
