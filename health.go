package rig

import (
	"net/http"
	"sync"
)

// CheckFunc is a function that returns nil if healthy, or an error if unhealthy.
type CheckFunc func() error

// Health manages liveness and readiness probes.
type Health struct {
	mu        sync.RWMutex
	readiness map[string]CheckFunc
	liveness  map[string]CheckFunc
}

// NewHealth creates a new Health manager.
func NewHealth() *Health {
	return &Health{
		readiness: make(map[string]CheckFunc),
		liveness:  make(map[string]CheckFunc),
	}
}

// AddReadinessCheck adds a check that must pass for the app to receive traffic
// (e.g., database connection, Redis, upstream API).
func (h *Health) AddReadinessCheck(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readiness[name] = check
}

// AddLivenessCheck adds a check that determines if the app is running
// (usually just a simple ping, or checking for deadlocks).
func (h *Health) AddLivenessCheck(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveness[name] = check
}

// LiveHandler returns a Rig HandlerFunc for liveness probes.
func (h *Health) LiveHandler() HandlerFunc {
	return h.handle(&h.liveness)
}

// ReadyHandler returns a Rig HandlerFunc for readiness probes.
func (h *Health) ReadyHandler() HandlerFunc {
	return h.handle(&h.readiness)
}

func (h *Health) handle(checks *map[string]CheckFunc) HandlerFunc {
	return func(c *Context) error {
		h.mu.RLock()
		defer h.mu.RUnlock()

		status := http.StatusOK
		response := make(map[string]string)

		// Run all checks
		for name, check := range *checks {
			if err := check(); err != nil {
				status = http.StatusServiceUnavailable
				response[name] = "FAIL: " + err.Error()
			} else {
				response[name] = "OK"
			}
		}

		return c.JSON(status, map[string]any{
			"status": http.StatusText(status),
			"checks": response,
		})
	}
}
