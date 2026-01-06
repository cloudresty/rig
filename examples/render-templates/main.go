// Example demonstrates how to use the rig/render package for HTML templates.
//
// Features demonstrated:
//   - Layouts with {{.Content}} and {{.Data}} access
//   - Partials (templates starting with _)
//   - Content negotiation (HTML/JSON/XML based on Accept header)
//   - Custom template functions
//   - DevMode hot reloading
//   - Error page fallback with HTMLSafe
//   - Debug helper with dump function
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/render"
)

// User represents a user for demonstration.
type User struct {
	ID    int    `json:"id" xml:"id"`
	Name  string `json:"name" xml:"name"`
	Email string `json:"email" xml:"email"`
}

// AppInfo demonstrates using a struct (not map) with layouts.
type AppInfo struct {
	Title       string
	Name        string
	Version     string
	Description string
}

// Sample data
var users = []User{
	{ID: 1, Name: "Alice Johnson", Email: "alice@example.com"},
	{ID: 2, Name: "Bob Smith", Email: "bob@example.com"},
	{ID: 3, Name: "Carol Williams", Email: "carol@example.com"},
}

func main() {
	// Create the render engine with configuration
	engine := render.New(render.Config{
		Directory: "./templates",  // Template directory
		Layout:    "layouts/base", // Base layout for all pages
		DevMode:   true,           // Hot reload templates on each request
	})

	// Add custom template functions (optional)
	engine.AddFunc("formatDate", func(t time.Time) string {
		return t.Format("Monday, January 2, 2006 at 3:04 PM")
	})

	// Create the rig router
	r := rig.New()

	// Register render middleware (loads templates on first request)
	r.Use(engine.Middleware())

	// =========================================================================
	// HTML Routes - Using layouts and templates
	// =========================================================================

	// GET / - Home page with map data
	r.GET("/", func(c *rig.Context) error {
		return render.HTML(c, http.StatusOK, "pages/home", map[string]any{
			"Title":   "Home",
			"Message": "Welcome to the Rig Render demo!",
			"Time":    time.Now().Format(time.RFC1123),
		})
	})

	// GET /about - About page with struct data (demonstrates .Data access in layouts)
	r.GET("/about", func(c *rig.Context) error {
		info := AppInfo{
			Title:       "About",
			Name:        "Rig Render Demo",
			Version:     "1.0.0",
			Description: "A demonstration of the rig/render template engine",
		}
		return render.HTML(c, http.StatusOK, "pages/about", info)
	})

	// GET /users - Content negotiation (returns HTML, JSON, or XML based on Accept header)
	r.GET("/users", func(c *rig.Context) error {
		data := map[string]any{
			"Title": "Users",
			"Users": users,
		}
		// Auto() checks Accept header and returns appropriate format
		return render.Auto(c, http.StatusOK, "pages/users", data)
	})

	// GET /debug - Debug page demonstrating the dump helper
	r.GET("/debug", func(c *rig.Context) error {
		return render.HTML(c, http.StatusOK, "pages/debug", map[string]any{
			"Title":     "Debug",
			"Message":   "This is debug data",
			"Users":     users,
			"Timestamp": time.Now(),
			"Config": map[string]any{
				"DevMode": true,
				"Layout":  "layouts/base",
			},
		})
	})

	// GET /error-demo - Demonstrates HTMLSafe with error fallback
	r.GET("/error-demo", func(c *rig.Context) error {
		// This will fail (template doesn't exist) and fall back to error page
		return render.HTMLSafe(c, http.StatusOK, "pages/nonexistent", nil, "errors/500")
	})

	// =========================================================================
	// API Routes - JSON responses without templates
	// =========================================================================

	api := r.Group("/api")

	// GET /api/users - Pure JSON endpoint
	api.GET("/users", func(c *rig.Context) error {
		return render.JSON(c, http.StatusOK, users)
	})

	// GET /api/users/:id - JSON with path parameter
	api.GET("/users/{id}", func(c *rig.Context) error {
		id := c.Param("id")
		for _, u := range users {
			if string(rune(u.ID+'0')) == id {
				return render.JSON(c, http.StatusOK, u)
			}
		}
		return render.JSON(c, http.StatusNotFound, map[string]string{
			"error": "User not found",
		})
	})

	// =========================================================================
	// Start server
	// =========================================================================

	addr := ":8080"
	log.Println("🚀 Rig Render Demo Server")
	log.Println("=========================")
	log.Printf("Server running at http://localhost%s", addr)
	log.Println("")
	log.Println("Try these endpoints:")
	log.Printf("  http://localhost%s/           - Home page (HTML)", addr)
	log.Printf("  http://localhost%s/about      - About page (struct data)", addr)
	log.Printf("  http://localhost%s/users      - Users (content negotiation)", addr)
	log.Printf("  http://localhost%s/debug      - Debug page (dump helper)", addr)
	log.Printf("  http://localhost%s/error-demo - Error page fallback", addr)
	log.Printf("  http://localhost%s/api/users  - JSON API", addr)
	log.Println("")
	log.Println("Content negotiation example:")
	log.Printf("  curl -H 'Accept: application/json' http://localhost%s/users", addr)

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
