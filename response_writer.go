package rig

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// responseWriter wraps the http.ResponseWriter handed to a request so that the
// status code actually sent can be read back afterwards (Context.StatusCode).
//
// It is installed by the router for every request, which is what makes the
// recorded status trustworthy: handlers that bypass Context's helpers and
// write through c.Writer() directly (http.Error, http.NotFound, http.ServeFile,
// a template Execute) go through it too.
//
// It deliberately implements http.Flusher, http.Hijacker, http.Pusher and
// io.ReaderFrom unconditionally, delegating to the wrapped writer, so that
// streaming (SSE), websocket upgrades and sendfile keep working through the
// wrapper. Where the wrapped writer lacks a capability the method returns
// http.ErrNotSupported (or, for Flush, does nothing), the same contract
// http.ResponseController uses. Unwrap exposes the original writer to
// http.ResponseController.
type responseWriter struct {
	http.ResponseWriter

	status   int  // first final (non-1xx) status sent; 0 until then
	hijacked bool // the connection was taken over by the handler
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

// WriteHeader records the first final status code and forwards every call.
//
// Informational 1xx codes (e.g. 103 Early Hints) may precede the final status
// and are not recorded, except 101 Switching Protocols, which is final.
// Later calls are still forwarded so net/http can report them as superfluous.
func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 && (code >= 200 || code == http.StatusSwitchingProtocols || code < 100) {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write marks an implicit 200 when no status was written first, matching what
// net/http sends in that case.
func (w *responseWriter) Write(b []byte) (int, error) {
	w.markImplicitOK()
	return w.ResponseWriter.Write(b)
}

// ReadFrom keeps the wrapped writer's io.ReaderFrom fast path (sendfile for
// http.ServeFile and io.Copy) available through the wrapper.
func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	w.markImplicitOK()
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	// writerOnly hides ReadFrom so io.Copy cannot recurse back into this method.
	return io.Copy(writerOnly{w.ResponseWriter}, r)
}

// Flush sends buffered data to the client. Flushing before WriteHeader commits
// an implicit 200, so it is recorded as such.
func (w *responseWriter) Flush() {
	w.markImplicitOK()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// FlushError is the error-reporting form http.ResponseController prefers.
func (w *responseWriter) FlushError() error {
	w.markImplicitOK()
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack lets the handler take over the connection (websockets). The status
// line is then written by the handler on the raw connection, where this
// wrapper cannot see it.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

// Push initiates an HTTP/2 server push when the wrapped writer supports it.
func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap returns the wrapped writer, for http.ResponseController.
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) markImplicitOK() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
}

// writerOnly exposes only Write, so io.Copy does not see a ReaderFrom.
type writerOnly struct{ io.Writer }
