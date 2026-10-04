package health

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cloudresty/rig"
)

// wireBody is the JSON body. It is a superset of the root rig.Health shape
// ({"status","checks"}): "informational", "details", "held", "note" and "evaluatedAt" (omitted when nothing has been evaluated)
// are additions, and a passing check is still the bare string "OK".
type wireBody struct {
	Status        string            `json:"status"`
	Checks        map[string]string `json:"checks"`
	Informational map[string]string `json:"informational"`
	Details       map[string]string `json:"details,omitempty"`
	Held          bool              `json:"held,omitempty"`
	Note          string            `json:"note,omitempty"`
	EvaluatedAt   string            `json:"evaluatedAt,omitempty"`
}

func render(s Snapshot) wireBody {
	b := wireBody{
		Status:        s.Status.String(),
		Checks:        make(map[string]string, len(s.Checks)),
		Informational: make(map[string]string, len(s.Informational)),
		Held:          s.Held,
		Note:          s.Note,
	}
	if !s.EvaluatedAt.IsZero() {
		b.EvaluatedAt = s.EvaluatedAt.UTC().Format(time.RFC3339)
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
	return func(w http.ResponseWriter, _ *http.Request) {
		// Snapshot reads cached state and calls no user code, so it is
		// answered inline: it cannot block on a dependency.
		s := r.Snapshot(scope)
		write(w, s.Code, render(s))
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
