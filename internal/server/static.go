package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// dist holds the built frontend. `make build` writes the Vite output here;
// the tracked .gitkeep keeps `go build` working on a clean checkout that has
// not produced frontend assets yet.
//
//go:embed all:dist
var dist embed.FS

// spaHandler serves the embedded single-page application with history-API
// fallback to index.html for unknown non-API paths.
func (s *Server) spaHandler() gin.HandlerFunc {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Only reachable if the embed directive itself is broken; fail closed.
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "frontend assets unavailable"})
		}
	}
	info, statErr := fs.Stat(sub, "index.html")
	shellMissing := statErr != nil || info.IsDir()
	fileServer := http.FileServer(http.FS(sub))

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if shellMissing {
			c.String(http.StatusServiceUnavailable, "frontend assets are not built; run `make build`")
			return
		}

		name := strings.TrimPrefix(c.Request.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		f, err := sub.Open(name)
		if err != nil {
			// SPA fallback: unknown paths render the app shell.
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		if closeErr := f.Close(); closeErr != nil {
			c.Error(closeErr)
		}

		if name == "index.html" {
			c.Header("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}
