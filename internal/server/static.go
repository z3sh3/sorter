package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// dist holds the built frontend. It is populated by `make build`; the
// checked-in placeholder keeps `go build` working without a frontend build.
//
//go:embed all:dist
var dist embed.FS

// spaHandler serves the embedded single-page application with history-API
// fallback to index.html for unknown non-API paths.
func (s *Server) spaHandler() gin.HandlerFunc {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
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
		f.Close()

		if name == "index.html" {
			c.Header("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}
