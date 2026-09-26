package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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
	// 1. Initialize logger
	logger.InitLogger("svc-users")
	logger.Log.Info().Msg("Starting svc-users ...")

	// 2. Load .ENV before parsing config
	if err := godotenv.Load(".ENV"); err != nil {
		logger.Log.Info().Msg("No .ENV file found, relying on OS environment variables")
	}

	// 3. Load Configurations
	cfg, err := LoadConfig()
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to load configurations")
	}

	// 4. Initialize Tracer
	tp, err := tracer.InitTracer("svc-users", cfg.OtlpEndpoint)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to initialize tracer")
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			logger.Log.Error().Err(err).Msg("Failed to shutdown tracer")
		}
	}()

	// 5. Connect MongoDB
	mongoClient, err := mongodb.ConnectMongoDB(cfg.MongoURI)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to connect to MongoDB")
	}
	defer func() {
		if err := mongoClient.Disconnect(context.Background()); err != nil {
			logger.Log.Error().Err(err).Msg("Failed to disconnect MongoDB")
		}
	}()
	database := mongoClient.Database(cfg.MongoDBName)
	logger.Log.Info().Str("dbName", cfg.MongoDBName).Msg("Using database")

	usersRepo, err := mongodb.NewUsersRepository(context.Background(), database)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to create users repository")
	}

	// 6. Connect Redis
	rdb, err := redisclient.NewClient(cfg.RedisAddr)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to connect to Redis")
	}

	// 7. Token Maker
	tokenMaker, err := token.NewJWTMaker(cfg.TokenSymmetricKey)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to create token maker")
	}

	// 8. Wallet gRPC Client
	walletClient, err := wallet.NewClient(cfg.WalletGrpcAddr)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to create wallet client")
	}

	// 9. Email Worker (Background Job)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	jobsChan := make(chan users.EmailJob, 100)
	emailWorker := jobs.NewEmailWorker(
		jobsChan, 
		rdb, 
		cfg.SMTPHost, 
		cfg.SMTPPort, 
		cfg.SMTPUser, 
		cfg.SMTPPass, 
		cfg.SenderEmail,
	)
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		emailWorker.Start(ctx)
	}()

	// 10. Service & Controller
	service := users.NewService(
		usersRepo,
		walletClient,
		jobsChan,
		tokenMaker,
		cfg.AccessTokenDur,
		cfg.RefreshTokenDur,
		rdb,
	)

	controller := rest.NewController(service)

	// 11. Router
	mux := rest.NewRouter(controller, rdb, tokenMaker)
	handler := rest.RequestLogger(rest.MetricsMiddleware(mux))

	// 12. HTTP Server
	server := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: handler,
	}

	go func() {
		logger.Log.Info().Str("port", cfg.ServerPort).Msg("Users HTTP server is listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal().Err(err).Msg("Server crashed")
		}
	}()

	// 13. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit
	logger.Log.Info().Msg("Shutting down svc-users gracefully...")

	// Shutdown HTTP Server (Stops accepting new requests)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error().Err(err).Msg("Server forced to shutdown")
	} else {
		logger.Log.Info().Msg("HTTP Server shut down")
	}

	// Close the Wallet gRPC connection
	if err := walletClient.Close(); err != nil {
		logger.Log.Error().Err(err).Msg("Failed to close wallet gRPC client")
	} else {
		logger.Log.Info().Msg("Wallet gRPC client closed")
	}

	// Gracefully shutdown the email worker by closing its channel
	// Note: We close the channel so it drains pending jobs and exits safely.
	logger.Log.Info().Msg("Waiting for background workers to finish...")
	close(jobsChan)

	// We use a separate goroutine to wait so we can enforce a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Log.Info().Msg("Background workers finished successfully")
	case <-time.After(5 * time.Second):
		logger.Log.Warn().Msg("Timeout waiting for background workers. Forcing shutdown.")
		cancel() // Force cancel context for email worker if it's stuck
	}

	logger.Log.Info().Msg("svc-users exited safely")
}
