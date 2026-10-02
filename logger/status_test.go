package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/auth"
)

// TestLoggedStatusIsTheStatusSent pins that the access log records the status
// code the client actually received, however the handler produced it.
//
// The logger used to INFER the status from the handler's return value (nil ->
// 200, error -> 500) and never looked at the response at all, so every 404,
// 403 and 401 written by a handler that then returned nil was logged as 200.
// Each row below asserts the logged status equals the recorder's status, so a
// row cannot pass by both sides agreeing on a wrong constant.
func TestLoggedStatusIsTheStatusSent(t *testing.T) {
	tests := []struct {
		name    string
		handler rig.HandlerFunc
		want    int
	}{
		{
			name: "WriteHeader directly on the writer",
			handler: func(c *rig.Context) error {
				c.Writer().WriteHeader(http.StatusNotFound)
				return nil
			},
			want: http.StatusNotFound,
		},
		{
			name: "Context.Status",
			handler: func(c *rig.Context) error {
				c.Status(http.StatusNotFound)
				return nil
			},
			want: http.StatusNotFound,
		},
		{
			name: "Context.JSON",
			handler: func(c *rig.Context) error {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
			},
			want: http.StatusForbidden,
		},
		{
			name: "Context.Data",
			handler: func(c *rig.Context) error {
				c.Data(http.StatusTeapot, "text/plain", []byte("short and stout"))
				return nil
			},
			want: http.StatusTeapot,
		},
		{
			name: "http.Error on the writer",
			handler: func(c *rig.Context) error {
				http.Error(c.Writer(), "unauthorised", http.StatusUnauthorized)
				return nil
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "http.NotFound on the writer",
			handler: func(c *rig.Context) error {
				http.NotFound(c.Writer(), c.Request())
				return nil
			},
			want: http.StatusNotFound,
		},
		{
			name: "Context.Redirect",
			handler: func(c *rig.Context) error {
				c.Redirect(http.StatusFound, "/elsewhere")
				return nil
			},
			want: http.StatusFound,
		},
		{
			name: "body written without WriteHeader is an implicit 200",
			handler: func(c *rig.Context) error {
				_, err := c.Writer().Write([]byte("hello"))
				return err
			},
			want: http.StatusOK,
		},
		{
			name: "nothing written and no error is an implicit 200",
			handler: func(c *rig.Context) error {
				return nil
			},
			want: http.StatusOK,
		},
		{
			name: "nothing written and an error is the error handler's 500",
			handler: func(c *rig.Context) error {
				return errors.New("boom")
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "a status written before returning an error is the status sent",
			handler: func(c *rig.Context) error {
				http.Error(c.Writer(), "gone", http.StatusNotFound)
				return errors.New("lookup failed")
			},
			want: http.StatusNotFound,
		},
		{
			// Separates "a body was sent, so 200" from the no-write fallback,
			// which would say 500 here because the handler returned an error.
			name: "a body written before returning an error is the 200 sent",
			handler: func(c *rig.Context) error {
				_, _ = c.Writer().Write([]byte("partial"))
				return errors.New("failed mid-stream")
			},
			want: http.StatusOK,
		},
		{
			name: "only the first final WriteHeader counts",
			handler: func(c *rig.Context) error {
				c.Writer().WriteHeader(http.StatusNotFound)
				c.Writer().WriteHeader(http.StatusOK) // superfluous; net/http ignores it
				return nil
			},
			want: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			r := rig.New()
			r.Use(New(Config{Format: FormatJSON, Output: &buf}))
			r.GET("/x", tt.handler)

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

			if rec.Code != tt.want {
				t.Fatalf("response status = %d, want %d (the test's premise is wrong)", rec.Code, tt.want)
			}

			var entry LogEntry
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("decoding log line %q: %v", buf.String(), err)
			}
			if entry.Status != rec.Code {
				t.Errorf("logged status = %d, but the client received %d", entry.Status, rec.Code)
			}
		})
	}
}

// TestLoggedStatusFromRigAuthMiddleware covers a refusal written by rig's own
// auth middleware, which sits between the logger and the handler.
func TestLoggedStatusFromRigAuthMiddleware(t *testing.T) {
	var buf bytes.Buffer

	r := rig.New()
	r.Use(New(Config{Format: FormatJSON, Output: &buf}))
	r.Use(auth.APIKeySimple("secret"))
	r.GET("/x", func(c *rig.Context) error { return nil })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("response status = %d, want 401", rec.Code)
	}

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decoding log line %q: %v", buf.String(), err)
	}
	if entry.Status != http.StatusUnauthorized {
		t.Errorf("logged status = %d, want 401", entry.Status)
	}
}
