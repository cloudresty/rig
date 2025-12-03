package rig

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestContext_JSON(t *testing.T) {
	tests := []struct {
		name       string
		code       int
		value      any
		wantStatus int
		wantBody   string
		wantCT     string
	}{
		{
			name:       "simple struct",
			code:       http.StatusOK,
			value:      map[string]string{"message": "hello"},
			wantStatus: http.StatusOK,
			wantBody:   `{"message":"hello"}`,
			wantCT:     "application/json; charset=utf-8",
		},
		{
			name:       "nil value",
			code:       http.StatusNoContent,
			value:      nil,
			wantStatus: http.StatusNoContent,
			wantBody:   "",
			wantCT:     "application/json; charset=utf-8",
		},
		{
			name:       "created status",
			code:       http.StatusCreated,
			value:      map[string]int{"id": 42},
			wantStatus: http.StatusCreated,
			wantBody:   `{"id":42}`,
			wantCT:     "application/json; charset=utf-8",
		},
		{
			name:       "error status",
			code:       http.StatusBadRequest,
			value:      map[string]string{"error": "invalid input"},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid input"}`,
			wantCT:     "application/json; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			c := newContext(w, r)

			err := c.JSON(tt.code, tt.value)
			if err != nil {
				t.Fatalf("JSON() error = %v", err)
			}

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if ct := w.Header().Get("Content-Type"); ct != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", ct, tt.wantCT)
			}

			got := strings.TrimSpace(w.Body.String())
			if got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}

			if !c.Written() {
				t.Error("Written() = false, want true")
			}
		})
	}
}

func TestContext_Bind(t *testing.T) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	tests := []struct {
		name    string
		body    string
		want    User
		wantErr bool
	}{
		{
			name:    "valid JSON",
			body:    `{"name":"John","email":"john@example.com"}`,
			want:    User{Name: "John", Email: "john@example.com"},
			wantErr: false,
		},
		{
			name:    "partial JSON",
			body:    `{"name":"Jane"}`,
			want:    User{Name: "Jane", Email: ""},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			body:    `{"name":}`,
			want:    User{},
			wantErr: true,
		},
		{
			name:    "empty body",
			body:    "",
			want:    User{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			c := newContext(w, r)

			var got User
			err := c.Bind(&got)

			if (err != nil) != tt.wantErr {
				t.Errorf("Bind() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("Bind() got = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestContext_Query(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		key      string
		want     string
		defValue string
		wantDef  string
	}{
		{
			name:     "existing key",
			url:      "/path?name=john",
			key:      "name",
			want:     "john",
			defValue: "default",
			wantDef:  "john",
		},
		{
			name:     "missing key",
			url:      "/path?other=value",
			key:      "name",
			want:     "",
			defValue: "default",
			wantDef:  "default",
		},
		{
			name:     "empty value",
			url:      "/path?name=",
			key:      "name",
			want:     "",
			defValue: "default",
			wantDef:  "default",
		},
		{
			name:     "multiple values",
			url:      "/path?name=first&name=second",
			key:      "name",
			want:     "first",
			defValue: "default",
			wantDef:  "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			c := newContext(w, r)

			if got := c.Query(tt.key); got != tt.want {
				t.Errorf("Query() = %q, want %q", got, tt.want)
			}

			if got := c.QueryDefault(tt.key, tt.defValue); got != tt.wantDef {
				t.Errorf("QueryDefault() = %q, want %q", got, tt.wantDef)
			}
		})
	}
}

func TestContext_Headers(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Custom-Header", "custom-value")
	c := newContext(w, r)

	// Test GetHeader
	if got := c.GetHeader("X-Custom-Header"); got != "custom-value" {
		t.Errorf("GetHeader() = %q, want %q", got, "custom-value")
	}

	if got := c.GetHeader("X-Missing"); got != "" {
		t.Errorf("GetHeader() for missing = %q, want empty", got)
	}

	// Test SetHeader
	c.SetHeader("X-Response-Header", "response-value")
	if got := w.Header().Get("X-Response-Header"); got != "response-value" {
		t.Errorf("SetHeader() result = %q, want %q", got, "response-value")
	}

	// Test Header()
	c.Header().Set("X-Another", "another-value")
	if got := w.Header().Get("X-Another"); got != "another-value" {
		t.Errorf("Header().Set() result = %q, want %q", got, "another-value")
	}
}

func TestContext_Write(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	n, err := c.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write() n = %d, want 5", n)
	}
	if !c.Written() {
		t.Error("Written() = false after Write()")
	}

	// Test WriteString
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	c2 := newContext(w2, r2)

	n, err = c2.WriteString(" world")
	if err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if n != 6 {
		t.Errorf("WriteString() n = %d, want 6", n)
	}
	if w2.Body.String() != " world" {
		t.Errorf("WriteString() body = %q, want %q", w2.Body.String(), " world")
	}
}

func TestContext_Status(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	c.Status(http.StatusAccepted)

	if w.Code != http.StatusAccepted {
		t.Errorf("Status() code = %d, want %d", w.Code, http.StatusAccepted)
	}
	if !c.Written() {
		t.Error("Written() = false after Status()")
	}
}

func TestContext_MethodAndPath(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/users/123", nil)
	c := newContext(w, r)

	if got := c.Method(); got != http.MethodPost {
		t.Errorf("Method() = %q, want %q", got, http.MethodPost)
	}

	if got := c.Path(); got != "/users/123" {
		t.Errorf("Path() = %q, want %q", got, "/users/123")
	}
}

func TestContext_RequestAndWriter(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	if c.Request() != r {
		t.Error("Request() did not return the original request")
	}

	if c.Writer() != w {
		t.Error("Writer() did not return the original writer")
	}
}

func TestContext_Context(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	if c.Context() != r.Context() {
		t.Error("Context() did not return request's context")
	}
}

func TestContext_Bind_ClosesBody(t *testing.T) {
	body := &trackingReadCloser{Reader: strings.NewReader(`{"name":"test"}`)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", body)
	r.Body = body
	c := newContext(w, r)

	var data map[string]string
	_ = c.Bind(&data)

	if !body.closed {
		t.Error("Bind() did not close the request body")
	}
}

// trackingReadCloser tracks if Close was called
type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (t *trackingReadCloser) Read(p []byte) (n int, err error) {
	return t.Reader.Read(p)
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}

func TestContext_JSON_ComplexTypes(t *testing.T) {
	type nested struct {
		Items []string `json:"items"`
		Count int      `json:"count"`
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	data := nested{Items: []string{"a", "b", "c"}, Count: 3}
	err := c.JSON(http.StatusOK, data)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	want := `{"items":["a","b","c"],"count":3}`
	got := strings.TrimSpace(w.Body.String())
	if got != want {
		t.Errorf("JSON() body = %q, want %q", got, want)
	}
}

func TestContext_Bind_Array(t *testing.T) {
	body := `[{"name":"a"},{"name":"b"}]`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	c := newContext(w, r)

	var data []map[string]string
	err := c.Bind(&data)
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	if len(data) != 2 {
		t.Errorf("Bind() len = %d, want 2", len(data))
	}
	if data[0]["name"] != "a" || data[1]["name"] != "b" {
		t.Errorf("Bind() data = %+v, unexpected values", data)
	}
}
func TestContext_Bind_NilBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = nil
	c := newContext(w, r)

	var data map[string]string
	err := c.Bind(&data)
	if err != nil {
		t.Errorf("Bind() with nil body should not error, got %v", err)
	}
}

func TestContext_SetAndGet(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// Test that store is nil initially (lazy initialization)
	if c.store != nil {
		t.Error("store should be nil initially")
	}

	// Test Get on empty store
	val, exists := c.Get("nonexistent")
	if exists {
		t.Error("Get() should return false for nonexistent key")
	}
	if val != nil {
		t.Errorf("Get() should return nil for nonexistent key, got %v", val)
	}

	// Test Set initializes store
	c.Set("key1", "value1")
	if c.store == nil {
		t.Error("store should be initialized after Set")
	}

	// Test Get returns correct value
	val, exists = c.Get("key1")
	if !exists {
		t.Error("Get() should return true for existing key")
	}
	if val != "value1" {
		t.Errorf("Get() = %v, want 'value1'", val)
	}

	// Test overwriting value
	c.Set("key1", "newvalue")
	val, exists = c.Get("key1")
	if !exists || val != "newvalue" {
		t.Errorf("Get() after overwrite = %v, want 'newvalue'", val)
	}

	// Test multiple keys
	c.Set("key2", 42)
	c.Set("key3", true)

	val2, _ := c.Get("key2")
	val3, _ := c.Get("key3")
	if val2 != 42 {
		t.Errorf("Get('key2') = %v, want 42", val2)
	}
	if val3 != true {
		t.Errorf("Get('key3') = %v, want true", val3)
	}
}

func TestContext_MustGet(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// Set a value
	c.Set("db", "database_connection")

	// Test MustGet returns correct value
	val := c.MustGet("db")
	if val != "database_connection" {
		t.Errorf("MustGet() = %v, want 'database_connection'", val)
	}
}

func TestContext_MustGet_Panics(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustGet() should panic for nonexistent key")
		} else {
			// Verify panic message contains the key
			msg, ok := r.(string)
			if !ok || !strings.Contains(msg, "nonexistent") {
				t.Errorf("panic message should contain key name, got: %v", r)
			}
		}
	}()

	c.MustGet("nonexistent")
}

func TestContext_Store_TypeAssertion(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// Store various types
	type CustomStruct struct {
		Name string
		ID   int
	}

	c.Set("string", "hello")
	c.Set("int", 123)
	c.Set("struct", &CustomStruct{Name: "test", ID: 1})
	c.Set("slice", []string{"a", "b", "c"})

	// Test type assertions
	strVal := c.MustGet("string").(string)
	if strVal != "hello" {
		t.Errorf("string value = %v, want 'hello'", strVal)
	}

	intVal := c.MustGet("int").(int)
	if intVal != 123 {
		t.Errorf("int value = %v, want 123", intVal)
	}

	structVal := c.MustGet("struct").(*CustomStruct)
	if structVal.Name != "test" || structVal.ID != 1 {
		t.Errorf("struct value = %+v, unexpected", structVal)
	}

	sliceVal := c.MustGet("slice").([]string)
	if len(sliceVal) != 3 || sliceVal[0] != "a" {
		t.Errorf("slice value = %v, unexpected", sliceVal)
	}
}

func TestContext_SetContext(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// Create a context with a value
	type ctxKey string
	key := ctxKey("user")
	ctx := context.WithValue(c.Context(), key, "john")

	// Set the new context
	c.SetContext(ctx)

	// Verify the context was updated
	if c.Context().Value(key) != "john" {
		t.Errorf("SetContext() did not update context, got %v", c.Context().Value(key))
	}

	// Verify Request() also reflects the new context
	if c.Request().Context().Value(key) != "john" {
		t.Errorf("Request().Context() did not reflect SetContext change")
	}
}

func TestContext_SetContext_WithCancel(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// Create a cancellable context
	ctx, cancel := context.WithCancel(c.Context())
	c.SetContext(ctx)

	// Verify context is not cancelled yet
	select {
	case <-c.Context().Done():
		t.Error("context should not be done yet")
	default:
		// expected
	}

	// Cancel and verify
	cancel()

	select {
	case <-c.Context().Done():
		// expected
	default:
		t.Error("context should be done after cancel")
	}
}

func TestGetType_Success(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	type Database struct {
		Name string
	}

	db := &Database{Name: "testdb"}
	c.Set("db", db)

	// Test successful type retrieval
	result, err := GetType[*Database](c, "db")
	if err != nil {
		t.Fatalf("GetType() error = %v", err)
	}
	if result.Name != "testdb" {
		t.Errorf("GetType() = %v, want %v", result.Name, "testdb")
	}
}

func TestGetType_KeyNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// Test key not found
	result, err := GetType[string](c, "nonexistent")
	if err == nil {
		t.Error("GetType() should return error for nonexistent key")
	}
	if result != "" {
		t.Errorf("GetType() should return zero value, got %v", result)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestGetType_WrongType(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	c.Set("number", 42)

	// Try to get as wrong type
	result, err := GetType[string](c, "number")
	if err == nil {
		t.Error("GetType() should return error for wrong type")
	}
	if result != "" {
		t.Errorf("GetType() should return zero value, got %v", result)
	}
	if !strings.Contains(err.Error(), "not of type") {
		t.Errorf("error should mention type mismatch, got: %v", err)
	}
}

func TestGetType_PointerTypes(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	type Config struct {
		Debug bool
	}

	cfg := &Config{Debug: true}
	c.Set("config", cfg)

	// GetType with pointer
	result, err := GetType[*Config](c, "config")
	if err != nil {
		t.Fatalf("GetType() error = %v", err)
	}
	if !result.Debug {
		t.Errorf("GetType() = %v, want Debug=true", result)
	}

	// Modifying the result should affect the original
	result.Debug = false
	original := c.MustGet("config").(*Config)
	if original.Debug {
		t.Error("modifying GetType result should affect original")
	}
}

func TestBindStrict(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name      string
		body      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid JSON",
			body:    `{"name":"John"}`,
			wantErr: false,
		},
		{
			name:      "unknown field",
			body:      `{"name":"John","admin":true}`,
			wantErr:   true,
			errSubstr: "unknown field",
		},
		{
			name:    "empty JSON",
			body:    `{}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			c := newContext(w, r)

			var user User
			err := c.BindStrict(&user)

			if tt.wantErr {
				if err == nil {
					t.Error("BindStrict() should return error")
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error should contain %q, got: %v", tt.errSubstr, err)
				}
			} else {
				if err != nil {
					t.Errorf("BindStrict() error = %v", err)
				}
			}
		})
	}
}

func TestBindStrict_NilBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = nil
	c := newContext(w, r)

	var data map[string]string
	err := c.BindStrict(&data)
	if err != nil {
		t.Errorf("BindStrict() with nil body should not error, got %v", err)
	}
}

func TestContext_Redirect(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		url          string
		wantCode     int
		wantLocation string
	}{
		{
			name:         "temporary redirect",
			code:         http.StatusFound,
			url:          "/new-location",
			wantCode:     http.StatusFound,
			wantLocation: "/new-location",
		},
		{
			name:         "permanent redirect",
			code:         http.StatusMovedPermanently,
			url:          "https://example.com",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "https://example.com",
		},
		{
			name:         "see other",
			code:         http.StatusSeeOther,
			url:          "/after-post",
			wantCode:     http.StatusSeeOther,
			wantLocation: "/after-post",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			c := newContext(w, r)

			c.Redirect(tt.code, tt.url)

			if w.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tt.wantCode)
			}

			if loc := w.Header().Get("Location"); loc != tt.wantLocation {
				t.Errorf("Location = %q, want %q", loc, tt.wantLocation)
			}

			if !c.Written() {
				t.Error("Written() should be true after Redirect")
			}
		})
	}
}

func TestContext_QueryArray(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/?tag=go&tag=rust&tag=python&single=value", nil)
	c := newContext(w, r)

	// Multiple values
	tags := c.QueryArray("tag")
	if len(tags) != 3 {
		t.Errorf("QueryArray(tag) = %d values, want 3", len(tags))
	}
	expected := []string{"go", "rust", "python"}
	for i, tag := range tags {
		if tag != expected[i] {
			t.Errorf("tags[%d] = %q, want %q", i, tag, expected[i])
		}
	}

	// Single value
	single := c.QueryArray("single")
	if len(single) != 1 || single[0] != "value" {
		t.Errorf("QueryArray(single) = %v, want [value]", single)
	}

	// Missing key
	missing := c.QueryArray("missing")
	if missing != nil {
		t.Errorf("QueryArray(missing) = %v, want nil", missing)
	}
}

func TestContext_QueryCaching(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/?key=value", nil)
	c := newContext(w, r)

	// First call parses and caches
	v1 := c.Query("key")
	if v1 != "value" {
		t.Errorf("Query(key) = %q, want %q", v1, "value")
	}

	// Second call uses cache (coverage of queryParams being non-nil)
	v2 := c.QueryDefault("key", "default")
	if v2 != "value" {
		t.Errorf("QueryDefault(key) = %q, want %q", v2, "value")
	}

	// Third call also uses cache
	v3 := c.QueryArray("key")
	if len(v3) != 1 || v3[0] != "value" {
		t.Errorf("QueryArray(key) = %v, want [value]", v3)
	}
}

func TestContext_JSON_DoubleWritePrevention(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// First call to Status
	c.Status(http.StatusCreated)

	// JSON should not overwrite the status
	err := c.JSON(http.StatusOK, map[string]string{"message": "test"})
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	// Status should remain 201 (from first call)
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d (first Status call should win)", w.Code, http.StatusCreated)
	}

	// Body should still be written
	if !strings.Contains(w.Body.String(), "message") {
		t.Errorf("body should contain JSON data, got %q", w.Body.String())
	}
}

func TestContext_JSON_WritePreventsPanic(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	// Write something first
	_, _ = c.Write([]byte("hello"))

	// JSON should not panic or try to write headers again
	err := c.JSON(http.StatusOK, map[string]string{"message": "test"})
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	// Body should contain both writes
	body := w.Body.String()
	if !strings.Contains(body, "hello") {
		t.Errorf("body should contain first write, got %q", body)
	}
	if !strings.Contains(body, "message") {
		t.Errorf("body should contain JSON, got %q", body)
	}
}

func TestContext_FormValue(t *testing.T) {
	// Create form data
	form := strings.NewReader("name=John&email=john@example.com")
	r := httptest.NewRequest(http.MethodPost, "/?name=QueryJohn", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	c := newContext(w, r)

	// POST body takes precedence over query string
	if got := c.FormValue("name"); got != "John" {
		t.Errorf("FormValue(name) = %q, want %q", got, "John")
	}

	// Value only in body
	if got := c.FormValue("email"); got != "john@example.com" {
		t.Errorf("FormValue(email) = %q, want %q", got, "john@example.com")
	}

	// Missing key
	if got := c.FormValue("missing"); got != "" {
		t.Errorf("FormValue(missing) = %q, want empty", got)
	}
}

func TestContext_PostFormValue(t *testing.T) {
	// Create form data with query param
	form := strings.NewReader("name=BodyName")
	r := httptest.NewRequest(http.MethodPost, "/?name=QueryName&queryOnly=value", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	c := newContext(w, r)

	// PostFormValue ignores query string
	if got := c.PostFormValue("name"); got != "BodyName" {
		t.Errorf("PostFormValue(name) = %q, want %q", got, "BodyName")
	}

	// Query-only param returns empty for PostFormValue
	if got := c.PostFormValue("queryOnly"); got != "" {
		t.Errorf("PostFormValue(queryOnly) = %q, want empty", got)
	}
}

func TestContext_File(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "rig-test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	content := "Hello, this is a test file!"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/download", nil)
	c := newContext(w, r)

	c.File(tmpFile.Name())

	if !c.Written() {
		t.Error("Written() should be true after File")
	}

	if !strings.Contains(w.Body.String(), content) {
		t.Errorf("body = %q, should contain %q", w.Body.String(), content)
	}
}

func TestContext_Data(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := newContext(w, r)

	data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	c.Data(http.StatusOK, "image/png", data)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}

	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Errorf("body = %v, want %v", w.Body.Bytes(), data)
	}

	if !c.Written() {
		t.Error("Written() should be true after Data")
	}
}
