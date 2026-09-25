package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheng-alvin/esplanade/server/config"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"github.com/cheng-alvin/esplanade/server/logging"
	"github.com/cheng-alvin/esplanade/server/middleware"
	"github.com/cheng-alvin/esplanade/server/repository"
	"github.com/cheng-alvin/esplanade/server/router"
	"go.uber.org/zap"
)

func main() {
	cfg, configErr := config.Load()
	logger, loggerErr := logging.New(cfg.LogLevel, cfg.Env)

	if loggerErr != nil || configErr != nil {
		fmt.Fprintf(os.Stderr, "Initialization error")
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("starting esplanade server",
		zap.String("env", cfg.Env),
		zap.String("addr", cfg.Addr()),
	)

	connectCtx, connectCancel := context.WithTimeout(
		context.Background(),
		cfg.MongoConnectTimeout+cfg.MongoServerSelectionTimeout,
	)
	mongoClient, err := esmongo.New(connectCtx, cfg)
	connectCancel()
	if err != nil {
		logger.Fatal("connecting to mongo", zap.Error(err))
	}
	logger.Info("connected to mongo", zap.String("database", cfg.MongoDatabase))

	mongoDB := esmongo.Database(mongoClient, cfg.MongoDatabase)

	indexCtx, indexCancel := context.WithTimeout(context.Background(), repository.DefaultOperationTimeout)
	if err = repository.EnsureIndexes(indexCtx, mongoDB); err != nil {
		logger.Fatal("ensuring mongo indexes", zap.Error(err))
	}
	indexCancel()

	mux := router.New(mongoClient)
	handler := middleware.Chain(
		mux,
		middleware.RequestID,
		middleware.Logging(logger),
		middleware.Recovery(logger),
		middleware.CORS(cfg.CORSOrigins),
	)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Exit logic:

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	case err := <-errCh:
		logger.Fatal("server error", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	logger.Info("shutting down server", zap.Duration("timeout", cfg.ShutdownTimeout))
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("server forced to shutdown", zap.Error(err))
	}

	if err := esmongo.Disconnect(ctx, mongoClient); err != nil {
		logger.Error("disconnecting mongo client", zap.Error(err))
	}

	logger.Info("server stopped")
}
