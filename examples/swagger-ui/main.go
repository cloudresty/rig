// Example demonstrates how to integrate Swagger UI with Rig.
//
// To run this example:
//
//	cd examples/swagger-ui
//	go run main.go
//
// Then open http://localhost:8080/docs/ in your browser.
package main

import (
	"log"
	"net/http"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/swagger"
)

// User represents a user in the system.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Response is a generic API response.
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// OpenAPI specification for the example API.
// In a real project, you would generate this using swaggo/swag:
//
//	swag init
//
// Then import your docs package and use swagger.NewFromSwag("swagger")
const apiSpec = `{
  "openapi": "3.0.0",
  "info": {
    "title": "Rig Example API",
    "description": "A simple example API demonstrating Rig with Swagger UI",
    "version": "1.0.0",
    "contact": {
      "name": "Cloudresty",
      "url": "https://cloudresty.com"
    }
  },
  "servers": [
    { "url": "http://localhost:8080", "description": "Local development" }
  ],
  "paths": {
    "/api/v1/users": {
      "get": {
        "summary": "List all users",
        "tags": ["Users"],
        "responses": {
          "200": {
            "description": "Successful response",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Response" }
              }
            }
          }
        }
      },
      "post": {
        "summary": "Create a new user",
        "tags": ["Users"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/User" }
            }
          }
        },
        "responses": {
          "201": { "description": "User created" }
        }
      }
    },
    "/api/v1/users/{id}": {
      "get": {
        "summary": "Get user by ID",
        "tags": ["Users"],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": { "type": "integer" }
          }
        ],
        "responses": {
          "200": { "description": "User found" },
          "404": { "description": "User not found" }
        }
      }
    },
    "/health": {
      "get": {
        "summary": "Health check",
        "tags": ["System"],
        "responses": {
          "200": { "description": "Service is healthy" }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "User": {
        "type": "object",
        "properties": {
          "id": { "type": "integer" },
          "name": { "type": "string" },
          "email": { "type": "string", "format": "email" }
        }
      },
      "Response": {
        "type": "object",
        "properties": {
          "success": { "type": "boolean" },
          "message": { "type": "string" },
          "data": { "type": "object" }
        }
      }
    }
  }
}`

func main() {
	r := rig.New()

	// Health check endpoint
	r.GET("/health", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, Response{Success: true, Message: "OK"})
	})

	// API routes
	api := r.Group("/api/v1")

	api.GET("/users", func(c *rig.Context) error {
		users := []User{
			{ID: 1, Name: "Alice", Email: "alice@example.com"},
			{ID: 2, Name: "Bob", Email: "bob@example.com"},
		}
		return c.JSON(http.StatusOK, Response{Success: true, Data: users})
	})

	api.GET("/users/{id}", func(c *rig.Context) error {
		id := c.Param("id")
		user := User{ID: 1, Name: "Alice", Email: "alice@example.com"}
		return c.JSON(http.StatusOK, Response{
			Success: true,
			Message: "User " + id,
			Data:    user,
		})
	})

	api.POST("/users", func(c *rig.Context) error {
		var user User
		if err := c.Bind(&user); err != nil {
			return c.JSON(http.StatusBadRequest, Response{Success: false, Message: err.Error()})
		}
		user.ID = 42
		return c.JSON(http.StatusCreated, Response{Success: true, Data: user})
	})

	// Register Swagger UI at /docs/
	sw := swagger.New(apiSpec).
		WithTitle("Rig Example API").
		WithDocExpansion("list")
	sw.Register(r, "/docs")

	// Start server
	addr := ":8080"
	log.Printf("🚀 Server running on http://localhost%s", addr)
	log.Printf("📚 Swagger UI available at http://localhost%s/docs/", addr)

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
