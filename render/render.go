// Package render provides HTML template rendering for the rig HTTP library.
//
// It wraps Go's html/template package with a convenient API for web applications,
// supporting template caching, layouts, partials, hot reloading, custom functions,
// and content negotiation.
//
// # Basic Usage
//
//	engine := render.New(render.Config{
//	    Directory: "./templates",
//	})
//	r := rig.New()
//	r.Use(engine.Middleware())
//
//	r.GET("/", func(c *rig.Context) error {
//	    return render.HTML(c, http.StatusOK, "home", map[string]any{
//	        "Title": "Welcome",
//	    })
//	})
//
// # With Layouts
//
//	engine := render.New(render.Config{
//	    Directory: "./templates",
//	    Layout:    "layouts/base",
//	})
//
// Templates can use {{.Content}} to include the page content.
//
// # Partials
//
// Files starting with underscore (e.g., _sidebar.html) are partials.
// They are automatically available to all templates:
//
//	{{template "_sidebar" .}}
//
// # Shared Directories (Component-Based Architecture)
//
// For larger applications, use SharedDirs to designate entire directories
// as globally available partials. This enables component-based architecture:
//
//	engine := render.New(render.Config{
//	    Directory: "./templates",
//	    Layout:    "layouts/base",
//	    SharedDirs: []string{"components", "layouts", "base"},
//	})
//
// Templates in shared directories can be included from any feature template:
//
//	{{template "components/button" .}}
//	{{template "base/modal" .}}
//
// This allows feature pages (e.g., features/dashboard/index) to remain isolated
// while sharing common components without naming conflicts.
//
// # Content Negotiation
//
//	// Returns HTML or JSON based on Accept header
//	render.Auto(c, http.StatusOK, "dashboard", data)
package render

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/cloudresty/rig"
)

// ContextKey is the key used to store the Engine in the rig context.
const ContextKey = "render.engine"

// Content types for response headers.
const (
	ContentTypeHTML = "text/html; charset=utf-8"
	ContentTypeJSON = "application/json; charset=utf-8"
	ContentTypeXML  = "application/xml; charset=utf-8"
)

// Config defines the configuration for the template engine.
type Config struct {
	// FileSystem allows loading templates from an embedded filesystem (embed.FS).
	// If nil, templates are loaded from the OS filesystem using Directory.
	// When set, Directory specifies the subdirectory within the FileSystem.
	//
	// Example with embed.FS:
	//   //go:embed templates/*
	//   var templateFS embed.FS
	//
	//   engine := render.New(render.Config{
	//       FileSystem: templateFS,
	//       Directory:  "templates",
	//   })
	FileSystem fs.FS

	// Directory is the root directory containing template files.
	// When FileSystem is nil, this is a path on the OS filesystem.
	// When FileSystem is set, this is a path within that filesystem.
	// Default: "templates".
	Directory string

	// Extensions is the list of file extensions to consider as templates.
	// Default: []string{".html", ".tmpl"}.
	Extensions []string

	// SharedDirs is a list of directories (relative to Directory) containing
	// templates that should be available globally to all other templates.
	//
	// Templates in these directories are treated as partials, meaning they
	// are loaded into the shared namespace and can be included from any template.
	// This enables component-based architecture without requiring underscore prefixes.
	//
	// Example:
	//   SharedDirs: []string{"components", "layouts", "base"}
	//
	// With this config, templates in "components/button.html" can be included
	// from any feature template using {{template "components/button" .}}
	//
	// This works alongside the underscore convention - files starting with "_"
	// are still treated as partials regardless of their directory.
	SharedDirs []string

	// Layout is the name of the base layout template (without extension).
	// If set, all templates will be rendered within this layout.
	// The layout should contain {{.Content}} to include page content.
	// Data passed to the template is available via {{.Data}}.
	// Default: "" (no layout).
	Layout string

	// DevMode enables hot reloading of templates on each request.
	// This is useful during development but should be disabled in production.
	// Default: false.
	DevMode bool

	// Funcs is a map of custom template functions.
	// These are available in all templates.
	Funcs template.FuncMap

	// Delims sets custom action delimiters for templates.
	// Useful when integrating with frontend frameworks like Vue.js, Angular,
	// or Alpine.js that also use {{ }} syntax.
	//
	// Example: []string{"[[", "]]"} changes Go templates to use [[ .Title ]]
	// Default: []string{"{{", "}}"} (standard Go template delimiters).
	Delims []string

	// Minify removes unnecessary whitespace from HTML output.
	// This can reduce bandwidth and improve page load times in production.
	// Default: false.
	Minify bool
}

// Engine is the template rendering engine.
type Engine struct {
	config     Config
	templates  map[string]*template.Template
	partials   *template.Template // Shared partials template
	layoutName string
	funcs      template.FuncMap
	mu         sync.RWMutex
}

// New creates a new template engine with the given configuration.
func New(config Config) *Engine {
	// Apply defaults
	if config.Directory == "" {
		config.Directory = "templates"
	}
	if len(config.Extensions) == 0 {
		config.Extensions = []string{".html", ".tmpl"}
	}

	e := &Engine{
		config:    config,
		templates: make(map[string]*template.Template),
		funcs:     make(template.FuncMap),
	}

	// Add default functions
	e.funcs["safe"] = func(s string) template.HTML {
		return template.HTML(s) //nolint:gosec // Intentional for trusted content
	}
	e.funcs["safeAttr"] = func(s string) template.HTMLAttr {
		return template.HTMLAttr(s) //nolint:gosec // Intentional for trusted content
	}
	e.funcs["safeURL"] = func(s string) template.URL {
		return template.URL(s) //nolint:gosec // Intentional for trusted content
	}
	e.funcs["dump"] = func(v any) template.HTML {
		b, _ := json.MarshalIndent(v, "", "  ")
		return template.HTML("<pre>" + string(b) + "</pre>") //nolint:gosec // Debug output
	}

	// Merge custom functions
	maps.Copy(e.funcs, config.Funcs)

	return e
}

// templateFile holds information about a discovered template file.
type templateFile struct {
	name    string
	path    string
	content string
}

// Load loads all templates from the configured directory.
// This is called automatically by Middleware(), but can be called manually
// to pre-load templates at startup.
//
// Templates are loaded into a shared set, allowing them to reference each other.
// Files starting with underscore (e.g., _sidebar.html) are treated as partials
// and are automatically available to all templates.
//
// When Config.FileSystem is set (e.g., embed.FS), templates are loaded from that
// filesystem. Otherwise, templates are loaded from the OS filesystem.
func (e *Engine) Load() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.templates = make(map[string]*template.Template)
	e.partials = nil
	e.layoutName = ""

	// Setup the filesystem
	// If FileSystem is provided, use it (e.g., embed.FS)
	// Otherwise, use OS filesystem rooted at Directory
	var fsys fs.FS
	if e.config.FileSystem != nil {
		// Use the provided filesystem, scoped to Directory
		if e.config.Directory != "" && e.config.Directory != "." {
			var err error
			fsys, err = fs.Sub(e.config.FileSystem, e.config.Directory)
			if err != nil {
				return fmt.Errorf("failed to access directory %q in filesystem: %w", e.config.Directory, err)
			}
		} else {
			fsys = e.config.FileSystem
		}
	} else {
		// Use OS filesystem
		fsys = os.DirFS(e.config.Directory)
	}

	// Collect all template files
	var files []templateFile
	var partialFiles []templateFile

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Check if file has a valid extension
		ext := filepath.Ext(path)
		if !e.isValidExtension(ext) {
			return nil
		}

		// Get template name (path without extension, normalized to forward slashes)
		name := strings.TrimSuffix(path, ext)
		name = filepath.ToSlash(name)

		// Read the template content from the filesystem
		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", name, err)
		}

		tf := templateFile{name: name, path: path, content: string(content)}

		// Check if this is a shared partial:
		// - filename starts with "_" (legacy convention), OR
		// - file resides in a SharedDirs directory
		if e.isShared(path) {
			partialFiles = append(partialFiles, tf)
		} else {
			files = append(files, tf)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// First, create a base template with all partials
	// This allows partials to be available to all templates
	if len(partialFiles) > 0 {
		e.partials = e.applyDelims(template.New("__partials__")).Funcs(e.funcs)
		for _, pf := range partialFiles {
			_, err := e.partials.New(pf.name).Parse(pf.content)
			if err != nil {
				return fmt.Errorf("failed to parse partial %s: %w", pf.name, err)
			}
		}
	}

	// Now parse each template, cloning the partials so they're available
	for _, tf := range files {
		var tmpl *template.Template
		if e.partials != nil {
			// Clone partials so each template has access to them
			var err error
			tmpl, err = e.partials.Clone()
			if err != nil {
				return fmt.Errorf("failed to clone partials for %s: %w", tf.name, err)
			}
			// Parse the main template content into the cloned template
			_, err = tmpl.New(tf.name).Parse(tf.content)
			if err != nil {
				return fmt.Errorf("failed to parse template %s: %w", tf.name, err)
			}
		} else {
			// No partials, create a new template
			var err error
			tmpl, err = e.applyDelims(template.New(tf.name)).Funcs(e.funcs).Parse(tf.content)
			if err != nil {
				return fmt.Errorf("failed to parse template %s: %w", tf.name, err)
			}
		}

		e.templates[tf.name] = tmpl
	}

	// Store layout name if specified
	if e.config.Layout != "" {
		if _, ok := e.templates[e.config.Layout]; !ok {
			return fmt.Errorf("layout template %q not found", e.config.Layout)
		}
		e.layoutName = e.config.Layout
	}

	return nil
}

// isValidExtension checks if the given extension is in the allowed list.
func (e *Engine) isValidExtension(ext string) bool {
	return slices.Contains(e.config.Extensions, ext)
}

// isShared checks if a template file should be treated as a shared partial.
// It returns true if:
//  1. The filename starts with underscore (legacy convention), OR
//  2. The file resides inside one of the configured SharedDirs
func (e *Engine) isShared(path string) bool {
	// Rule 1: Filename starts with underscore (legacy convention)
	baseName := filepath.Base(path)
	if strings.HasPrefix(baseName, "_") {
		return true
	}

	// Rule 2: File resides in a Shared Directory
	if len(e.config.SharedDirs) == 0 {
		return false
	}

	// Normalize path separators to forward slashes for comparison
	normalizedPath := filepath.ToSlash(path)

	for _, dir := range e.config.SharedDirs {
		// Normalize dir and ensure no trailing slash
		cleanDir := strings.TrimSuffix(filepath.ToSlash(dir), "/")

		// Check if path starts with dir/
		// We add a trailing slash to ensure we match directory boundaries
		// e.g., "components" matches "components/button.html" but not "components-extra/file.html"
		if strings.HasPrefix(normalizedPath, cleanDir+"/") {
			return true
		}
	}

	return false
}

// applyDelims applies custom delimiters to a template if configured.
func (e *Engine) applyDelims(t *template.Template) *template.Template {
	if len(e.config.Delims) == 2 {
		return t.Delims(e.config.Delims[0], e.config.Delims[1])
	}
	return t
}

// Middleware returns a rig middleware that injects the engine into the context.
// It also loads templates on first request (and on each request in DevMode).
func (e *Engine) Middleware() rig.MiddlewareFunc {
	var loaded bool
	var loadMu sync.Mutex

	return func(next rig.HandlerFunc) rig.HandlerFunc {
		return func(c *rig.Context) error {
			// Load or reload templates
			if e.config.DevMode || !loaded {
				loadMu.Lock()
				if e.config.DevMode || !loaded {
					if err := e.Load(); err != nil {
						loadMu.Unlock()
						return fmt.Errorf("failed to load templates: %w", err)
					}
					loaded = true
				}
				loadMu.Unlock()
			}

			// Store engine in context
			c.Set(ContextKey, e)

			return next(c)
		}
	}
}

// Render renders a template by name with the given data.
func (e *Engine) Render(name string, data any) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	tmpl, ok := e.templates[name]
	if !ok {
		return "", fmt.Errorf("template %q not found", name)
	}

	var buf bytes.Buffer

	// If we have a layout, render the content template first, then the layout
	if e.layoutName != "" && name != e.layoutName {
		// Render content template - execute the named template within the set
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			return "", fmt.Errorf("failed to execute template %s: %w", name, err)
		}

		// Create layout data using the "View Bag" pattern.
		// This wraps the original data in .Data so both maps and structs work correctly.
		// In the layout template:
		//   - Use {{.Content}} for the rendered page content
		//   - Use {{.Data.Title}} to access fields from the original data
		layoutData := map[string]any{
			"Content": template.HTML(buf.String()), //nolint:gosec // Content is from our own templates
			"Data":    data,                        // Original data is always available via .Data
		}

		// For backward compatibility, also merge map fields at the top level
		// This allows {{.Title}} in layouts when data is a map
		if dataMap, ok := data.(map[string]any); ok {
			maps.Copy(layoutData, dataMap)
		}

		// Get the layout template and render it
		layoutTmpl, ok := e.templates[e.layoutName]
		if !ok {
			return "", fmt.Errorf("layout template %q not found", e.layoutName)
		}

		buf.Reset()
		if err := layoutTmpl.ExecuteTemplate(&buf, e.layoutName, layoutData); err != nil {
			return "", fmt.Errorf("failed to execute layout: %w", err)
		}
	} else {
		// No layout, render template directly
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			return "", fmt.Errorf("failed to execute template %s: %w", name, err)
		}
	}

	result := buf.String()
	if e.config.Minify {
		result = minifyHTML(result)
	}
	return result, nil
}

// HTML renders a template and writes it as an HTML response.
// It retrieves the engine from the context (set by Middleware).
func HTML(c *rig.Context, status int, name string, data any) error {
	engine := GetEngine(c)
	if engine == nil {
		return fmt.Errorf("render engine not found in context; did you forget to use engine.Middleware()?")
	}

	content, err := engine.Render(name, data)
	if err != nil {
		return err
	}

	c.SetHeader("Content-Type", ContentTypeHTML)
	c.Status(status)
	_, err = c.WriteString(content)
	return err
}

// HTMLDirect renders a template using the provided engine directly.
// This is useful when you don't want to use middleware.
func HTMLDirect(c *rig.Context, engine *Engine, status int, name string, data any) error {
	content, err := engine.Render(name, data)
	if err != nil {
		return err
	}

	c.SetHeader("Content-Type", ContentTypeHTML)
	c.Status(status)
	_, err = c.WriteString(content)
	return err
}

// HTMLSafe renders a template with automatic error page fallback.
// If the primary template fails to render, it attempts to render an error
// template (e.g., "errors/500" or "500") with the error details.
//
// This is useful in production to show pretty error pages instead of
// returning raw error messages to users.
//
// Example:
//
//	r.GET("/page", func(c *rig.Context) error {
//	    return render.HTMLSafe(c, http.StatusOK, "page", data, "errors/500")
//	})
//
// The error template receives: {"Error": "error message", "StatusCode": 500}
func HTMLSafe(c *rig.Context, status int, name string, data any, errorTemplate string) error {
	err := HTML(c, status, name, data)
	if err != nil {
		// Attempt to render the error template
		errorData := map[string]any{
			"Error":      err.Error(),
			"StatusCode": 500,
		}
		// Ignore errors from error template rendering - best effort
		_ = HTML(c, 500, errorTemplate, errorData)
		return err
	}
	return nil
}

// JSON renders data as a JSON response.
func JSON(c *rig.Context, status int, data any) error {
	c.SetHeader("Content-Type", ContentTypeJSON)
	c.Status(status)

	encoder := json.NewEncoder(c.Writer())
	encoder.SetEscapeHTML(true)
	return encoder.Encode(data)
}

// XML renders data as an XML response.
func XML(c *rig.Context, status int, data any) error {
	c.SetHeader("Content-Type", ContentTypeXML)
	c.Status(status)

	if _, err := c.WriteString(xml.Header); err != nil {
		return err
	}

	encoder := xml.NewEncoder(c.Writer())
	encoder.Indent("", "  ")
	return encoder.Encode(data)
}

// Auto performs content negotiation based on the Accept header.
// It renders HTML (using the template) for browsers, or JSON for API clients.
// If a template name is empty, only JSON/XML responses are supported.
func Auto(c *rig.Context, status int, templateName string, data any) error {
	accept := c.Request().Header.Get("Accept")

	// Check for JSON preference
	if strings.Contains(accept, "application/json") {
		return JSON(c, status, data)
	}

	// Check for HTML preference (browsers send text/html first in Accept header)
	// This must come before XML check because browsers also include application/xml
	if templateName != "" && (strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")) {
		return HTML(c, status, templateName, data)
	}

	// Check for XML preference
	if strings.Contains(accept, "application/xml") || strings.Contains(accept, "text/xml") {
		return XML(c, status, data)
	}

	// Default to HTML if template is provided
	if templateName != "" {
		return HTML(c, status, templateName, data)
	}

	// No template, fall back to JSON
	return JSON(c, status, data)
}

// AutoDirect performs content negotiation using a specific engine.
func AutoDirect(c *rig.Context, engine *Engine, status int, templateName string, data any) error {
	accept := c.Request().Header.Get("Accept")

	// Check for JSON preference
	if strings.Contains(accept, "application/json") {
		return JSON(c, status, data)
	}

	// Check for HTML preference (browsers send text/html first in Accept header)
	// This must come before XML check because browsers also include application/xml
	if templateName != "" && (strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")) {
		return HTMLDirect(c, engine, status, templateName, data)
	}

	// Check for XML preference
	if strings.Contains(accept, "application/xml") || strings.Contains(accept, "text/xml") {
		return XML(c, status, data)
	}

	// Default to HTML if template is provided
	if templateName != "" {
		return HTMLDirect(c, engine, status, templateName, data)
	}

	// No template, fall back to JSON
	return JSON(c, status, data)
}

// GetEngine retrieves the render engine from the context.
// Returns nil if not found.
func GetEngine(c *rig.Context) *Engine {
	if val, ok := c.Get(ContextKey); ok {
		if engine, ok := val.(*Engine); ok {
			return engine
		}
	}
	return nil
}

// AddFunc adds a custom template function.
// This method is thread-safe and can be called concurrently, but should
// typically be called before Load() or Middleware() for best results.
func (e *Engine) AddFunc(name string, fn any) *Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.funcs[name] = fn
	return e
}

// AddFuncs adds multiple custom template functions.
// This method is thread-safe and can be called concurrently, but should
// typically be called before Load() or Middleware() for best results.
func (e *Engine) AddFuncs(funcs template.FuncMap) *Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	maps.Copy(e.funcs, funcs)
	return e
}

// TemplateNames returns a list of all loaded template names.
// This is useful for debugging.
func (e *Engine) TemplateNames() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	names := make([]string, 0, len(e.templates))
	for name := range e.templates {
		names = append(names, name)
	}
	return names
}

// PartialNames returns a list of all loaded partial names.
// Partials are templates whose filename starts with underscore.
func (e *Engine) PartialNames() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.partials == nil {
		return nil
	}

	var names []string
	for _, t := range e.partials.Templates() {
		if t.Name() != "__partials__" {
			names = append(names, t.Name())
		}
	}
	return names
}
