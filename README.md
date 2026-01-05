# Rig

[Home](README.md) &nbsp;/

&nbsp;

**Rig** is a lightweight HTTP framework for Go that wraps `net/http` with zero external dependencies. Built for Go 1.22+, it provides the ergonomics of popular frameworks while staying true to the standard library.

&nbsp;

[![Go Reference](https://pkg.go.dev/badge/github.com/cloudresty/rig.svg)](https://pkg.go.dev/github.com/cloudresty/rig)
[![Go Tests](https://github.com/cloudresty/rig/actions/workflows/ci.yaml/badge.svg)](https://github.com/cloudresty/rig/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloudresty/rig)](https://goreportcard.com/report/github.com/cloudresty/rig)
[![GitHub Tag](https://img.shields.io/github/v/tag/cloudresty/rig?label=Version)](https://github.com/cloudresty/rig/tags)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

&nbsp;

## Features

- **Zero Dependencies** - Only the Go standard library (core package)
- **Go 1.22+ Pattern Matching** - Full support for method routing and path parameters
- **Middleware** - Global, group, and per-route middleware with onion-style execution
- **Route Groups** - Organize routes with shared prefixes and middleware
- **JSON Handling** - `Bind`, `BindStrict`, and `JSON` response helpers
- **Static Files** - Serve directories with a single line
- **Production Middleware** - Built-in `Recover` and `CORS` middleware
- **Health Checks** - Liveness and readiness probes for Kubernetes
- **HTML Templates** - Template rendering with layouts, partials, embed.FS, and content negotiation (`render/` sub-package)
- **Authentication** - API Key and Bearer Token middleware (`auth/` sub-package)
- **Request ID** - ULID-based request tracking (`requestid/` sub-package)
- **Logging** - Structured request logging with JSON support (`logger/` sub-package)
- **Swagger UI** - Optional sub-package for API documentation
- **Type-Safe Context** - Generic `GetType[T]` for dependency injection
- **99%+ Test Coverage** - Battle-tested and production-ready

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Installation

```bash
go get github.com/cloudresty/rig
```

Requires **Go 1.22** or later.

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Quick Start

```go
package main

import (
    "net/http"

    "github.com/cloudresty/rig"
)

func main() {
    r := rig.New()

    // Add middleware
    r.Use(rig.Recover())
    r.Use(rig.DefaultCORS())

    // Simple route
    r.GET("/", func(c *rig.Context) error {
        return c.JSON(http.StatusOK, map[string]string{
            "message": "Hello, World!",
        })
    })

    // Path parameters (Go 1.22+)
    r.GET("/users/{id}", func(c *rig.Context) error {
        id := c.Param("id")
        return c.JSON(http.StatusOK, map[string]string{
            "user_id": id,
        })
    })

    http.ListenAndServe(":8080", r)
}
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Route Groups

Organize routes with shared prefixes and middleware:

```go
r := rig.New()

// API group with authentication middleware
api := r.Group("/api")
api.Use(authMiddleware)

api.GET("/users", listUsers)       // GET /api/users
api.POST("/users", createUser)     // POST /api/users
api.GET("/users/{id}", getUser)    // GET /api/users/{id}

// Nested groups
v1 := api.Group("/v1")
v1.GET("/status", getStatus)       // GET /api/v1/status
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Middleware

Middleware follows the decorator pattern with onion-style execution:

```go
// Custom middleware
func Logger() rig.MiddlewareFunc {
    return func(next rig.HandlerFunc) rig.HandlerFunc {
        return func(c *rig.Context) error {
            start := time.Now()
            err := next(c)
            log.Printf("%s %s %v", c.Method(), c.Path(), time.Since(start))
            return err
        }
    }
}

// Apply middleware
r.Use(rig.Recover())    // Global - catches panics
r.Use(rig.DefaultCORS()) // Global - enables CORS
r.Use(Logger())          // Global - logs requests
```

&nbsp;

### Built-in Middleware

| Middleware | Description |
| :--- | :--- |
| `Recover()` | Catches panics and returns a 500 JSON error |
| `DefaultCORS()` | Permissive CORS (allows all origins) |
| `CORS(config)` | Configurable CORS with specific origins/methods/headers |

&nbsp;

**Additional middleware sub-packages** (see sections below):

| Package | Description |
| :--- | :--- |
| `auth/` | API Key and Bearer Token authentication |
| `requestid/` | ULID-based request ID generation |
| `logger/` | Structured request logging (text/JSON) |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Request Handling

### Path Parameters

```go
r.GET("/users/{id}", func(c *rig.Context) error {
    id := c.Param("id")
    // ...
})
```

&nbsp;

### Query Parameters

```go
r.GET("/search", func(c *rig.Context) error {
    q := c.Query("q")                    // Single value
    tags := c.QueryArray("tag")          // Multiple values: ?tag=a&tag=b
    page := c.QueryDefault("page", "1")  // With default
    // ...
})
```

&nbsp;

### JSON Body

```go
r.POST("/users", func(c *rig.Context) error {
    var user User
    if err := c.Bind(&user); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
    }
    // Use c.BindStrict(&user) to reject unknown fields
    // ...
})
```

&nbsp;

### Form Data

```go
r.POST("/login", func(c *rig.Context) error {
    username := c.FormValue("username")      // Body takes precedence over query
    password := c.PostFormValue("password")  // Body only
    // ...
})
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Response Helpers

```go
// JSON response
c.JSON(http.StatusOK, data)

// String response
c.WriteString("Hello")

// Redirect
c.Redirect(http.StatusFound, "/new-location")

// Serve a file
c.File("./reports/monthly.pdf")

// Raw bytes
c.Data(http.StatusOK, "image/png", pngBytes)

// Set status only
c.Status(http.StatusNoContent)
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Static Files

Serve a directory of static files:

```go
r.Static("/assets", "./public")
// GET /assets/css/style.css → serves ./public/css/style.css
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Dependency Injection

Use the context store for request-scoped values:

```go
// Middleware: inject dependencies
func InjectDB(db *Database) rig.MiddlewareFunc {
    return func(next rig.HandlerFunc) rig.HandlerFunc {
        return func(c *rig.Context) error {
            c.Set("db", db)
            return next(c)
        }
    }
}

// Handler: retrieve with type safety
r.GET("/users", func(c *rig.Context) error {
    db, err := rig.GetType[*Database](c, "db")
    if err != nil {
        return err
    }
    // Use db...
})
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## CORS Configuration

```go
// Permissive (all origins)
r.Use(rig.DefaultCORS())

// Restrictive with exact origins
r.Use(rig.CORS(rig.CORSConfig{
    AllowOrigins: []string{"https://myapp.com", "https://admin.myapp.com"},
    AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders: []string{"Content-Type", "Authorization"},
}))

// Wildcard subdomain support
r.Use(rig.CORS(rig.CORSConfig{
    AllowOrigins: []string{
        "https://*.myapp.com",           // Matches any subdomain
        "https://*.staging.myapp.com",   // Matches nested subdomains
        "https://api.production.com",    // Exact match also works
    },
    AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders: []string{"Content-Type", "Authorization"},
}))
```

&nbsp;

**AllowOrigins patterns:**

| Pattern | Matches | Does Not Match |
| :--- | :--- | :--- |
| `"*"` | All origins | - |
| `"https://example.com"` | Exact match only | `https://sub.example.com` |
| `"https://*.example.com"` | `https://app.example.com`, `https://a.b.example.com` | `https://example.com`, `http://app.example.com` |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Health Checks

Rig provides an opt-in health utility for liveness and readiness probes, perfect for Kubernetes deployments:

```go
func main() {
    r := rig.New()
    db := connectDB()

    // Create the Health manager
    health := rig.NewHealth()

    // Add readiness check (don't send traffic if DB is down)
    health.AddReadinessCheck("database", func() error {
        return db.Ping()
    })

    // Add liveness check (is the app running?)
    health.AddLivenessCheck("ping", func() error {
        return nil // Always healthy
    })

    // Mount the handlers (user chooses the paths)
    h := r.Group("/health")
    h.GET("/live", health.LiveHandler())
    h.GET("/ready", health.ReadyHandler())

    r.Run(":8080")
}
```

&nbsp;

**Response format:**

```json
// GET /health/ready (all checks pass)
{ "status": "OK", "checks": { "database": "OK" } }

// GET /health/ready (a check fails)
{ "status": "Service Unavailable", "checks": { "database": "FAIL: connection refused" } }
```

&nbsp;

| Method | Description |
| :--- | :--- |
| `NewHealth()` | Creates a new Health manager |
| `AddReadinessCheck(name, fn)` | Adds a check for traffic readiness (DB, Redis, etc.) |
| `AddLivenessCheck(name, fn)` | Adds a check for app liveness (deadlock detection, etc.) |
| `LiveHandler()` | Returns a handler for liveness probes |
| `ReadyHandler()` | Returns a handler for readiness probes |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Swagger UI

Rig provides optional Swagger UI support via a separate sub-package to keep the core framework dependency-free:

```bash
go get github.com/cloudresty/rig/swagger
```

&nbsp;

### Basic Usage with swaggo/swag

```go
package main

import (
    "github.com/cloudresty/rig"
    "github.com/cloudresty/rig/swagger"
    _ "myapp/docs" // Generated by: swag init
)

func main() {
    r := rig.New()

    // Your API routes
    r.GET("/api/v1/users", handleUsers)

    // Register Swagger UI at /docs/
    sw := swagger.NewFromSwag("swagger")
    sw.Register(r, "/docs")

    r.Run(":8080")
}
// Access Swagger UI at http://localhost:8080/docs/
```

&nbsp;

### Usage with Custom Spec

```go
spec := `{"openapi":"3.0.0","info":{"title":"My API","version":"1.0"}}`
sw := swagger.New(spec).
    WithTitle("My API Documentation").
    WithDocExpansion("list")
sw.Register(r, "/api-docs")
```

&nbsp;

### Usage with Route Groups

```go
api := r.Group("/api/v1")
sw := swagger.NewFromSwag("swagger")
sw.RegisterGroup(api, "/docs")
// Access at /api/v1/docs/
```

&nbsp;

| Method | Description |
| :--- | :--- |
| `New(specJSON)` | Creates Swagger UI with a JSON spec string |
| `NewFromSwag(name)` | Creates Swagger UI from swaggo/swag registered spec |
| `WithTitle(title)` | Sets the page title |
| `WithDeepLinking(bool)` | Enables/disables URL deep linking (default: true) |
| `WithDocExpansion(mode)` | Sets expansion mode: "list", "full", "none" |
| `Register(router, path)` | Registers routes on a Router |
| `RegisterGroup(group, path)` | Registers routes on a RouteGroup |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Authentication Middleware

The `auth/` sub-package provides API Key and Bearer Token authentication:

```bash
go get github.com/cloudresty/rig/auth
```

&nbsp;

### API Key Authentication

```go
import "github.com/cloudresty/rig/auth"

// Simple: validate against a list of keys
api := r.Group("/api")
api.Use(auth.APIKeySimple("key1", "key2", "key3"))

// Advanced: custom validation with identity
api.Use(auth.APIKey(auth.APIKeyConfig{
    Name:   "X-API-Key",        // Header name (default)
    Source: "header",           // "header" or "query"
    Validator: func(key string) (identity string, valid bool) {
        if key == os.Getenv("API_KEY") {
            return "my-service", true
        }
        return "", false
    },
}))

// In handlers, get the authenticated identity
r.GET("/profile", func(c *rig.Context) error {
    identity := auth.GetIdentity(c)  // Returns identity from Validator
    method := auth.GetMethod(c)      // Returns "api_key" or "bearer"
    return c.JSON(http.StatusOK, map[string]string{"user": identity})
})
```

&nbsp;

### Bearer Token Authentication

```go
api.Use(auth.Bearer(auth.BearerConfig{
    Realm: "API",
    Validator: func(token string) (identity string, valid bool) {
        // Validate JWT or lookup token
        claims, err := validateJWT(token)
        if err != nil {
            return "", false
        }
        return claims.UserID, true
    },
}))
```

&nbsp;

| Function | Description |
| :--- | :--- |
| `APIKeySimple(keys...)` | Simple API key validation (constant-time comparison) |
| `APIKey(config)` | Configurable API key middleware |
| `Bearer(config)` | Bearer token middleware (RFC 6750) |
| `GetIdentity(c)` | Get authenticated identity from context |
| `GetMethod(c)` | Get auth method ("api_key" or "bearer") |
| `IsAuthenticated(c)` | Check if request is authenticated |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Request ID Middleware

The `requestid/` sub-package generates unique request IDs using ULIDs:

```bash
go get github.com/cloudresty/rig/requestid
```

&nbsp;

```go
import "github.com/cloudresty/rig/requestid"

r := rig.New()

// Add request ID middleware (generates ULID for each request)
r.Use(requestid.New())

// With custom configuration
r.Use(requestid.New(requestid.Config{
    Header:     "X-Request-ID",  // Response header (default)
    TrustProxy: true,            // Trust incoming X-Request-ID header
    Generator: func() (string, error) {
        return uuid.New().String(), nil  // Use UUID instead of ULID
    },
}))

// In handlers, get the request ID
r.GET("/", func(c *rig.Context) error {
    reqID := requestid.Get(c)
    return c.JSON(http.StatusOK, map[string]string{
        "request_id": reqID,
    })
})
```

&nbsp;

| Function | Description |
| :--- | :--- |
| `New()` | Create middleware with default config |
| `New(config)` | Create middleware with custom config |
| `Get(c)` | Get request ID from context |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Logger Middleware

The `logger/` sub-package provides structured request logging:

```bash
go get github.com/cloudresty/rig/logger
```

&nbsp;

```go
import "github.com/cloudresty/rig/logger"

r := rig.New()

// Add request ID first (logger will include it)
r.Use(requestid.New())

// Add logger middleware
r.Use(logger.New(logger.Config{
    Format:    logger.FormatJSON,              // FormatText or FormatJSON
    Output:    os.Stdout,                       // io.Writer
    SkipPaths: []string{"/health", "/ready"},  // Don't log these paths
}))
```

&nbsp;

**Text format output:**

```text
2024/01/15 10:30:45 | 200 |    1.234ms | 192.168.1.1 | GET /api/users | req_id: 01HQ...
```

**JSON format output:**

```json
{"time":"2024-01-15T10:30:45Z","status":200,"latency":"1.234ms","latency_ms":1.234,"client_ip":"192.168.1.1","method":"GET","path":"/api/users","request_id":"01HQ..."}
```

&nbsp;

| Option | Description |
| :--- | :--- |
| `Format` | `FormatText` (default) or `FormatJSON` |
| `Output` | `io.Writer` for log output (default: `os.Stdout`) |
| `SkipPaths` | Paths to exclude from logging (e.g., health checks) |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## HTML Template Rendering

The `render` sub-package provides HTML template rendering with layouts, partials, hot reloading, and content negotiation.

```bash
go get github.com/cloudresty/rig/render
```

&nbsp;

### Basic Usage

```go
import (
    "github.com/cloudresty/rig"
    "github.com/cloudresty/rig/render"
)

func main() {
    engine := render.New(render.Config{
        Directory: "./templates",
    })

    r := rig.New()
    r.Use(engine.Middleware())

    r.GET("/", func(c *rig.Context) error {
        return render.HTML(c, http.StatusOK, "home", map[string]any{
            "Title": "Welcome",
            "User":  "John",
        })
    })

    r.Run(":8080")
}
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Embedded Templates (embed.FS)

For single-binary deployments, embed templates directly into your Go binary:

```go
import (
    "embed"
    "github.com/cloudresty/rig/render"
)

//go:embed templates/*
var templateFS embed.FS

func main() {
    engine := render.New(render.Config{
        FileSystem: templateFS,
        Directory:  "templates",
    })
    // Templates are now compiled into the binary!
}
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### With Layouts

```go
engine := render.New(render.Config{
    Directory: "./templates",
    Layout:    "layouts/base", // Base layout template
})
```

&nbsp;

Layout template (`templates/layouts/base.html`):

```html
<!DOCTYPE html>
<html>
<head><title>{{.Data.Title}}</title></head>
<body>
    <header>My Site</header>
    <main>{{.Content}}</main>
    <footer>© 2026</footer>
</body>
</html>
```

&nbsp;

Page template (`templates/home.html`):

```html
<h1>Welcome, {{.User}}!</h1>
```

&nbsp;

**Layout Data Access:**

In layouts, data is available via `{{.Data}}` (works with both structs and maps):

| Expression | Description |
| :--- | :--- |
| `{{.Content}}` | Rendered page content |
| `{{.Data.Title}}` | Access data fields (works with structs!) |
| `{{.Title}}` | Direct access (backward compatible with maps) |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Partials

Templates starting with underscore (`_`) are automatically treated as partials and available to all templates:

```text
templates/
├── _sidebar.html    ← Partial (available everywhere)
├── _footer.html     ← Partial (available everywhere)
├── home.html
└── about.html
```

&nbsp;

Use partials in any template:

```html
<div class="page">
    {{template "_sidebar" .}}
    <main>{{.Content}}</main>
    {{template "_footer" .}}
</div>
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### JSON and XML Responses

Render JSON or XML directly without templates:

```go
// JSON response
r.GET("/api/users", func(c *rig.Context) error {
    return render.JSON(c, http.StatusOK, users)
})

// XML response
r.GET("/api/users.xml", func(c *rig.Context) error {
    return render.XML(c, http.StatusOK, users)
})
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Content Negotiation

Use `Auto()` to automatically select the response format based on the `Accept` header:

```go
r.GET("/users", func(c *rig.Context) error {
    // Returns HTML for browsers, JSON for API clients
    return render.Auto(c, http.StatusOK, "users/list", users)
})
```

&nbsp;

| Accept Header | Response Format |
| :--- | :--- |
| `application/json` | JSON |
| `application/xml` or `text/xml` | XML |
| `text/html` or other | HTML (if template provided) |
| No template provided | JSON (fallback) |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Error Pages (Production)

Use `HTMLSafe()` to automatically render a pretty error page when template rendering fails:

```go
r.GET("/page", func(c *rig.Context) error {
    return render.HTMLSafe(c, http.StatusOK, "page", data, "errors/500")
})
```

&nbsp;

If `"page"` fails to render, it automatically falls back to `"errors/500"` with error details:

```html
<!-- templates/errors/500.html -->
<h1>Something went wrong</h1>
<p>Error: {{.Error}}</p>
<p>Status: {{.StatusCode}}</p>
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Development Mode

Enable hot reloading during development:

```go
engine := render.New(render.Config{
    Directory: "./templates",
    DevMode:   true, // Reloads templates on each request
})
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Custom Template Functions

```go
engine := render.New(render.Config{
    Directory: "./templates",
    Funcs: template.FuncMap{
        "upper": strings.ToUpper,
        "formatDate": func(t time.Time) string {
            return t.Format("Jan 2, 2006")
        },
    },
})
// Or use chained methods:
engine.AddFunc("lower", strings.ToLower)
```

&nbsp;

Use in templates: `{{upper .Name}}` or `{{formatDate .CreatedAt}}`

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Built-in Functions

| Function | Description | Usage |
| :--- | :--- | :--- |
| `safe` | Render trusted HTML without escaping | `{{safe .RawHTML}}` |
| `safeAttr` | Render trusted HTML attribute | `{{safeAttr .Attr}}` |
| `safeURL` | Render trusted URL | `{{safeURL .Link}}` |
| `dump` | Debug helper - outputs data as formatted JSON | `{{dump .}}` |

&nbsp;

The `dump` function is invaluable during development:

```html
<!-- Debug: see what data was passed to the template -->
{{dump .Data}}
```

Outputs:

```html
<pre>{
  "Title": "My Page",
  "User": {
    "Name": "John"
  }
}</pre>
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Using Sprig Functions

For 100+ additional template functions (string manipulation, math, dates, etc.), integrate [Sprig](https://github.com/Masterminds/sprig):

```go
import "github.com/Masterminds/sprig/v3"

engine := render.New(render.Config{
    Directory: "./templates",
})
engine.AddFuncs(sprig.FuncMap())
```

&nbsp;

Now you can use functions like `{{.Name | upper}}`, `{{now | date "2006-01-02"}}`, and many more.

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Custom Delimiters (Vue.js / Angular / Alpine.js)

When using frontend frameworks that also use `{{ }}` syntax, configure custom delimiters:

```go
engine := render.New(render.Config{
    Directory: "./templates",
    Delims:    []string{"[[", "]]"}, // Use [[ ]] for Go templates
})
```

&nbsp;

Now your templates can mix Go and Vue/Angular syntax:

```html
<div>
    <!-- Go template (rendered on server) -->
    <h1>[[ .Title ]]</h1>

    <!-- Vue.js binding (rendered on client) -->
    <p>{{ message }}</p>
</div>
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

### Debugging

List loaded templates and partials:

```go
engine.TemplateNames() // Returns all template names
engine.PartialNames()  // Returns all partial names (files starting with _)
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## Examples

The `examples/` directory contains runnable examples:

| Example | Description |
| :--- | :--- |
| [basic-api](examples/basic-api) | REST API with middleware, dependency injection, and route groups |
| [health-checks](examples/health-checks) | Kubernetes-style liveness and readiness probes |
| [swagger-ui](examples/swagger-ui) | API with integrated Swagger UI documentation |
| [auth-middleware](examples/auth-middleware) | API Key authentication using the `auth/` package |
| [logging](examples/logging) | Request logging with request ID tracking |
| [render-templates](examples/render-templates) | HTML templates with layouts, partials, and content negotiation |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## API Reference

### Context Methods

| Method | Description |
| :--- | :--- |
| `Param(name)` | Get path parameter |
| `Query(key)` | Get query parameter |
| `QueryDefault(key, def)` | Get query parameter with default |
| `QueryArray(key)` | Get all values for a query parameter |
| `FormValue(key)` | Get form value (body precedence) |
| `PostFormValue(key)` | Get form value (body only) |
| `GetHeader(key)` | Get request header |
| `SetHeader(key, value)` | Set response header |
| `Bind(v)` | Decode JSON body |
| `BindStrict(v)` | Decode JSON body (reject unknown fields) |
| `JSON(code, v)` | Send JSON response |
| `Status(code)` | Set status code |
| `Redirect(code, url)` | Send redirect |
| `File(path)` | Serve a file |
| `Data(code, contentType, data)` | Send raw bytes |
| `Set(key, value)` | Store request-scoped value |
| `Get(key)` | Retrieve stored value |
| `MustGet(key)` | Retrieve stored value (panics if missing) |
| `Context()` | Get `context.Context` |
| `SetContext(ctx)` | Set `context.Context` |
| `Request()` | Get `*http.Request` |
| `Writer()` | Get `http.ResponseWriter` |

&nbsp;

### Router Methods

| Method | Description |
| :--- | :--- |
| `New()` | Create a new router |
| `Use(middleware...)` | Add global middleware |
| `Handle(pattern, handler)` | Register a handler |
| `GET/POST/PUT/DELETE/PATCH/OPTIONS/HEAD(path, handler)` | Register method-specific handler |
| `Group(prefix)` | Create a route group |
| `Static(path, root)` | Serve static files |
| `ServeHTTP(w, r)` | Implement `http.Handler` |

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

&nbsp;

🔝 [back to top](#rig)

&nbsp;

&nbsp;

---

### Cloudresty

[Website](https://cloudresty.com) &nbsp;|&nbsp; [LinkedIn](https://www.linkedin.com/company/cloudresty) &nbsp;|&nbsp; [BlueSky](https://bsky.app/profile/cloudresty.com) &nbsp;|&nbsp; [GitHub](https://github.com/cloudresty) &nbsp;|&nbsp; [Docker Hub](https://hub.docker.com/u/cloudresty)

<sub>&copy; Cloudresty - All rights reserved</sub>

&nbsp;
