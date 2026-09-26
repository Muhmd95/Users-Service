package rest

import (
	"encoding/json"
	"go.opentelemetry.io/otel"
	"net/http"

	"svc-users/internal/users"
	"svc-users/util/logger"
)

// helper to respond with JSON
func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		json.NewEncoder(w).Encode(payload)
	}
}

// helper to respond with error
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// handleError maps known domain errors to safe HTTP responses and hides internal server errors
func handleError(w http.ResponseWriter, err error) {
	switch err {
	case users.ErrUserNotFound, users.ErrInvalidUserID:
		respondError(w, http.StatusNotFound, err.Error())
	case users.ErrDuplicatePhone, users.ErrDuplicateNationalID, users.ErrDuplicateEmail,
		users.ErrInvalidPhoneNumber, users.ErrInvalidNationalID, users.ErrInvalidPassword,
		users.ErrMaxWalletsReached, users.ErrMaxPhonesReached, users.ErrPhoneHasWallet:
		respondError(w, http.StatusBadRequest, err.Error())
	case users.ErrInvalidCredentials, users.ErrUserSuspended, users.ErrUserDeleted,
		users.ErrEmailNotVerified, users.ErrInvalidToken, users.ErrTokenExpired, users.ErrTokenRevoked:
		respondError(w, http.StatusUnauthorized, err.Error())
	case users.ErrWalletOwnership:
		respondError(w, http.StatusForbidden, err.Error())
	default:
		// Mask raw/internal errors
		logger.Log.Error().Err(err).Str("layer", "api").Msg("Internal server error exposed to API")
		respondError(w, http.StatusInternalServerError, "internal server error")
	}
}

// helper to extract user id from context
func getUserID(r *http.Request) string {
	uid, _ := r.Context().Value(UserIDContextKey).(string)
	return uid
}

// helper to set refresh token cookie
func setRefreshTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})
}

// helper to clear refresh token cookie
func clearRefreshTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// ==========================================
// AUTH HANDLERS
// ==========================================

func (c *Controller) Register(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "Register")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.RegisterUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	resp, err := c.svc.RegisterUser(r.Context(), &req)
	if err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, resp)
}

func (c *Controller) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "VerifyEmail")
	defer span.End()
	r = r.WithContext(ctx)
	token := r.URL.Query().Get("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "token query parameter is required")
		return
	}

	req := &users.VerifyEmailRequest{Token: token}
	if err := c.svc.VerifyEmail(r.Context(), req); err != nil {
		handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

func (c *Controller) Login(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "Login")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	resp, err := c.svc.Login(r.Context(), &req)
	if err != nil {
		handleError(w, err)
		return
	}

	setRefreshTokenCookie(w, resp.RefreshToken)
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) RefreshToken(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "RefreshToken")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.RefreshTokenRequest
	
	// Decode JSON if present, ignore errors as we might only have cookie
	_ = json.NewDecoder(r.Body).Decode(&req)
	
	// Check cookie and override
	if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
		req.RefreshToken = cookie.Value
	}

	if req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "refresh token is required")
		return
	}

	resp, err := c.svc.RefreshToken(r.Context(), &req)
	if err != nil {
		handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) Logout(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "Logout")
	defer span.End()
	r = r.WithContext(ctx)
	userID := getUserID(r)
	if err := c.svc.Logout(r.Context(), userID); err != nil {
		handleError(w, err)
		return
	}
	clearRefreshTokenCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (c *Controller) ChangePassword(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "ChangePassword")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	if err := req.Validate(); err != nil {
		handleError(w, err)
		return
	}

	userID := getUserID(r)
	resp, err := c.svc.ChangePassword(r.Context(), userID, req.OldPassword, req.NewPassword)
	if err != nil {
		handleError(w, err)
		return
	}

	setRefreshTokenCookie(w, resp.RefreshToken)
	respondJSON(w, http.StatusOK, resp)
}

// ==========================================
// PROFILE HANDLERS
// ==========================================

func (c *Controller) GetMe(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "GetMe")
	defer span.End()
	r = r.WithContext(ctx)
	userID := getUserID(r)
	resp, err := c.svc.GetUser(r.Context(), userID)
	if err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) UpdateMe(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "UpdateMe")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	userID := getUserID(r)
	resp, err := c.svc.UpdateUser(r.Context(), userID, &req)
	if err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) DeleteMe(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "DeleteMe")
	defer span.End()
	r = r.WithContext(ctx)
	userID := getUserID(r)
	if err := c.svc.SoftDeleteUser(r.Context(), userID); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ==========================================
// PHONE HANDLERS
// ==========================================

func (c *Controller) AddPhone(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "AddPhone")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.AddPhoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	userID := getUserID(r)
	if err := c.svc.AddPhoneNumber(r.Context(), userID, req.PhoneNumber); err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "phone number added"})
}

func (c *Controller) RemovePhone(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "RemovePhone")
	defer span.End()
	r = r.WithContext(ctx)
	// The path will be /v1/users/me/phones/{phone_number}
	// Extract phone number from URL path
	phone := r.PathValue("phone")
	if phone == "" {
		respondError(w, http.StatusBadRequest, "phone number required in path")
		return
	}

	userID := getUserID(r)
	if err := c.svc.RemovePhoneNumber(r.Context(), userID, phone); err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "phone number removed"})
}
