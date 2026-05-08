package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yadrotask/internal/config"
	"yadrotask/internal/db"
	"yadrotask/internal/httpapi"
	"yadrotask/internal/logger"
	"yadrotask/internal/store"
)

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := connectWithRetry(ctx, cfg.DatabaseURL, cfg.StartupWait)
	if err != nil {
		log.Error("database connection failed", logger.Field{"error": err.Error()})
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.ApplySchema(ctx, pool); err != nil {
		log.Error("schema apply failed", logger.Field{"error": err.Error()})
		os.Exit(1)
	}

	st := store.New(pool)
	server := httpapi.New(cfg, st, log)

	httpServer := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("server started", logger.Field{"addr": cfg.Addr()})
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server stopped unexpectedly", logger.Field{"error": err.Error()})
			cancel()
		}
	}()

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
	}
}

func connectWithRetry(ctx context.Context, dsn string, timeout time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		pool, err := db.Connect(ctx, dsn, 5*time.Second)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}
