package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"sorter/internal/auth"
	"sorter/internal/config"
	"sorter/internal/extractor"
	"sorter/internal/jobs"
	"sorter/internal/store"
)

// Server wires together all application components.
type Server struct {
	cfg   *config.Config
	store *store.Store
	auth  *auth.Manager
	queue *jobs.Queue
}

// New initializes storage, auth, the extraction queue and the HTTP router.
func New(cfg *config.Config) (*Server, error) {
	if err := os.MkdirAll(cfg.Storage.UploadDir, 0o755); err != nil {
		return nil, err
	}

	st, err := store.Open(cfg.Storage.DBPath)
	if err != nil {
		return nil, err
	}

	am, err := auth.NewManager(cfg.Auth.TokenHash, cfg.Auth.JWTTTL)
	if err != nil {
		st.Close()
		return nil, err
	}

	svc := extractor.NewService(cfg.LLM)
	queue := jobs.NewQueue(st, svc, cfg.Extractor.Workers)

	// Re-enqueue invoices that were left in "processing" by an unclean stop.
	if ids, err := st.ProcessingIDs(); err == nil {
		for _, id := range ids {
			queue.Enqueue(id)
		}
	}

	return &Server{cfg: cfg, store: st, auth: am, queue: queue}, nil
}

// Run starts the HTTP server and blocks until SIGINT/SIGTERM.
func (s *Server) Run() error {
	router := s.router()

	httpServer := &http.Server{
		Addr:              s.cfg.Server.Addr,
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("sorter listening on %s", s.cfg.Server.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	s.queue.Stop()
	return s.store.Close()
}

func (s *Server) router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	api.POST("/login", s.handleLogin)

	authed := api.Group("")
	authed.Use(s.authMiddleware())
	{
		authed.POST("/invoices", s.handleUpload)
		authed.GET("/invoices", s.handleList)
		authed.GET("/invoices/:id", s.handleGet)
		authed.PUT("/invoices/:id", s.handleUpdate)
		authed.DELETE("/invoices/:id", s.handleDelete)
		authed.POST("/invoices/:id/re-extract", s.handleReExtract)
		authed.GET("/invoices/:id/file", s.handleFile)
		authed.GET("/stats", s.handleStats)
		authed.GET("/stats/summary", s.handleSummary)
	}

	// Everything else is the embedded SPA.
	r.NoRoute(s.spaHandler())
	return r
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		if err := s.auth.Validate(header[len(prefix):]); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		c.Next()
	}
}
