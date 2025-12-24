// Package swagger provides Swagger UI support for Rig.
// This is a separate package to keep the core Rig framework dependency-free.
package swagger

import (
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudresty/rig"
	swaggerFiles "github.com/swaggo/files/v2"
	"github.com/swaggo/swag"
)

// Swagger provides Swagger UI serving capabilities.
type Swagger struct {
	specJSON     string
	title        string
	deepLinking  bool
	docExpansion string
}

// New creates a new Swagger UI server with the given OpenAPI/Swagger spec JSON.
func New(specJSON string) *Swagger {
	return &Swagger{
		specJSON:     specJSON,
		title:        "API Documentation",
		deepLinking:  true,
		docExpansion: "list",
	}
}

// NewFromSwag creates a Swagger UI server using swaggo/swag's registered spec.
// The instanceName is typically "swagger" unless you registered a custom name.
// Requires importing your generated docs package: _ "myapp/docs"
func NewFromSwag(instanceName string) *Swagger {
	doc, err := swag.ReadDoc(instanceName)
	if err != nil {
		doc = `{"openapi":"3.0.0","info":{"title":"API","version":"1.0"},"paths":{}}`
	}
	return New(doc)
}

// WithTitle sets the page title for the Swagger UI.
func (s *Swagger) WithTitle(title string) *Swagger {
	s.title = title
	return s
}

// WithDeepLinking enables or disables deep linking in Swagger UI.
// When enabled, the URL updates as you navigate the documentation.
// Default: true
func (s *Swagger) WithDeepLinking(enabled bool) *Swagger {
	s.deepLinking = enabled
	return s
}

// WithDocExpansion sets the default expansion mode for operations.
// Valid values: "list" (default), "full", "none"
func (s *Swagger) WithDocExpansion(mode string) *Swagger {
	switch mode {
	case "list", "full", "none":
		s.docExpansion = mode
	}
	return s
}

// Register registers Swagger UI routes at the given path prefix.
// Example: s.Register(router, "/docs") serves UI at /docs/
func (s *Swagger) Register(r *rig.Router, pathPrefix string) {
	s.register(r, pathPrefix)
}

// RegisterGroup registers Swagger UI routes on a route group.
// Example: s.RegisterGroup(apiGroup, "/docs") serves UI at /api/docs/
func (s *Swagger) RegisterGroup(g *rig.RouteGroup, pathPrefix string) {
	s.registerGroup(g, pathPrefix)
}

func (s *Swagger) register(r *rig.Router, pathPrefix string) {
	pathPrefix = normalizePath(pathPrefix)

	r.GET(pathPrefix+"/doc.json", s.serveSpec())
	r.GET(pathPrefix+"/", s.serveIndex(pathPrefix))
	r.GET(pathPrefix+"/index.html", s.serveIndex(pathPrefix))
	r.GET(pathPrefix+"/swagger-ui.css", s.serveStatic("swagger-ui.css", "text/css; charset=utf-8"))
	r.GET(pathPrefix+"/swagger-ui-bundle.js", s.serveStatic("swagger-ui-bundle.js", "application/javascript; charset=utf-8"))
	r.GET(pathPrefix+"/swagger-ui-standalone-preset.js", s.serveStatic("swagger-ui-standalone-preset.js", "application/javascript; charset=utf-8"))
	r.GET(pathPrefix+"/favicon-32x32.png", s.serveStatic("favicon-32x32.png", "image/png"))
	r.GET(pathPrefix+"/favicon-16x16.png", s.serveStatic("favicon-16x16.png", "image/png"))
	r.GET(pathPrefix, s.serveRedirect(pathPrefix+"/"))
}

func (s *Swagger) registerGroup(g *rig.RouteGroup, pathPrefix string) {
	pathPrefix = normalizePath(pathPrefix)

	g.GET(pathPrefix+"/doc.json", s.serveSpec())
	g.GET(pathPrefix+"/", s.serveIndex(pathPrefix))
	g.GET(pathPrefix+"/index.html", s.serveIndex(pathPrefix))
	g.GET(pathPrefix+"/swagger-ui.css", s.serveStatic("swagger-ui.css", "text/css; charset=utf-8"))
	g.GET(pathPrefix+"/swagger-ui-bundle.js", s.serveStatic("swagger-ui-bundle.js", "application/javascript; charset=utf-8"))
	g.GET(pathPrefix+"/swagger-ui-standalone-preset.js", s.serveStatic("swagger-ui-standalone-preset.js", "application/javascript; charset=utf-8"))
	g.GET(pathPrefix+"/favicon-32x32.png", s.serveStatic("favicon-32x32.png", "image/png"))
	g.GET(pathPrefix+"/favicon-16x16.png", s.serveStatic("favicon-16x16.png", "image/png"))
	g.GET(pathPrefix, s.serveRedirect(pathPrefix+"/"))
}

func normalizePath(prefix string) string {
	if prefix == "" {
		prefix = "/docs"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimSuffix(prefix, "/")
}

func (s *Swagger) serveSpec() rig.HandlerFunc {
	return func(c *rig.Context) error {
		c.Writer().Header().Set("Content-Type", "application/json; charset=utf-8")
		_, err := c.Writer().Write([]byte(s.specJSON))
		return err
	}
}

func (s *Swagger) serveRedirect(target string) rig.HandlerFunc {
	return func(c *rig.Context) error {
		http.Redirect(c.Writer(), c.Request(), target, http.StatusMovedPermanently)
		return nil
	}
}

func (s *Swagger) serveIndex(pathPrefix string) rig.HandlerFunc {
	tmpl := template.Must(template.New("swagger").Parse(indexTemplate))
	return func(c *rig.Context) error {
		c.Writer().Header().Set("Content-Type", "text/html; charset=utf-8")
		return tmpl.Execute(c.Writer(), map[string]any{
			"Title":        s.title,
			"SpecURL":      pathPrefix + "/doc.json",
			"DeepLinking":  s.deepLinking,
			"DocExpansion": s.docExpansion,
		})
	}
}

func (s *Swagger) serveStatic(filename, contentType string) rig.HandlerFunc {
	return func(c *rig.Context) error {
		c.Writer().Header().Set("Content-Type", contentType)
		file, err := swaggerFiles.FS.Open(filename)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		http.ServeContent(c.Writer(), c.Request(), filename, time.Time{}, file.(io.ReadSeeker))
		return nil
	}
}

const indexTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" type="text/css" href="./swagger-ui.css">
    <link rel="icon" type="image/png" href="./favicon-32x32.png" sizes="32x32">
    <link rel="icon" type="image/png" href="./favicon-16x16.png" sizes="16x16">
    <style>
        html { box-sizing: border-box; overflow-y: scroll; }
        *, *:before, *:after { box-sizing: inherit; }
        body { margin: 0; background: #fafafa; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="./swagger-ui-bundle.js" charset="UTF-8"></script>
    <script src="./swagger-ui-standalone-preset.js" charset="UTF-8"></script>
    <script>
        window.onload = function() {
            window.ui = SwaggerUIBundle({
                url: "{{.SpecURL}}",
                dom_id: '#swagger-ui',
                deepLinking: {{.DeepLinking}},
                docExpansion: "{{.DocExpansion}}",
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout"
            });
        };
    </script>
</body>
</html>`
