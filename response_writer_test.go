package rig

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// serve runs r on a real server, because httptest.ResponseRecorder does not
// behave like net/http for 1xx codes, hijacking or flushing to the wire.
func serve(t *testing.T, r *Router) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestWriterKeepsOptionalInterfaces pins that wrapping the writer to record the
// status does not hide the capabilities streaming, websockets and sendfile need
// from code that type-asserts for them.
func TestWriterKeepsOptionalInterfaces(t *testing.T) {
	r := New()
	var got []string
	r.GET("/x", func(c *Context) error {
		w := c.Writer()
		if _, ok := w.(http.Flusher); ok {
			got = append(got, "Flusher")
		}
		if _, ok := w.(http.Hijacker); ok {
			got = append(got, "Hijacker")
		}
		if _, ok := w.(http.Pusher); ok {
			got = append(got, "Pusher")
		}
		if _, ok := w.(io.ReaderFrom); ok {
			got = append(got, "ReaderFrom")
		}
		if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); !ok || u.Unwrap() == nil {
			t.Error("writer does not unwrap for http.ResponseController")
		}
		return nil
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if want := "Flusher,Hijacker,Pusher,ReaderFrom"; strings.Join(got, ",") != want {
		t.Errorf("writer implements %v, want %s", got, want)
	}
}

// TestStreamingFlushReachesTheClient proves Flush goes through the wrapper to
// the wire (SSE): the client reads the first event while the handler is still
// blocked, which cannot happen if the flush was swallowed.
func TestStreamingFlushReachesTheClient(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	statusSeen := make(chan int, 1)

	r := New()
	r.GET("/events", func(c *Context) error {
		c.SetHeader("Content-Type", "text/event-stream")
		_, _ = c.WriteString("data: first\n\n")
		c.Writer().(http.Flusher).Flush()
		<-release
		statusSeen <- c.StatusCode()
		return nil
	})
	srv := serve(t, r)
	// Registered after serve so it runs BEFORE srv.Close (cleanups are LIFO):
	// a failed assertion must not leave Close waiting on the blocked handler.
	t.Cleanup(unblock)

	// The request runs in a goroutine: with a swallowed flush the client would
	// not even receive headers until the handler returned, which it never does.
	type result struct {
		resp *http.Response
		line string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get(srv.URL + "/events")
		if err != nil {
			got <- result{err: err}
			return
		}
		l, err := bufio.NewReader(resp.Body).ReadString('\n')
		got <- result{resp: resp, line: l, err: err}
	}()

	var resp *http.Response
	select {
	case res := <-got:
		if res.err != nil {
			t.Fatal(res.err)
		}
		resp = res.resp
		defer func() { _ = resp.Body.Close() }()
		if res.line != "data: first\n" {
			t.Errorf("first line = %q", res.line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("flushed event never reached the client")
	}
	unblock()
	_, _ = io.Copy(io.Discard, resp.Body)

	if status := <-statusSeen; status != http.StatusOK {
		t.Errorf("StatusCode after streaming = %d, want 200", status)
	}
}

// TestResponseControllerFlushWorks covers code that flushes through
// http.ResponseController rather than a type assertion.
func TestResponseControllerFlushWorks(t *testing.T) {
	r := New()
	var flushErr error
	r.GET("/x", func(c *Context) error {
		_, _ = c.WriteString("x")
		flushErr = http.NewResponseController(c.Writer()).Flush()
		return nil
	})
	srv := serve(t, r)

	resp, err := http.Get(srv.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if flushErr != nil {
		t.Errorf("ResponseController.Flush through the wrapper: %v", flushErr)
	}
}

// TestHijackThroughTheWriter performs a websocket-style upgrade: the handler
// hijacks the connection and writes its own 101 status line on it.
func TestHijackThroughTheWriter(t *testing.T) {
	statusSeen := make(chan int, 1)

	r := New()
	r.GET("/ws", func(c *Context) error {
		conn, rw, err := c.Writer().(http.Hijacker).Hijack()
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: test\r\nConnection: Upgrade\r\n\r\nhello")
		_ = rw.Flush()
		statusSeen <- c.StatusCode()
		return nil
	})
	srv := serve(t, r)

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write([]byte("GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: test\r\nConnection: Upgrade\r\n\r\n"))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("reading upgrade response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("client saw %d, want 101 (hijack failed through the wrapper)", resp.StatusCode)
	}
	select {
	case got := <-statusSeen:
		if got != http.StatusSwitchingProtocols {
			t.Errorf("StatusCode after hijack = %d, want 101", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler never reported its status")
	}
}

// TestServeFileThroughTheWriter exercises the io.ReaderFrom path (http.ServeFile
// copies the file with io.CopyN, which uses ReadFrom when available).
func TestServeFileThroughTheWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 64<<10)), 0o600); err != nil {
		t.Fatal(err)
	}

	statusSeen := make(chan int, 1)
	r := New()
	r.GET("/f", func(c *Context) error {
		c.File(path)
		statusSeen <- c.StatusCode()
		return nil
	})
	srv := serve(t, r)

	resp, err := http.Get(srv.URL + "/f")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if len(body) != 64<<10 {
		t.Errorf("served %d bytes, want %d", len(body), 64<<10)
	}
	if status := <-statusSeen; status != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", status)
	}
}

// TestReadFromWithoutUnderlyingReaderFrom covers the fallback copy when the
// wrapped writer has no ReadFrom (httptest.ResponseRecorder has none).
func TestReadFromWithoutUnderlyingReaderFrom(t *testing.T) {
	rec := httptest.NewRecorder()
	w := newResponseWriter(rec)

	n, err := w.ReadFrom(strings.NewReader("hello"))
	if err != nil || n != 5 {
		t.Fatalf("ReadFrom = %d, %v", n, err)
	}
	if rec.Body.String() != "hello" || w.status != http.StatusOK {
		t.Errorf("body %q status %d", rec.Body.String(), w.status)
	}
}

// TestPushWithoutSupportReportsNotSupported: HTTP/1 has no server push.
func TestPushWithoutSupportReportsNotSupported(t *testing.T) {
	w := newResponseWriter(httptest.NewRecorder())
	if err := w.Push("/x", nil); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Push = %v, want http.ErrNotSupported", err)
	}
}

// TestInformationalStatusIsNotFinal: 103 Early Hints precedes the real status.
func TestInformationalStatusIsNotFinal(t *testing.T) {
	statusSeen := make(chan int, 1)
	r := New()
	r.GET("/x", func(c *Context) error {
		c.Writer().WriteHeader(http.StatusEarlyHints)
		c.Writer().WriteHeader(http.StatusAccepted)
		statusSeen <- c.StatusCode()
		return nil
	})
	srv := serve(t, r)

	resp, err := http.Get(srv.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("client saw %d, want 202", resp.StatusCode)
	}
	if status := <-statusSeen; status != http.StatusAccepted {
		t.Errorf("StatusCode = %d, want 202", status)
	}
}

// TestStatusCodeBeforeAnythingIsWritten is 0, so callers can tell "undecided"
// from "200".
func TestStatusCodeBeforeAnythingIsWritten(t *testing.T) {
	r := New()
	var status = -1
	var written = true
	r.GET("/x", func(c *Context) error {
		c.SetHeader("X-Only", "a header")
		status, written = c.StatusCode(), c.Written()
		return nil
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if status != 0 || written {
		t.Errorf("StatusCode = %d, Written = %v; want 0, false", status, written)
	}
}

// TestErrorHandlerSkippedAfterDirectWrite: a handler that answered through
// c.Writer() and then returned an error has already responded, so the error
// handler must not append a second response to the body.
func TestErrorHandlerSkippedAfterDirectWrite(t *testing.T) {
	r := New()
	r.GET("/x", func(c *Context) error {
		http.Error(c.Writer(), "gone", http.StatusNotFound)
		return errors.New("lookup failed")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Internal Server Error") {
		t.Errorf("error handler wrote a second response: %q", rec.Body.String())
	}
}

// pushRecorder is a writer that supports server push, as an HTTP/2 writer does.
type pushRecorder struct {
	*httptest.ResponseRecorder
	pushed []string
}

func (p *pushRecorder) Push(target string, _ *http.PushOptions) error {
	p.pushed = append(p.pushed, target)
	return nil
}

// TestPushIsDelegated: when the wrapped writer can push, the wrapper must not
// hide it behind http.ErrNotSupported.
func TestPushIsDelegated(t *testing.T) {
	under := &pushRecorder{ResponseRecorder: httptest.NewRecorder()}
	w := newResponseWriter(under)

	if err := w.Push("/app.css", nil); err != nil {
		t.Fatalf("Push = %v", err)
	}
	if len(under.pushed) != 1 || under.pushed[0] != "/app.css" {
		t.Errorf("wrapped writer pushed %v", under.pushed)
	}
}
