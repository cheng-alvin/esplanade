package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheng-alvin/esplanade/server/auth/jwt"
	"github.com/cheng-alvin/esplanade/server/auth/providers/google"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/config"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"github.com/cheng-alvin/esplanade/server/devices"
	"github.com/cheng-alvin/esplanade/server/email"
	"github.com/cheng-alvin/esplanade/server/logging"
	"github.com/cheng-alvin/esplanade/server/middleware"
	"github.com/cheng-alvin/esplanade/server/ratelimit"
	"github.com/cheng-alvin/esplanade/server/router"
	"go.mongodb.org/mongo-driver/v2/mongo"
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

	indexCtx, indexCancel := context.WithTimeout(context.Background(), 5*time.Second)
	indexes := []struct {
		collection string
		models     []mongo.IndexModel
	}{
		{devices.DeviceCollection, devices.Device{}.Indexes()},
		{authrepo.UserCollection, authrepo.User{}.Indexes()},
		{authrepo.SessionCollection, authrepo.Session{}.Indexes()},
		{authrepo.PasswordResetCollection, authrepo.PasswordResetToken{}.Indexes()},
	}
	for _, index := range indexes {
		if _, err := mongoDB.Collection(index.collection).Indexes().CreateMany(indexCtx, index.models); err != nil {
			logger.Fatal("ensuring mongo indexes", zap.String("collection", index.collection), zap.Error(err))
		}
	}
	indexCancel()

	if cfg.JWTSecret == "" {
		logger.Fatal("JWT secret not configured", zap.String("env_var", "ESPLANADE_JWT_SECRET"))
	}
	if cfg.GoogleClientID == "" {
		logger.Warn("google client id not configured; google sign-in will be unavailable")
	}

	userRepo := authrepo.NewUserRepository(esmongo.Collection(mongoDB, authrepo.UserCollection))
	sessionRepo := authrepo.NewSessionRepository(esmongo.Collection(mongoDB, authrepo.SessionCollection))
	resetRepo := authrepo.NewPasswordResetRepository(esmongo.Collection(mongoDB, authrepo.PasswordResetCollection))
	deviceStore := devices.NewStore(esmongo.Collection(mongoDB, devices.DeviceCollection))

	authService := service.New(service.Deps{
		Config:      cfg,
		Logger:      logger,
		Users:       userRepo,
		Sessions:    sessionRepo,
		ResetTokens: resetRepo,
		Devices:     deviceStore,
		Emails:      email.NewSender(cfg, logger),
		Google:      google.New(cfg.GoogleClientID),
		Issuer:      jwt.NewIssuer(cfg.JWTSecret, cfg.AccessTokenTTL),
		Limiter:     ratelimit.New(cfg.ResendLimit, cfg.ResendWindow),
	})

	mux := router.New(mongoClient, router.AuthDeps{
		Service:  authService,
		Verifier: jwt.NewVerifier(cfg.JWTSecret),
		Users:    userRepo,
	})
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
