package rest

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "svc-users/docs" // Import generated swagger docs
	"svc-users/external/redis"
	"svc-users/util/token"
)

// @title           Users Service API
// @version         1.0
// @description     This is the REST API for the Users Service.
// @host            localhost:8001
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter Bearer <access_token>. Requires a signed, unexpired JWT with token_type=access; refresh tokens and older tokens without token_type are rejected.
func NewRouter(c *Controller, rdb *redis.Client, tokenMaker token.Maker) *http.ServeMux {
	mux := http.NewServeMux()

	// Swagger UI
	mux.HandleFunc("/v1/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/v1/swagger/doc.json"),
	))

	// 1. Core middlewares (Logger + Metrics)
	// We'll wrap the final mux with these when returning

	// 2. Middlewares
	authMid := AuthMiddleware(tokenMaker)
	rateLimiter := RateLimiterMiddleware(rdb, 60, 1*time.Minute)       // 60 req/min general limit
	strictRateLimiter := RateLimiterMiddleware(rdb, 10, 1*time.Minute) // 10 req/min for auth

	// --- Unprotected Routes (Auth) ---

	// We wrap unprotected routes with the strict rate limiter
	mux.Handle("POST /v1/users/register", strictRateLimiter(http.HandlerFunc(c.Register)))
	mux.Handle("GET /v1/users/verify-email", strictRateLimiter(http.HandlerFunc(c.VerifyEmail)))
	mux.Handle("POST /v1/users/login", strictRateLimiter(http.HandlerFunc(c.Login)))
	mux.Handle("POST /v1/users/refresh", strictRateLimiter(http.HandlerFunc(c.RefreshToken)))

	// --- Protected Routes ---

	// We chain Auth -> Rate Limiter -> Handler
	protected := func(h http.HandlerFunc) http.Handler {
		return authMid(rateLimiter(h))
	}

	// Profile
	mux.Handle("GET /v1/users/me", protected(c.GetMe))
	mux.Handle("PUT /v1/users/me", protected(c.UpdateMe))
	mux.Handle("DELETE /v1/users/me", protected(c.DeleteMe))

	// Security
	mux.Handle("POST /v1/users/logout", protected(c.Logout))
	mux.Handle("PUT /v1/users/me/password", protected(c.ChangePassword))

	// Phones
	mux.Handle("POST /v1/users/me/phones", protected(c.AddPhone)) // need better optimization
	mux.Handle("DELETE /v1/users/me/phones/{phone}", protected(c.RemovePhone)) // needs better optimization

	// Wallets
	mux.Handle("POST /v1/users/me/wallets", protected(c.CreateWallet))
	mux.Handle("GET /v1/users/me/wallets", protected(c.GetUserWallets))
	mux.Handle("GET /v1/users/me/wallets/{wallet_id}/balance", protected(c.GetWalletBalance))
	mux.Handle("DELETE /v1/users/me/wallets/{wallet_id}", protected(c.DeleteWallet))

	mux.Handle("POST /v1/users/me/transactions/deposit", protected(c.Deposit))
	mux.Handle("POST /v1/users/me/transactions/withdraw", protected(c.Withdraw))
	mux.Handle("POST /v1/users/me/transactions/transfer", protected(c.Transfer))

	// Metrics endpoint (Prometheus)
	mux.Handle("/metrics", promhttp.Handler())

	return mux
}

// WrapWithCoreMiddleware wraps the main router with Observability middlewares
func WrapWithCoreMiddleware(handler http.Handler) http.Handler {
	return RequestLogger(MetricsMiddleware(handler))
}
