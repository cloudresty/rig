package rig

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// CheckFunc is a function that returns nil if healthy, or an error if unhealthy.
type CheckFunc func() error

// CheckFuncContext is a function that accepts a context for cancellation/timeout support.
// Use this for health checks that make external calls (database pings, HTTP requests).
type CheckFuncContext func(ctx context.Context) error

// HealthConfig defines configuration for the Health manager.
type HealthConfig struct {
	// CheckTimeout is the maximum duration allowed for each individual health check.
	// If a check exceeds this timeout, it is considered failed.
	// Default: 5 seconds.
	CheckTimeout time.Duration

	// Parallel controls whether checks run concurrently.
	// When true, all checks run in parallel (faster but uses more resources).
	// When false, checks run sequentially (slower but predictable resource usage).
	// Default: false (sequential).
	Parallel bool
}

// DefaultHealthConfig returns production-safe default configuration.
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		CheckTimeout: 5 * time.Second,
		Parallel:     false,
	}
}

// healthCheck represents a registered health check with its configuration.
type healthCheck struct {
	name    string
	check   CheckFunc
	checkFn CheckFuncContext
	timeout time.Duration // per-check timeout override (0 = use global)
}

// Health manages liveness and readiness probes.
type Health struct {
	mu        sync.RWMutex
	readiness []healthCheck
	liveness  []healthCheck
	config    HealthConfig
}

// NewHealth creates a new Health manager with default configuration.
func NewHealth() *Health {
	return NewHealthWithConfig(DefaultHealthConfig())
}

// NewHealthWithConfig creates a new Health manager with custom configuration.
func NewHealthWithConfig(config HealthConfig) *Health {
	return &Health{
		readiness: make([]healthCheck, 0),
		liveness:  make([]healthCheck, 0),
		config:    config,
	}
}

// AddReadinessCheck adds a check that must pass for the app to receive traffic
// (e.g., database connection, Redis, upstream API).
func (h *Health) AddReadinessCheck(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readiness = append(h.readiness, healthCheck{name: name, check: check})
}

// AddReadinessCheckContext adds a context-aware readiness check.
// Use this for checks that make external calls and should respect timeouts.
//
// Example:
//
//	health.AddReadinessCheckContext("database", func(ctx context.Context) error {
//	    return db.PingContext(ctx)
//	})
func (h *Health) AddReadinessCheckContext(name string, check CheckFuncContext) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readiness = append(h.readiness, healthCheck{name: name, checkFn: check})
}

// AddReadinessCheckWithTimeout adds a readiness check with a custom timeout.
// This overrides the global CheckTimeout for this specific check.
func (h *Health) AddReadinessCheckWithTimeout(name string, timeout time.Duration, check CheckFuncContext) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readiness = append(h.readiness, healthCheck{name: name, checkFn: check, timeout: timeout})
}

// AddLivenessCheck adds a check that determines if the app is running
// (usually just a simple ping, or checking for deadlocks).
func (h *Health) AddLivenessCheck(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveness = append(h.liveness, healthCheck{name: name, check: check})
}

// AddLivenessCheckContext adds a context-aware liveness check.
// Use this for checks that make external calls and should respect timeouts.
func (h *Health) AddLivenessCheckContext(name string, check CheckFuncContext) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveness = append(h.liveness, healthCheck{name: name, checkFn: check})
}

// AddLivenessCheckWithTimeout adds a liveness check with a custom timeout.
// This overrides the global CheckTimeout for this specific check.
func (h *Health) AddLivenessCheckWithTimeout(name string, timeout time.Duration, check CheckFuncContext) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveness = append(h.liveness, healthCheck{name: name, checkFn: check, timeout: timeout})
}

// LiveHandler returns a Rig HandlerFunc for liveness probes.
func (h *Health) LiveHandler() HandlerFunc {
	return h.handle(&h.liveness)
}

// ReadyHandler returns a Rig HandlerFunc for readiness probes.
func (h *Health) ReadyHandler() HandlerFunc {
	return h.handle(&h.readiness)
}

// checkResult holds the result of a single health check.
type checkResult struct {
	name   string
	status string
	failed bool
}

func (h *Health) handle(checks *[]healthCheck) HandlerFunc {
	return func(c *Context) error {
		h.mu.RLock()
		checksCopy := make([]healthCheck, len(*checks))
		copy(checksCopy, *checks)
		h.mu.RUnlock()

		status := http.StatusOK
		response := make(map[string]string)

		if h.config.Parallel {
			// Run checks in parallel
			results := make(chan checkResult, len(checksCopy))

			for _, hc := range checksCopy {
				go func(hc healthCheck) {
					result := h.runCheck(c.Context(), hc)
					results <- result
				}(hc)
			}

			// Collect results
			for range checksCopy {
				result := <-results
				response[result.name] = result.status
				if result.failed {
					status = http.StatusServiceUnavailable
				}
			}
		} else {
			// Run checks sequentially
			for _, hc := range checksCopy {
				result := h.runCheck(c.Context(), hc)
				response[result.name] = result.status
				if result.failed {
					status = http.StatusServiceUnavailable
				}
			}
		}

		return c.JSON(status, map[string]any{
			"status": http.StatusText(status),
			"checks": response,
		})
	}
}

// runCheck executes a single health check with timeout support.
func (h *Health) runCheck(parentCtx context.Context, hc healthCheck) checkResult {
	// Determine timeout for this check
	timeout := h.config.CheckTimeout
	if hc.timeout > 0 {
		timeout = hc.timeout
	}

	// If it's a simple CheckFunc (no context), just run it directly
	if hc.check != nil {
		// Still apply timeout by running in goroutine
		done := make(chan error, 1)
		go func() {
			done <- hc.check()
		}()

		select {
		case err := <-done:
			if err != nil {
				return checkResult{name: hc.name, status: "FAIL: " + err.Error(), failed: true}
			}
			return checkResult{name: hc.name, status: "OK", failed: false}
		case <-time.After(timeout):
			return checkResult{name: hc.name, status: "FAIL: check timed out", failed: true}
		}
	}

	// Context-aware check
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- hc.checkFn(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			return checkResult{name: hc.name, status: "FAIL: " + err.Error(), failed: true}
		}
		return checkResult{name: hc.name, status: "OK", failed: false}
	case <-ctx.Done():
		return checkResult{name: hc.name, status: "FAIL: check timed out", failed: true}
	}
}
