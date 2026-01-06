package render

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/cloudresty/rig"
)

func TestNew_DefaultConfig(t *testing.T) {
	engine := New(Config{})

	if engine.config.Directory != "templates" {
		t.Errorf("Directory = %q, want %q", engine.config.Directory, "templates")
	}

	if len(engine.config.Extensions) != 2 {
		t.Errorf("Extensions = %v, want 2 elements", engine.config.Extensions)
	}
}

func TestNew_CustomConfig(t *testing.T) {
	engine := New(Config{
		Directory:  "./views",
		Extensions: []string{".gohtml"},
		Layout:     "base",
		DevMode:    true,
	})

	if engine.config.Directory != "./views" {
		t.Errorf("Directory = %q, want %q", engine.config.Directory, "./views")
	}

	if len(engine.config.Extensions) != 1 || engine.config.Extensions[0] != ".gohtml" {
		t.Errorf("Extensions = %v, want [.gohtml]", engine.config.Extensions)
	}

	if engine.config.Layout != "base" {
		t.Errorf("Layout = %q, want %q", engine.config.Layout, "base")
	}

	if !engine.config.DevMode {
		t.Error("DevMode = false, want true")
	}
}

func TestEngine_Load(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check that templates were loaded
	expectedTemplates := []string{"simple", "layouts/base", "pages/home", "safe_html"}
	for _, name := range expectedTemplates {
		if _, ok := engine.templates[name]; !ok {
			t.Errorf("Template %q not loaded", name)
		}
	}
}

func TestEngine_Load_DirectoryNotFound(t *testing.T) {
	engine := New(Config{
		Directory: "./nonexistent",
	})

	err := engine.Load()
	if err == nil {
		t.Error("Load() expected error for nonexistent directory")
	}
}

func TestEngine_Load_WithLayout(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
		Layout:    "layouts/base",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if engine.layoutName == "" {
		t.Error("Layout name not set")
	}
}

func TestEngine_Load_LayoutNotFound(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
		Layout:    "nonexistent",
	})

	err := engine.Load()
	if err == nil {
		t.Error("Load() expected error for nonexistent layout")
	}
}

func TestEngine_Render_Simple(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("simple", map[string]any{
		"Title":   "Hello",
		"Message": "World",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "<h1>Hello</h1>") {
		t.Errorf("Result should contain title, got: %s", result)
	}

	if !strings.Contains(result, "<p>World</p>") {
		t.Errorf("Result should contain message, got: %s", result)
	}
}

func TestEngine_Render_NotFound(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	_, err := engine.Render("nonexistent", nil)
	if err == nil {
		t.Error("Render() expected error for nonexistent template")
	}
}

func TestEngine_Render_WithLayout(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
		Layout:    "layouts/base",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("pages/home", map[string]any{
		"Title": "My App",
		"Name":  "John",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Should contain layout structure
	if !strings.Contains(result, "<!DOCTYPE html>") {
		t.Error("Result should contain DOCTYPE from layout")
	}
	if !strings.Contains(result, "<title>My App</title>") {
		t.Error("Result should contain title from layout")
	}
	if !strings.Contains(result, "Site Header") {
		t.Error("Result should contain header from layout")
	}
	if !strings.Contains(result, "Site Footer") {
		t.Error("Result should contain footer from layout")
	}

	// Should contain page content
	if !strings.Contains(result, "Welcome, John!") {
		t.Errorf("Result should contain page content, got: %s", result)
	}
}

func TestEngine_Render_NestedPath(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("pages/home", map[string]any{
		"Name": "Alice",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "Welcome, Alice!") {
		t.Errorf("Result should contain name, got: %s", result)
	}
}

func TestEngine_AddFunc(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates_with_funcs",
	})

	engine.AddFunc("upper", strings.ToUpper)

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("with_func", map[string]any{
		"Text": "hello",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "HELLO") {
		t.Errorf("Result should contain uppercased text, got: %s", result)
	}
}

func TestEngine_AddFuncs(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates_with_funcs",
	})

	engine.AddFuncs(template.FuncMap{
		"upper": strings.ToUpper,
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("with_func", map[string]any{
		"Text": "world",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "WORLD") {
		t.Errorf("Result should contain uppercased text, got: %s", result)
	}
}

func TestEngine_SafeFunction(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("safe_html", map[string]any{
		"RawHTML": "<strong>Bold</strong>",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// The safe function should preserve HTML
	if !strings.Contains(result, "<strong>Bold</strong>") {
		t.Errorf("Result should contain raw HTML, got: %s", result)
	}
}

func TestMiddleware(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	r := rig.New()
	r.Use(engine.Middleware())

	r.GET("/", func(c *rig.Context) error {
		return HTML(c, http.StatusOK, "simple", map[string]any{
			"Title":   "Test",
			"Message": "Hello",
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/html; charset=utf-8")
	}

	body := w.Body.String()
	if !strings.Contains(body, "<h1>Test</h1>") {
		t.Errorf("Body should contain title, got: %s", body)
	}
}

func TestHTML_NoMiddleware(t *testing.T) {
	r := rig.New()

	r.GET("/", func(c *rig.Context) error {
		return HTML(c, http.StatusOK, "simple", nil)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	// Should return 500 because middleware wasn't used
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHTMLDirect(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	r := rig.New()

	r.GET("/", func(c *rig.Context) error {
		return HTMLDirect(c, engine, http.StatusOK, "simple", map[string]any{
			"Title":   "Direct",
			"Message": "Test",
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "<h1>Direct</h1>") {
		t.Errorf("Body should contain title, got: %s", body)
	}
}

func TestGetEngine(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	r := rig.New()
	r.Use(engine.Middleware())

	var retrieved *Engine

	r.GET("/", func(c *rig.Context) error {
		retrieved = GetEngine(c)
		return c.JSON(http.StatusOK, nil)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if retrieved == nil {
		t.Error("GetEngine() returned nil")
	}

	if retrieved != engine {
		t.Error("GetEngine() returned different engine")
	}
}

func TestGetEngine_NoMiddleware(t *testing.T) {
	r := rig.New()

	var retrieved *Engine

	r.GET("/", func(c *rig.Context) error {
		retrieved = GetEngine(c)
		return c.JSON(http.StatusOK, nil)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if retrieved != nil {
		t.Error("GetEngine() should return nil when middleware not used")
	}
}

func TestDevMode_ReloadsTemplates(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
		DevMode:   true,
	})

	r := rig.New()
	r.Use(engine.Middleware())

	r.GET("/", func(c *rig.Context) error {
		return HTML(c, http.StatusOK, "simple", map[string]any{
			"Title":   "Dev",
			"Message": "Mode",
		})
	})

	// First request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("First request: status = %d, want %d", w.Code, http.StatusOK)
	}

	// Second request (should reload templates)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Second request: status = %d, want %d", w2.Code, http.StatusOK)
	}
}

func TestConfig_CustomFuncs(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates_with_funcs",
		Funcs: template.FuncMap{
			"upper": strings.ToUpper,
		},
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("with_func", map[string]any{
		"Text": "config",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "CONFIG") {
		t.Errorf("Result should contain uppercased text, got: %s", result)
	}
}

// Tests for new features: Partials, JSON, XML, Auto

func TestEngine_Partials(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check that partials were loaded
	partials := engine.PartialNames()
	if len(partials) < 2 {
		t.Errorf("Expected at least 2 partials, got %d: %v", len(partials), partials)
	}

	// Render a template that uses partials
	result, err := engine.Render("with_partials", map[string]any{
		"Title": "Test Page",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Check that partial content is included
	if !strings.Contains(result, `class="sidebar"`) {
		t.Errorf("Result should contain sidebar partial, got: %s", result)
	}
	if !strings.Contains(result, "Test Page") {
		t.Errorf("Result should contain page title, got: %s", result)
	}
	if !strings.Contains(result, "2025 Test Company") {
		t.Errorf("Result should contain footer partial, got: %s", result)
	}
}

func TestEngine_TemplateNames(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	names := engine.TemplateNames()
	if len(names) == 0 {
		t.Error("TemplateNames() returned empty list")
	}

	// Should contain some known templates
	if !slices.Contains(names, "simple") {
		t.Errorf("TemplateNames() should contain 'simple', got: %v", names)
	}
}

func TestJSON(t *testing.T) {
	r := rig.New()

	r.GET("/api", func(c *rig.Context) error {
		return JSON(c, http.StatusOK, map[string]any{
			"message": "hello",
			"count":   42,
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != ContentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", contentType, ContentTypeJSON)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"message":"hello"`) {
		t.Errorf("Body should contain message, got: %s", body)
	}
	if !strings.Contains(body, `"count":42`) {
		t.Errorf("Body should contain count, got: %s", body)
	}
}

func TestXML(t *testing.T) {
	type Response struct {
		XMLName xml.Name `xml:"response"`
		Message string   `xml:"message"`
		Count   int      `xml:"count"`
	}

	r := rig.New()

	r.GET("/api", func(c *rig.Context) error {
		return XML(c, http.StatusOK, Response{
			Message: "hello",
			Count:   42,
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != ContentTypeXML {
		t.Errorf("Content-Type = %q, want %q", contentType, ContentTypeXML)
	}

	body := w.Body.String()
	if !strings.Contains(body, "<?xml") {
		t.Errorf("Body should contain XML header, got: %s", body)
	}
	if !strings.Contains(body, "<message>hello</message>") {
		t.Errorf("Body should contain message element, got: %s", body)
	}
}

func TestAuto_JSON(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	r := rig.New()
	r.Use(engine.Middleware())

	r.GET("/data", func(c *rig.Context) error {
		return Auto(c, http.StatusOK, "simple", map[string]any{
			"Title":   "Test",
			"Message": "Hello",
		})
	})

	// Request with JSON Accept header
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Accept", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != ContentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", contentType, ContentTypeJSON)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"Title":"Test"`) {
		t.Errorf("Body should contain JSON data, got: %s", body)
	}
}

func TestAuto_XML(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	r := rig.New()
	r.Use(engine.Middleware())

	type Data struct {
		XMLName xml.Name `xml:"data"`
		Title   string   `xml:"title"`
	}

	r.GET("/data", func(c *rig.Context) error {
		return Auto(c, http.StatusOK, "", Data{Title: "Test"})
	})

	// Request with XML Accept header
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Accept", "application/xml")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != ContentTypeXML {
		t.Errorf("Content-Type = %q, want %q", contentType, ContentTypeXML)
	}
}

func TestAuto_HTML(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	r := rig.New()
	r.Use(engine.Middleware())

	r.GET("/page", func(c *rig.Context) error {
		return Auto(c, http.StatusOK, "simple", map[string]any{
			"Title":   "Test Page",
			"Message": "Welcome",
		})
	})

	// Request with HTML Accept header (browser)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != ContentTypeHTML {
		t.Errorf("Content-Type = %q, want %q", contentType, ContentTypeHTML)
	}

	body := w.Body.String()
	if !strings.Contains(body, "<h1>Test Page</h1>") {
		t.Errorf("Body should contain HTML, got: %s", body)
	}
}

func TestAuto_DefaultToHTML(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	r := rig.New()
	r.Use(engine.Middleware())

	r.GET("/page", func(c *rig.Context) error {
		return Auto(c, http.StatusOK, "simple", map[string]any{
			"Title":   "Default",
			"Message": "No Accept header",
		})
	})

	// Request without Accept header - should default to HTML
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != ContentTypeHTML {
		t.Errorf("Content-Type = %q, want %q (should default to HTML)", contentType, ContentTypeHTML)
	}
}

func TestAuto_NoTemplate_DefaultsToJSON(t *testing.T) {
	r := rig.New()

	r.GET("/api", func(c *rig.Context) error {
		return Auto(c, http.StatusOK, "", map[string]any{
			"status": "ok",
		})
	})

	// Request without Accept header and no template - should default to JSON
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != ContentTypeJSON {
		t.Errorf("Content-Type = %q, want %q (should default to JSON when no template)", contentType, ContentTypeJSON)
	}
}

func TestEngine_EmbedFS(t *testing.T) {
	// Create a test filesystem using fstest.MapFS
	testFS := fstest.MapFS{
		"templates/home.html":         {Data: []byte("<h1>{{.Title}}</h1>")},
		"templates/about.html":        {Data: []byte("<h2>About: {{.Name}}</h2>")},
		"templates/_header.html":      {Data: []byte("<header>Header</header>")},
		"templates/with_partial.html": {Data: []byte("{{template \"_header\" .}}<main>{{.Content}}</main>")},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  "templates",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check templates were loaded
	names := engine.TemplateNames()
	if len(names) < 2 {
		t.Errorf("Expected at least 2 templates, got %d: %v", len(names), names)
	}

	// Render a template
	result, err := engine.Render("home", map[string]any{"Title": "Embedded!"})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(result, "<h1>Embedded!</h1>") {
		t.Errorf("Expected rendered content, got: %s", result)
	}

	// Check partials work with embed.FS
	partials := engine.PartialNames()
	if len(partials) == 0 {
		t.Error("Expected partials to be loaded from embed.FS")
	}

	result, err = engine.Render("with_partial", map[string]any{"Content": "Main content"})
	if err != nil {
		t.Fatalf("Render with partial error = %v", err)
	}
	if !strings.Contains(result, "<header>Header</header>") {
		t.Errorf("Expected header partial, got: %s", result)
	}
}

func TestEngine_EmbedFS_RootDirectory(t *testing.T) {
	// Test with templates at root (Directory = ".")
	testFS := fstest.MapFS{
		"index.html": {Data: []byte("<p>Root template</p>")},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("index", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(result, "Root template") {
		t.Errorf("Expected root template content, got: %s", result)
	}
}

// PageData is a test struct for layout testing
type PageData struct {
	Title   string
	Message string
	Count   int
}

func TestEngine_LayoutWithStruct(t *testing.T) {
	// Create filesystem with layout and page template
	testFS := fstest.MapFS{
		"layouts/base.html": {Data: []byte(`<!DOCTYPE html>
<html>
<head><title>{{.Data.Title}}</title></head>
<body>{{.Content}}</body>
</html>`)},
		"page.html": {Data: []byte(`<h1>{{.Title}}</h1><p>{{.Message}}</p><span>{{.Count}}</span>`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
		Layout:     "layouts/base",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Render with a struct (not a map!)
	data := PageData{
		Title:   "Struct Title",
		Message: "Hello from struct",
		Count:   42,
	}

	result, err := engine.Render("page", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Layout should see .Data.Title
	if !strings.Contains(result, "<title>Struct Title</title>") {
		t.Errorf("Layout should access struct via .Data.Title, got: %s", result)
	}
	// Page content should be rendered
	if !strings.Contains(result, "<h1>Struct Title</h1>") {
		t.Errorf("Page should render struct fields, got: %s", result)
	}
	if !strings.Contains(result, "<span>42</span>") {
		t.Errorf("Page should render Count field, got: %s", result)
	}
}

func TestEngine_LayoutWithMap_BackwardCompatible(t *testing.T) {
	// Test that maps still work with direct access (backward compatibility)
	testFS := fstest.MapFS{
		"layouts/base.html": {Data: []byte(`<title>{{.Title}}</title><body>{{.Content}}</body>`)},
		"page.html":         {Data: []byte(`<p>{{.Message}}</p>`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
		Layout:     "layouts/base",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Render with a map (backward compatible)
	data := map[string]any{
		"Title":   "Map Title",
		"Message": "Hello from map",
	}

	result, err := engine.Render("page", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Layout should still see .Title directly for maps
	if !strings.Contains(result, "<title>Map Title</title>") {
		t.Errorf("Layout should access map via .Title (backward compat), got: %s", result)
	}
}

func TestEngine_AddFunc_Concurrent(t *testing.T) {
	engine := New(Config{
		Directory: "./testdata/templates",
	})

	// Run AddFunc concurrently to ensure no race conditions
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			engine.AddFunc(fmt.Sprintf("func%d", n), func() string {
				return fmt.Sprintf("result%d", n)
			})
		}(i)
	}
	wg.Wait()

	// Should not panic and should have added all functions
	// (exact count may vary due to race, but should be > 0)
}

func TestEngine_CustomDelimiters(t *testing.T) {
	// Test with Vue.js-style delimiters [[ ]]
	testFS := fstest.MapFS{
		"page.html": {Data: []byte(`<div>Hello, [[ .Name ]]!</div>{{ vueVariable }}`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
		Delims:     []string{"[[", "]]"},
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("page", map[string]any{"Name": "World"})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Go should have rendered [[ .Name ]] as "World"
	if !strings.Contains(result, "Hello, World!") {
		t.Errorf("Expected 'Hello, World!', got: %s", result)
	}

	// Vue's {{ vueVariable }} should be left untouched
	if !strings.Contains(result, "{{ vueVariable }}") {
		t.Errorf("Expected Vue syntax to be preserved, got: %s", result)
	}
}

func TestEngine_CustomDelimiters_WithPartials(t *testing.T) {
	// Test that custom delimiters work with partials
	testFS := fstest.MapFS{
		"_header.html": {Data: []byte(`<header>[[ .Title ]]</header>`)},
		"page.html":    {Data: []byte(`[[ template "_header" . ]]<main>[[ .Content ]]</main>`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
		Delims:     []string{"[[", "]]"},
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("page", map[string]any{
		"Title":   "My Page",
		"Content": "Page content",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "<header>My Page</header>") {
		t.Errorf("Expected header with title, got: %s", result)
	}
	if !strings.Contains(result, "<main>Page content</main>") {
		t.Errorf("Expected main with content, got: %s", result)
	}
}

func TestEngine_CustomDelimiters_WithLayout(t *testing.T) {
	// Test that custom delimiters work with layouts
	testFS := fstest.MapFS{
		"layouts/base.html": {Data: []byte(`<!DOCTYPE html><title>[[ .Data.Title ]]</title><body>[[ .Content ]]</body>`)},
		"page.html":         {Data: []byte(`<p>[[ .Message ]]</p>{{ angularBinding }}`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
		Layout:     "layouts/base",
		Delims:     []string{"[[", "]]"},
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	result, err := engine.Render("page", map[string]any{
		"Title":   "Test Page",
		"Message": "Hello Angular!",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Check Go template rendered correctly
	if !strings.Contains(result, "<title>Test Page</title>") {
		t.Errorf("Expected title, got: %s", result)
	}
	if !strings.Contains(result, "<p>Hello Angular!</p>") {
		t.Errorf("Expected message, got: %s", result)
	}

	// Check Angular syntax preserved
	if !strings.Contains(result, "{{ angularBinding }}") {
		t.Errorf("Expected Angular binding to be preserved, got: %s", result)
	}
}

func TestEngine_DumpFunction(t *testing.T) {
	// Test the dump helper function for debugging
	testFS := fstest.MapFS{
		"debug.html": {Data: []byte(`<div>{{ dump . }}</div>`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
	})

	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	data := map[string]any{
		"Name":  "Test User",
		"Count": 42,
	}

	result, err := engine.Render("debug", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Should output JSON in a <pre> tag
	if !strings.Contains(result, "<pre>") {
		t.Errorf("Expected <pre> tag, got: %s", result)
	}
	if !strings.Contains(result, `"Name": "Test User"`) {
		t.Errorf("Expected Name in dump output, got: %s", result)
	}
	if !strings.Contains(result, `"Count": 42`) {
		t.Errorf("Expected Count in dump output, got: %s", result)
	}
}

func TestHTMLSafe_Success(t *testing.T) {
	testFS := fstest.MapFS{
		"page.html":       {Data: []byte(`<h1>{{.Title}}</h1>`)},
		"errors/500.html": {Data: []byte(`<h1>Error: {{.Error}}</h1>`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
	})
	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	r := rig.New()
	r.Use(engine.Middleware())
	r.GET("/", func(c *rig.Context) error {
		return HTMLSafe(c, http.StatusOK, "page", map[string]any{"Title": "Success"}, "errors/500")
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "<h1>Success</h1>") {
		t.Errorf("Expected success content, got: %s", w.Body.String())
	}
}

func TestHTMLSafe_FallbackToErrorPage(t *testing.T) {
	testFS := fstest.MapFS{
		"page.html":       {Data: []byte(`<h1>{{.Title}}</h1>`)},
		"errors/500.html": {Data: []byte(`<h1>Error: {{.Error}}</h1><p>Code: {{.StatusCode}}</p>`)},
	}

	engine := New(Config{
		FileSystem: testFS,
		Directory:  ".",
	})
	if err := engine.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	r := rig.New()
	r.Use(engine.Middleware())
	r.GET("/", func(c *rig.Context) error {
		// Try to render a non-existent template
		return HTMLSafe(c, http.StatusOK, "nonexistent", nil, "errors/500")
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should have rendered the error page with 500 status
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "Error:") {
		t.Errorf("Expected error page content, got: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Code: 500") {
		t.Errorf("Expected status code in error page, got: %s", w.Body.String())
	}
}
