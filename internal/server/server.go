package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"sorter/internal/app"
	"sorter/internal/auth"
	"sorter/internal/config"
	"sorter/internal/extractor"
	"sorter/internal/jobs"
	"sorter/internal/store"
)

type Server struct {
	cfg        *config.Config
	store      *store.Store
	auth       *auth.Manager
	queue      *jobs.Queue
	app        *app.Service
	loginLimit loginLimiter
}

// loginLimiter is a bounded global token bucket for the single-admin deployment.
type loginLimiter struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func (l *loginLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.last.IsZero() {
		l.tokens = 20
	} else {
		l.tokens = min(20, l.tokens+now.Sub(l.last).Seconds()/3)
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
func New(cfg *config.Config) (*Server, error) {
	files, err := app.NewLocalFiles(cfg.Storage.UploadDir)
	if err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := st.RecoverPending(ctx); err != nil {
		st.Close()
		return nil, err
	}
	paths, err := st.FilePaths(ctx)
	if err != nil {
		st.Close()
		return nil, err
	}
	if err := files.ReportOrphans(paths); err != nil {
		st.Close()
		return nil, err
	}
	queue := jobs.NewQueue(st, extractor.NewService(cfg.LLM), cfg.Extractor.Workers)
	return &Server{cfg: cfg, store: st, auth: am, queue: queue, app: app.New(st, files, queue)}, nil
}
func (s *Server) Run() error {
	httpServer := &http.Server{Addr: s.cfg.Server.Addr, Handler: s.router(), ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 1)
	go func() {
		log.Printf("sorter listening on %s", s.cfg.Server.Addr)
		err := httpServer.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errorsCh <- err
		}
	}()
	var runErr error
	select {
	case runErr = <-errorsCh:
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Cancel workers immediately; unfinished records remain durable for restart.
	workerStop := make(chan error, 1)
	go func() { workerStop <- s.queue.Stop(shutdownCtx) }()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
		httpServer.Close()
	}
	workerErr := <-workerStop
	return errors.Join(runErr, workerErr, s.store.Close())
}
func (s *Server) router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.MaxMultipartMemory = 8 << 20
	r.SetTrustedProxies(nil)
	r.Use(gin.Logger(), gin.Recovery(), func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	})
	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if err := s.store.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	api.POST("/login", s.handleLogin)
	authed := api.Group("")
	authed.Use(s.authMiddleware())
	authed.POST("/invoices", s.handleUpload)
	authed.GET("/invoices", s.handleList)
	authed.GET("/invoices/:id", s.handleGet)
	authed.PUT("/invoices/:id", s.handleUpdate)
	authed.DELETE("/invoices/:id", s.handleDelete)
	authed.POST("/invoices/:id/re-extract", s.handleReExtract)
	authed.GET("/invoices/:id/file", s.handleFile)
	authed.GET("/stats", s.handleStats)
	authed.GET("/stats/summary", s.handleSummary)
	r.NoRoute(s.spaHandler())
	return r
}
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix || s.auth.Validate(header[len(prefix):]) != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		c.Next()
	}
}
