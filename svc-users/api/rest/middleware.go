package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"svc-users/external/redis"
	"svc-users/util/logger"
	"svc-users/util/metrics"
	"svc-users/util/token"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

const UserIDContextKey contextKey = "user_id"

// --- Response Writer Wrapper ---
// Captures status code and bytes written for logging and metrics.

type responseRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytesWritten += n
	return n, err
}

// --- Request Logger Middleware ---
// Wraps every request with structured logging including:
// user_id, method, path, status, duration, bytes_read, bytes_written

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Count bytes read from the request body
		bytesRead := int64(0)
		if r.Body != nil {
			r.Body = &countingReader{ReadCloser: r.Body, bytesRead: &bytesRead}
		}

		// Wrap the response writer to capture status and bytes written
		recorder := &responseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		duration := time.Since(start)
		log := logger.Ctx(r.Context()).With().Str("layer", "api").Logger()

		// Extract user_id from context if set by auth middleware
		userID := ""
		if uid, ok := r.Context().Value(UserIDContextKey).(string); ok {
			userID = uid
		}

		// Choose log level based on status code
		var event *zerolog.Event
		switch {
		case recorder.statusCode >= 500:
			event = log.Error()
		case recorder.statusCode >= 400:
			event = log.Warn()
		default:
			event = log.Info()
		}

		// Build the enriched log entry
		event.
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", recorder.statusCode).
			Dur("duration", duration).
			Int64("bytes_read", bytesRead).
			Int("bytes_written", recorder.bytesWritten).
			Str("remote_addr", r.RemoteAddr).
			Str("user_agent", r.UserAgent())

		if userID != "" {
			event.Str("user_id", userID)
		}

		event.Msg("request completed")
	})
}

// --- Metrics Middleware ---
// Records Prometheus metrics for every HTTP request.

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Count bytes read from the request body
		bytesRead := int64(0)
		if r.Body != nil {
			r.Body = &countingReader{ReadCloser: r.Body, bytesRead: &bytesRead}
		}

		recorder := &responseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(recorder.statusCode)

		metrics.HTTPRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
		metrics.HTTPRequestBytesRead.WithLabelValues(r.Method, r.URL.Path).Observe(float64(bytesRead))
		metrics.HTTPResponseBytesWritten.WithLabelValues(r.Method, r.URL.Path).Observe(float64(recorder.bytesWritten))
	})
}

// --- Counting Reader ---
// Wraps io.ReadCloser to count bytes consumed from request body.

type countingReader struct {
	io.ReadCloser
	bytesRead *int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.ReadCloser.Read(p)
	*cr.bytesRead += int64(n)
	return n, err
}

// --- Rate Limiter Middleware ---

func RateLimiterMiddleware(redisClient *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get key based on UserID if authenticated, otherwise IP address
			key := r.RemoteAddr
			if uid, ok := r.Context().Value(UserIDContextKey).(string); ok && uid != "" {
				key = "user:" + uid
			} else {
				// Strip port from IP
				if colonIdx := strings.LastIndex(key, ":"); colonIdx != -1 {
					key = key[:colonIdx]
				}
				key = "ip:" + key
			}

			// Add prefix
			key = "ratelimit:" + key

			allowed, err := redisClient.AllowSlidingWindow(r.Context(), key, limit, window)
			if err != nil {
				// If Redis fails, log it but let the request through to avoid cascading failure
				log := logger.Ctx(r.Context())
				log.Error().Err(err).Str("layer", "api").Msg("Rate limiter redis check failed")
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// --- Auth Middleware ---

func AuthMiddleware(tokenMaker token.Maker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "authorization header is required"})
				return
			}

			fields := strings.Fields(authHeader)
			if len(fields) < 2 || strings.ToLower(fields[0]) != "bearer" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid authorization header format"})
				return
			}

			accessToken := fields[1]
			payload, err := tokenMaker.VerifyToken(accessToken, token.AccessToken)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired access token"})
				return
			}

			ctx := context.WithValue(r.Context(), UserIDContextKey, payload.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// not currently used
// --- Ownership Middleware ---

// OwnershipMiddleware extracts the user ID from the URL path (e.g., /users/{userID}/...)
// and ensures it matches the authenticated user ID from the context.
// In our routing, we actually use "me" instead of an explicit user ID for most routes,
// but for cases where we pass a target user ID, we can enforce ownership.
// Note: Since all our protected routes use /v1/users/me/..., this middleware is optional
// as the handlers automatically use the user ID from the context.
// However, if we ever expose /v1/users/{user_id}, this middleware will be crucial.
func OwnershipMiddleware(targetUserIDParam func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authUserID, ok := r.Context().Value(UserIDContextKey).(string)
			if !ok || authUserID == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			targetUserID := targetUserIDParam(r)

			// If the target is "me", it conceptually matches the auth user.
			if targetUserID != "me" && targetUserID != authUserID {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: you can only access your own resources"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
