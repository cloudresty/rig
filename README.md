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

- **Zero Dependencies** - Only the Go standard library
- **Go 1.22+ Pattern Matching** - Full support for method routing and path parameters
- **Middleware** - Global, group, and per-route middleware with onion-style execution
- **Route Groups** - Organize routes with shared prefixes and middleware
- **JSON Handling** - `Bind`, `BindStrict`, and `JSON` response helpers
- **Static Files** - Serve directories with a single line
- **Production Middleware** - Built-in `Recover` and `CORS` middleware
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
|------------|-------------|
| `Recover()` | Catches panics and returns a 500 JSON error |
| `DefaultCORS()` | Permissive CORS (allows all origins) |
| `CORS(config)` | Configurable CORS with specific origins/methods/headers |

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

// Restrictive
r.Use(rig.CORS(rig.CORSConfig{
    AllowOrigins: []string{"https://myapp.com", "https://admin.myapp.com"},
    AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders: []string{"Content-Type", "Authorization"},
}))
```

&nbsp;

🔝 [back to top](#rig)

&nbsp;

## API Reference

### Context Methods

| Method | Description |
|--------|-------------|
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
|--------|-------------|
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

<sub>&copy; 2025 Cloudresty</sub>

&nbsp;
