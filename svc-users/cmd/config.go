package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerPort         string
	MongoURI           string
	MongoDBName        string
	RedisAddr          string
	WalletGrpcAddr     string
	TokenSymmetricKey  string
	AccessTokenDur     time.Duration
	RefreshTokenDur    time.Duration
	OtlpEndpoint       string

	SMTPHost    string
	SMTPPort    int
	SMTPUser    string
	SMTPPass    string
	SenderEmail string
}

func LoadConfig() (*Config, error) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI environment variable is required")
	}

	walletGrpcAddr := os.Getenv("WALLET_GRPC_URL")
	if walletGrpcAddr == "" {
		return nil, fmt.Errorf("WALLET_GRPC_URL environment variable is required")
	}

	tokenKey := os.Getenv("JWT_SECRET")
	if len(tokenKey) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters long")
	}

	accessDur, err := time.ParseDuration(getEnvOrDefault("JWT_ACCESS_EXPIRY", "15m"))
	if err != nil {
		accessDur = 15 * time.Minute
	}

	refreshDur, err := time.ParseDuration(getEnvOrDefault("JWT_REFRESH_EXPIRY", "168h"))
	if err != nil {
		refreshDur = 7 * 24 * time.Hour
	}

	smtpPort, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if smtpPort == 0 {
		smtpPort = 587
	}

	return &Config{
		ServerPort:        getEnvOrDefault("SERVER_PORT", "8080"),
		MongoURI:          mongoURI,
		MongoDBName:       getEnvOrDefault("MONGO_DB_NAME", "users_db"),
		RedisAddr:         getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
		WalletGrpcAddr:    walletGrpcAddr,
		TokenSymmetricKey: tokenKey,
		AccessTokenDur:    accessDur,
		RefreshTokenDur:   refreshDur,
		OtlpEndpoint:      os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		SMTPHost:          os.Getenv("SMTP_HOST"),
		SMTPPort:          smtpPort,
		SMTPUser:          os.Getenv("SMTP_USER"),
		SMTPPass:          os.Getenv("SMTP_PASS"),
		SenderEmail:       getEnvOrDefault("SENDER_EMAIL", "noreply@antigravity.wallet"),
	}, nil
}

func getEnvOrDefault(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
