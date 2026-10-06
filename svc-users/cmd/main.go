package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	jobs "svc-users/Jobs"
	"svc-users/api/rest"
	"svc-users/client/wallet"
	"svc-users/external/mongodb"
	redisclient "svc-users/external/redis"
	"svc-users/internal/users"
	"svc-users/util/logger"
	"svc-users/util/token"
	"svc-users/util/tracer"
)

func main() {
	logger.InitLogger("svc-users")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		logger.Log.Error().Err(err).Msg("svc-users stopped with errors")
		os.Exit(1)
	}
	logger.Log.Info().Msg("svc-users stopped")
}

// Returning from run executes dependency cleanup on both shutdown and startup errors.
func run(ctx context.Context) error {
	logger.Log.Info().Msg("Starting svc-users ...")
	if err := godotenv.Load(".ENV"); err != nil {
		logger.Log.Info().Msg("No .ENV file found, relying on OS environment variables")
	}

	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	tp, err := tracer.InitTracer("svc-users", cfg.OtlpEndpoint)
	if err != nil {
		return fmt.Errorf("initialize tracer: %w", err)
	}
	defer closeWithTimeout("tracer", tp.Shutdown)

	mongoClient, err := mongodb.ConnectMongoDB(cfg.MongoURI)
	if err != nil {
		return fmt.Errorf("connect MongoDB: %w", err)
	}
	defer closeWithTimeout("MongoDB", mongoClient.Disconnect)
	database := mongoClient.Database(cfg.MongoDBName)
	logger.Log.Info().Str("dbName", cfg.MongoDBName).Msg("Using database")

	usersRepo, err := mongodb.NewUsersRepository(ctx, database)
	if err != nil {
		return fmt.Errorf("create users repository: %w", err)
	}

	rdb, err := redisclient.NewClient(cfg.RedisAddr)
	if err != nil {
		return fmt.Errorf("connect Redis: %w", err)
	}
	defer func() {
		if err := rdb.Close(); err != nil {
			logger.Log.Error().Err(err).Msg("Failed to close Redis")
		}
	}()

	tokenMaker, err := token.NewJWTMaker(cfg.TokenSymmetricKey)
	if err != nil {
		return fmt.Errorf("create token maker: %w", err)
	}

	walletClient, err := wallet.NewClient(cfg.WalletGrpcAddr)
	if err != nil {
		return fmt.Errorf("create wallet client: %w", err)
	}
	defer func() {
		if err := walletClient.Close(); err != nil {
			logger.Log.Error().Err(err).Msg("Failed to close Wallet gRPC client")
		}
	}()

	jobsChan := make(chan users.EmailJob, 100)
	emailWorker := jobs.NewEmailWorker(
		jobsChan, rdb, cfg.SMTPHost, cfg.SMTPPort,
		cfg.SMTPUser, cfg.SMTPPass, cfg.SenderEmail,
	)
	service := users.NewService(
		usersRepo, walletClient, jobsChan, tokenMaker,
		cfg.AccessTokenDur, cfg.RefreshTokenDur, rdb,
	)
	controller := rest.NewController(service, cfg.SecureCookies)
	mux := rest.NewRouter(controller, rdb, tokenMaker)
	server := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: rest.RequestLogger(rest.MetricsMiddleware(mux)),
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP requests: %w", err)
	}
	logger.Log.Info().Str("port", cfg.ServerPort).Msg("Users HTTP server is listening")
	return serve(ctx, server, listener, jobsChan, emailWorker.Start, defaultShutdownTimeouts)
}
