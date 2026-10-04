package health

import (
	"context"
	"fmt"
)

// Level is the verdict of a check.
type Level int

const (
	// OK means the check passed.
	OK Level = iota
	// Degraded means functional but worrying. It never changes the HTTP status.
	Degraded
	// Failed means the check failed.
	Failed
)

// String returns the wire name of the level: "OK", "DEGRADED" or "FAIL".
func (l Level) String() string {
	switch l {
	case OK:
		return "OK"
	case Degraded:
		return "DEGRADED"
	case Failed:
		return "FAIL"
	default:
		return fmt.Sprintf("Level(%d)", int(l))
	}
}

// normalise clamps an out-of-range level: below OK is OK, above Failed is Failed.
func (l Level) normalise() Level {
	if l < OK {
		return OK
	}
	if l > Failed {
		return Failed
	}
	return l
}

// Result is the outcome of one evaluation. Detail is free text; it is rendered
// for Degraded and Failed results and kept in Snapshot for OK ones.
type Result struct {
	Level  Level
	Detail string
}

// Check evaluates one aspect of health. It runs in a background evaluator with
// a timeout context, never inside a probe request. A check should honour ctx,
// but one that does not is still bounded: the registry never starts a second
// run while one is in flight.
type Check func(ctx context.Context) Result

// Kind says whether a check can affect the probe status.
type Kind int

const (
	// Gating checks can fail a probe (after their grace).
	Gating Kind = iota
	// Informational checks are reported but never affect the status.
	Informational
)

// String returns "gating" or "informational".
func (k Kind) String() string {
	if k == Informational {
		return "informational"
	}
	return "gating"
}

// Scope selects which probe a Snapshot describes.
type Scope int

const (
	// Readiness is the /ready probe: may this pod receive traffic.
	Readiness Scope = iota
	// Liveness is the /live probe: is this process wedged.
	Liveness
	// Startup is the startup probe: has wiring finished and every readiness
	// check been evaluated at least once.
	Startup
)

// String returns "readiness", "liveness" or "startup".
func (s Scope) String() string {
	switch s {
	case Readiness:
		return "readiness"
	case Liveness:
		return "liveness"
	case Startup:
		return "startup"
	default:
		return fmt.Sprintf("Scope(%d)", int(s))
	}
}
