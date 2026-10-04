package health

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cloudresty/rig"
)

// wireBody is the JSON body. It is a superset of the root rig.Health shape
// ({"status","checks"}): "informational", "details", "held" and "evaluatedAt"
// are additions, and a passing check is still the bare string "OK".
type wireBody struct {
	Status        string            `json:"status"`
	Checks        map[string]string `json:"checks"`
	Informational map[string]string `json:"informational"`
	Details       map[string]string `json:"details,omitempty"`
	Held          bool              `json:"held,omitempty"`
	EvaluatedAt   string            `json:"evaluatedAt"`
}

func render(s Snapshot) wireBody {
	b := wireBody{
		Status:        s.Status.String(),
		Checks:        make(map[string]string, len(s.Checks)),
		Informational: make(map[string]string, len(s.Informational)),
		Held:          s.Held,
		EvaluatedAt:   s.EvaluatedAt.UTC().Format(time.RFC3339),
	}
	for _, m := range []struct {
		src map[string]CheckState
		dst map[string]string
	}{{s.Checks, b.Checks}, {s.Informational, b.Informational}} {
		for name, st := range m.src {
			m.dst[name] = line(st)
			if st.Level == OK && st.Detail != "" {
				if b.Details == nil {
					b.Details = map[string]string{}
				}
				b.Details[name] = st.Detail
			}
		}
	}
	return b
}

func line(st CheckState) string {
	if st.Level == OK || st.Detail == "" {
		return st.Level.String()
	}
	return st.Level.String() + ": " + st.Detail
}

func write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (r *Registry) handler(scope Scope) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// The snapshot only reads cached state and calls no user code, so it
		// cannot legitimately block. The budget is a backstop: a probe always
		// answers, and a stuck read is reported as a FAIL, not as a timeout.
		ch := make(chan Snapshot, 1)
		go func() { ch <- r.snap(scope) }()
		timer := time.NewTimer(r.budget)
		defer timer.Stop()
		select {
		case s := <-ch:
			write(w, s.Code, render(s))
		case <-timer.C:
			write(w, http.StatusServiceUnavailable, wireBody{
				Status:        Failed.String(),
				Checks:        map[string]string{"probe": "FAIL: probe budget exceeded"},
				Informational: map[string]string{},
				EvaluatedAt:   r.now().UTC().Format(time.RFC3339),
			})
		case <-req.Context().Done():
		}
	}
}

// LiveHandler serves the liveness probe from cached results.
func (r *Registry) LiveHandler() http.HandlerFunc { return r.handler(Liveness) }

// ReadyHandler serves the readiness probe from cached results.
func (r *Registry) ReadyHandler() http.HandlerFunc { return r.handler(Readiness) }

// StartupHandler serves the startup probe from cached results.
func (r *Registry) StartupHandler() http.HandlerFunc { return r.handler(Startup) }

// Adapt converts a probe handler to a rig.HandlerFunc so it can be mounted on
// a rig.Router: r.GET("/health/ready", health.Adapt(reg.ReadyHandler())).
func Adapt(h http.Handler) rig.HandlerFunc {
	return func(c *rig.Context) error {
		h.ServeHTTP(c.Writer(), c.Request())
		return nil
	}
}
