// Example demonstrates Rig's health check feature for Kubernetes probes.
//
// To run this example:
//
//	cd examples/health-checks
//	go run main.go
//
// Then test the endpoints:
//
//	curl http://localhost:8080/health/live
//	curl http://localhost:8080/health/ready
package main

import (
	"errors"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/cloudresty/rig"
)

// Simulated dependencies
type Database struct {
	connected atomic.Bool
}

func (db *Database) Ping() error {
	if !db.connected.Load() {
		return errors.New("database connection lost")
	}
	return nil
}

type Cache struct {
	available atomic.Bool
}

func (c *Cache) Ping() error {
	if !c.available.Load() {
		return errors.New("cache unavailable")
	}
	return nil
}

func main() {
	// Initialize simulated dependencies
	db := &Database{}
	db.connected.Store(true)

	cache := &Cache{}
	cache.available.Store(true)

	// Create router and health manager
	r := rig.New()
	health := rig.NewHealth()

	// Liveness checks - is the process alive?
	// Keep these simple. If they fail, Kubernetes will restart the pod.
	health.AddLivenessCheck("goroutine", func() error {
		// Example: check for deadlock or excessive goroutines
		return nil // Always healthy in this demo
	})

	// Readiness checks - can we serve traffic?
	// These determine if the pod should receive requests.
	health.AddReadinessCheck("database", db.Ping)
	health.AddReadinessCheck("cache", cache.Ping)

	// Register health endpoints (Kubernetes standard paths)
	r.GET("/health/live", health.LiveHandler())
	r.GET("/health/ready", health.ReadyHandler())

	// Demo endpoint
	r.GET("/", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"message": "Hello! Try /health/live and /health/ready",
		})
	})

	// Admin endpoints to toggle dependency health (for demo purposes)
	r.POST("/admin/db/down", func(c *rig.Context) error {
		db.connected.Store(false)
		log.Println("⚠️  Database marked as DOWN")
		return c.JSON(http.StatusOK, map[string]string{"database": "down"})
	})

	r.POST("/admin/db/up", func(c *rig.Context) error {
		db.connected.Store(true)
		log.Println("✅ Database marked as UP")
		return c.JSON(http.StatusOK, map[string]string{"database": "up"})
	})

	r.POST("/admin/cache/down", func(c *rig.Context) error {
		cache.available.Store(false)
		log.Println("⚠️  Cache marked as DOWN")
		return c.JSON(http.StatusOK, map[string]string{"cache": "down"})
	})

	r.POST("/admin/cache/up", func(c *rig.Context) error {
		cache.available.Store(true)
		log.Println("✅ Cache marked as UP")
		return c.JSON(http.StatusOK, map[string]string{"cache": "up"})
	})

	// Start server
	addr := ":8080"
	log.Printf("🚀 Server running on http://localhost%s", addr)
	log.Println()
	log.Println("Health endpoints:")
	log.Printf("  curl http://localhost%s/health/live", addr)
	log.Printf("  curl http://localhost%s/health/ready", addr)
	log.Println()
	log.Println("Toggle dependencies (for testing):")
	log.Printf("  curl -X POST http://localhost%s/admin/db/down", addr)
	log.Printf("  curl -X POST http://localhost%s/admin/db/up", addr)
	log.Printf("  curl -X POST http://localhost%s/admin/cache/down", addr)
	log.Printf("  curl -X POST http://localhost%s/admin/cache/up", addr)

	// Simulate cache going down after 30 seconds (optional demo)
	go func() {
		time.Sleep(30 * time.Second)
		cache.available.Store(false)
		log.Println("⚠️  [Demo] Cache went down after 30s - /health/ready will now fail")
	}()

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

