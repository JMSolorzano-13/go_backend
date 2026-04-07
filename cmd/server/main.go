package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	azblobinfra "github.com/siigofiscal/go_backend/internal/infra/azblob"
	azqueues "github.com/siigofiscal/go_backend/internal/infra/azservicebus"
	cognitoinfra "github.com/siigofiscal/go_backend/internal/infra/cognito"
	"github.com/siigofiscal/go_backend/internal/infra/jwks"
	s3infra "github.com/siigofiscal/go_backend/internal/infra/s3"
	"github.com/siigofiscal/go_backend/internal/infra/selfauth"
	sqsinfra "github.com/siigofiscal/go_backend/internal/infra/sqs"
	"github.com/siigofiscal/go_backend/internal/logger"
	"github.com/siigofiscal/go_backend/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	logger.Init(cfg.LogLevel)
	slog.Warn("go_backend: started",
		"db_host", cfg.DBHost,
		"local_infra", cfg.LocalInfra,
		"region", cfg.RegionName,
		"log_level", cfg.LogLevel,
	)

	database, err := db.New(cfg)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	slog.Warn("database connected", "host", cfg.DBHost, "db", cfg.DBName)

	if os.Getenv("RUN_MIGRATIONS") == "1" {
		sqlDB := database.Primary.DB
		if err := db.RunMigrations(context.Background(), sqlDB); err != nil {
			slog.Error("migrations failed", "error", err)
			os.Exit(1)
		}
		slog.Warn("migrations: completed")
	}

	var files port.FileStorage
	var msgPub port.MessagePublisher

	switch cfg.CloudProvider {
	case "azure":
		if cfg.AzureStorageConnectionString == "" {
			fmt.Fprintf(os.Stderr, "config: AZURE_STORAGE_CONNECTION_STRING required when CLOUD_PROVIDER=azure\n")
			os.Exit(1)
		}
		blobc, err := azblobinfra.NewFromConnectionString(cfg.AzureStorageConnectionString)
		if err != nil {
			slog.Error("azblob: client init failed", "error", err)
			os.Exit(1)
		}
		files = blobc
		qp, err := azqueues.NewQueuePublisher(cfg.AzureStorageConnectionString)
		if err != nil {
			slog.Error("azure queue publisher init failed", "error", err)
			os.Exit(1)
		}
		msgPub = qp
	default:
		s3c, err := s3infra.NewClient(cfg)
		if err != nil {
			slog.Error("s3: client init failed", "error", err)
			os.Exit(1)
		}
		files = s3infra.FileStorageAdapter{Client: s3c}
		sqsc, err := sqsinfra.NewClient(
			cfg.RegionName,
			cfg.AWSEndpointURL,
			cfg.AWSAccessKeyID,
			cfg.AWSSecretAccessKey,
		)
		if err != nil {
			slog.Error("sqs: client init failed", "error", err)
			os.Exit(1)
		}
		msgPub = sqsinfra.Publisher{Client: sqsc}
	}

	bus := event.NewBus(cfg.LocalInfra)
	sqsinfra.SubscribeAllHandlers(bus, cfg, msgPub)
	slog.Warn("event_bus: initialized", "local_infra", cfg.LocalInfra)

	slog.Warn("object_storage", "cloud_provider", cfg.CloudProvider, "certs_bucket", cfg.S3Certs)

	var idp port.IdentityProvider
	var jwtDecoder *auth.JWTDecoder

	switch cfg.CloudProvider {
	case "azure":
		sa := selfauth.New(selfauth.Config{
			DB:         database.Primary,
			SigningKey: cfg.SelfAuthSigningKey,
			Issuer:     "solucioncp-selfauth",
			Audience:   "solucioncp",
		})
		idp = sa
		jwtDecoder = auth.NewJWTDecoderSelfAuth(cfg, sa.SigningKey(), sa.Issuer(), sa.Audience())
		slog.Warn("identity_provider: selfauth (azure)", "issuer", sa.Issuer())
	default:
		cognitoClient, err := cognitoinfra.NewClient(cfg)
		if err != nil {
			slog.Error("cognito: client init failed", "error", err)
			os.Exit(1)
		}
		idp = cognitoinfra.NewAdapter(cognitoClient)
		jwtDecoder = auth.NewJWTDecoder(cfg, &jwks.HTTPFetcher{})
		slog.Warn("identity_provider: cognito", "pool_id", cfg.CognitoUserPoolID)
	}

	handler := server.New(cfg, database, bus, files, idp, jwtDecoder)

	srv := &http.Server{
		Addr:         ":8001",
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Warn("server listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("server stopped")
}
