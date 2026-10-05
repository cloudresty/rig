// Package health provides tri-state, cache-backed liveness, readiness and
// startup probes for Kubernetes-style deployments.
//
// It supersedes the root rig.Health manager, which is binary, sequential and
// runs every check inline inside the probe request (one slow dependency can
// push a probe past the kubelet timeout). rig.Health is kept unchanged for
// existing users; new code should use this package.
//
// # Model
//
// A Registry owns a set of named checks. Each check is evaluated by its own
// background evaluator goroutine on its own interval and timeout; the HTTP
// handlers only read the last cached evaluation, so a probe is O(1) and never
// waits on a dependency. A check whose last evaluation is older than three
// times its interval is reported as FAIL "stale", which turns a wedged
// evaluator into a visible signal.
//
// Results have three levels: OK, Degraded and Failed. Degraded never changes
// the HTTP status (it is 200 with a detail line). Only Gating checks can fail a
// probe, and only after Grace consecutive failures; Informational checks are
// reported but never affect the status.
//
// # Liveness is restricted to in-process facts
//
// A restart cures a wedge in this process; it never cures a database or broker
// outage, and restarting every replica during an outage makes it worse.
// Registry.RegisterLiveness therefore accepts only InProcessCheck, a type that
// cannot be built from an arbitrary I/O function: it is obtained from
// Watchdog.Liveness or FromFacts. A liveness FAIL is additionally held for a
// per-pod jitter (see WithLivenessHold) so replicas do not restart together.
//
// # Probe bodies are scrubbed
//
// A health body is a security boundary. Every string rendered into
// /health/live, /health/ready and /health/startup (check names, details, error
// text) is passed through ScrubCredentials, always, at the render layer, so a
// URI carrying a password never reaches a probe whatever check produced it.
// WithScrubber adds extra patterns after the built-in rules; nothing disables
// them.
//
// This package imports nothing outside the standard library and the root rig
// package (for Adapt), and nothing about brokers or databases: callers map
// their own client's verdict to a Level, typically through FromFacts.
package health
