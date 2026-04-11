// Command worker consumes Azure Service Bus SAT pipeline queues and runs the Go SAT handlers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	azblobinfra "github.com/siigofiscal/go_backend/internal/infra/azblob"
	"github.com/siigofiscal/go_backend/internal/infra/azsbconsumer"
	"github.com/siigofiscal/go_backend/internal/infra/azsbpub"
	sqsinfra "github.com/siigofiscal/go_backend/internal/infra/sqs"
	"github.com/siigofiscal/go_backend/internal/logger"
	"github.com/siigofiscal/go_backend/internal/worker/handlers"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	logger.Init(cfg.LogLevel)

	if strings.ToLower(strings.TrimSpace(cfg.CloudProvider)) != "azure" {
		fmt.Fprintf(os.Stderr, "go-worker: CLOUD_PROVIDER must be azure (got %q)\n", cfg.CloudProvider)
		os.Exit(1)
	}

	listenCS := strings.TrimSpace(os.Getenv("AZURE_SERVICEBUS_LISTEN_CONNECTION_STRING"))
	if listenCS == "" {
		listenCS = cfg.AzureServiceBusConnectionString
	}
	if listenCS == "" {
		fmt.Fprintf(os.Stderr, "go-worker: set AZURE_SERVICEBUS_LISTEN_CONNECTION_STRING or AZURE_SERVICEBUS_CONNECTION_STRING (Listen)\n")
		os.Exit(1)
	}

	if cfg.AzureStorageConnectionString == "" {
		fmt.Fprintf(os.Stderr, "go-worker: AZURE_STORAGE_CONNECTION_STRING is required\n")
		os.Exit(1)
	}

	slog.Warn("go-worker: starting",
		"db_host", cfg.DBHost,
		"local_infra", cfg.LocalInfra,
		"log_level", cfg.LogLevel,
	)

	database, err := db.New(cfg)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if os.Getenv("RUN_MIGRATIONS") == "1" {
		sqlDB := database.Primary.DB
		if err := db.RunMigrations(context.Background(), sqlDB); err != nil {
			slog.Error("migrations failed", "error", err)
			os.Exit(1)
		}
		slog.Warn("migrations: completed")
	}

	blobc, err := azblobinfra.NewFromConnectionString(cfg.AzureStorageConnectionString)
	if err != nil {
		slog.Error("azblob: client init failed", "error", err)
		os.Exit(1)
	}
	var files port.FileStorage = blobc

	var msgPub port.MessagePublisher
	hybridLocalSAT := cfg.LocalInfra && cfg.AWSEndpointURL != ""
	if hybridLocalSAT {
		sqsc, err := sqsinfra.NewClient(
			cfg.RegionName,
			cfg.AWSEndpointURL,
			cfg.AWSAccessKeyID,
			cfg.AWSSecretAccessKey,
		)
		if err != nil {
			slog.Error("sqs: hybrid local SAT publisher init failed", "error", err)
			os.Exit(1)
		}
		msgPub = sqsinfra.Publisher{Client: sqsc}
		slog.Warn("go-worker: hybrid_local_sat blob=azurite sqs=localstack")
	} else if cfg.AzureServiceBusConnectionString != "" {
		sb, err := azsbpub.NewPublisher(cfg.AzureServiceBusConnectionString)
		if err != nil {
			slog.Error("azure service bus publisher init failed", "error", err)
			os.Exit(1)
		}
		defer func() { _ = sb.Close(context.Background()) }()
		msgPub = sb
		slog.Warn("go-worker: message_publisher: azure service bus")
	} else {
		fmt.Fprintf(os.Stderr, "go-worker: set AZURE_SERVICEBUS_CONNECTION_STRING for event publishing (or hybrid LocalStack SQS)\n")
		os.Exit(1)
	}

	bus := event.NewBus(cfg.LocalInfra)
	sqsinfra.SubscribeAllHandlers(bus, cfg, msgPub)
	slog.Warn("go-worker: event_bus initialized", "local_infra", cfg.LocalInfra)

	deps := handlers.Deps{
		DB:      database,
		Bus:     bus,
		Storage: files,
		Cfg:     cfg,
	}

	consumer, err := azsbconsumer.New(listenCS, azsbconsumer.Options{
		MaxMessagesPerReceive: 1,
	})
	if err != nil {
		slog.Error("azsbconsumer: init failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = consumer.Close(context.Background()) }()

	type route struct {
		queueCfg string
		name     string
		fn       func(context.Context, json.RawMessage) error
	}
	routes := []route{
		{cfg.SQSSendQueryMetadata, "send_query_metadata", (&handlers.SendQueryMetadata{Deps: deps}).Handle},
		{cfg.SQSCreateQuery, "create_query", (&handlers.CreateQuery{Deps: deps}).Handle},
		{cfg.SQSVerifyQuery, "verify_query", (&handlers.VerifyQuery{Deps: deps}).Handle},
		{cfg.SQSDownloadQuery, "download_query", (&handlers.DownloadQuery{Deps: deps}).Handle},
		{cfg.SQSProcessPackageMetadata, "process_metadata", (&handlers.ProcessMetadata{Deps: deps}).Handle},
		{cfg.SQSProcessPackageXML, "process_xml", (&handlers.ProcessXML{Deps: deps}).Handle},
		{cfg.SQSCompleteCFDIs, "complete_cfdis", (&handlers.CompleteCFDIs{Deps: deps}).Handle},
	}

	for _, r := range routes {
		qn := azsbpub.QueueNameFromSQSURL(r.queueCfg)
		if qn == "" {
			slog.Error("go-worker: empty queue name from config", "route", r.name, "raw", r.queueCfg)
			os.Exit(1)
		}
		routeName := r.name
		handle := r.fn
		h := func(ctx context.Context, msg azsbconsumer.Incoming) azsbconsumer.HandleResult {
			return dispatchJSON(ctx, routeName, msg, handle)
		}
		if err := consumer.RegisterQueue(qn, h); err != nil {
			slog.Error("go-worker: register queue", "queue", qn, "route", r.name, "error", err)
			os.Exit(1)
		}
		slog.Warn("go-worker: registered queue", "queue", qn, "handler", r.name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit
		slog.Info("go-worker: shutdown signal", "signal", sig.String())
		cancel()
	}()

	slog.Warn("go-worker: consuming (Service Bus)")
	if err := consumer.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("go-worker: consumer stopped", "error", err)
		os.Exit(1)
	}
	slog.Info("go-worker: stopped")
}

func dispatchJSON(parent context.Context, route string, msg azsbconsumer.Incoming, fn func(context.Context, json.RawMessage) error) azsbconsumer.HandleResult {
	logArgs := []any{"route", route, "queue", msg.QueueName, "message_id", msg.MessageID}
	body := strings.TrimSpace(string(msg.Body))
	if body == "" {
		slog.Warn("go-worker: empty message body", logArgs...)
		return azsbconsumer.HandleResult{
			Outcome:               azsbconsumer.AckDeadLetter,
			DeadLetterReason:      "EmptyBody",
			DeadLetterDescription: "message body is empty",
		}
	}

	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()

	raw := json.RawMessage(msg.Body)
	if err := fn(ctx, raw); err != nil {
		slog.Error("go-worker: handler failed", append(logArgs, "error", err)...)
		return azsbconsumer.HandleResult{Outcome: azsbconsumer.AckAbandon, Err: err}
	}
	return azsbconsumer.HandleResult{Outcome: azsbconsumer.AckComplete}
}
