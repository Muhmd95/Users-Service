package rest

import (
	"encoding/json"
	"errors"
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
	if errors.Is(err, users.ErrInvalidInput) || errors.Is(err, users.ErrInvalidVerificationToken) {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
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
	case users.ErrWalletOwnership, users.ErrPhoneOwnership:
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
func (c *Controller) setRefreshTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})
}

// helper to clear refresh token cookie
func (c *Controller) clearRefreshTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// ==========================================
// AUTH HANDLERS
// ==========================================

// Register handles the creation of a new user.
// @Summary      Register a new user
// @Description  Creates a new user account.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        request  body      users.RegisterUserRequest  true  "Register Payload"
// @Success      201      {object}  users.RegisterUserResponse "User created successfully"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/register [post]
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

// VerifyEmail handles email verification.
// @Summary      Verify Email
// @Description  Verifies a user's email using a token. Missing, invalid, expired, or orphaned verification tokens return 400; database or Redis failures return 500.
// @Tags         Auth
// @Produce      json
// @Param        token    query     string                     true  "Verification Token"
// @Success      200      {object}  map[string]string          "Email verified successfully"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/verify-email [get]
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

// Login handles user authentication.
// @Summary      Login user
// @Description  Authenticates a verified user with phone number and password. Returns a JWT with token_type=access in JSON; the token_type=refresh JWT is returned only in the refresh_token cookie, not in the JSON body.
// @Description  The cookie uses HttpOnly, SameSite=Strict and Path=/; Secure is false only when APP_ENV=development. Cookie Max-Age is 30 days, independently of the configured refresh JWT lifetime. expires_in is the access-token lifetime in seconds.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        request  body      users.LoginRequest         true  "Login Payload"
// @Success      200      {object}  users.LoginResponse        "Login successful"
// @Header       200      {string}  Set-Cookie                 "refresh_token cookie; Secure except in development"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/login [post]
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

	c.setRefreshTokenCookie(w, resp.RefreshToken)
	respondJSON(w, http.StatusOK, resp)
}

// RefreshToken generates a new access token using a refresh token.
// @Summary      Refresh Access Token
// @Description  Requires a signed, unexpired JWT with token_type=refresh and a matching non-revoked database record. Access tokens and older tokens without token_type are rejected.
// @Description  Reads refresh_token from the HttpOnly cookie or optional JSON body; a nonempty cookie takes precedence. Returns a new token_type=access JWT without rotating the refresh token or replacing its cookie. expires_in is measured in seconds.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        request  body      users.RefreshTokenRequest  false "Refresh Token Payload (or via HttpOnly Cookie)"
// @Success      200      {object}  users.RefreshTokenResponse "Token refreshed"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/refresh [post]
func (c *Controller) RefreshToken(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "RefreshToken")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.RefreshTokenRequest

	// Decode JSON if present, ignore errors as we might only have cookie
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Check cookie and override
	// if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
	// 	req.RefreshToken = cookie.Value
	// }

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

// Logout invalidates the user's session.
// @Summary      Logout user
// @Description  Requires a token_type=access bearer token. Revokes all stored refresh tokens for the authenticated user and clears the refresh_token cookie. Already-issued access tokens remain valid until expiry.
// @Tags         Auth
// @Produce      json
// @Security     BearerAuth
// @Success      204      "No Content"
// @Header       204      {string}  Set-Cookie                 "Clears refresh_token with the same Secure policy used at login"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/logout [post]
func (c *Controller) Logout(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "Logout")
	defer span.End()
	r = r.WithContext(ctx)
	userID := getUserID(r)
	if err := c.svc.Logout(r.Context(), userID); err != nil {
		handleError(w, err)
		return
	}
	c.clearRefreshTokenCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// ChangePassword updates the user's password.
// @Summary      Change Password
// @Description  Requires a token_type=access bearer token and the old password. Saves the new password hash, attempts to revoke all existing refresh tokens, and issues a new access token in JSON and a refresh_token cookie.
// @Description  The cookie is HttpOnly and SameSite=Strict; Secure is false only in development. Already-issued access tokens remain valid until expiry. Refresh-token revocation failures are currently logged without failing this operation.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      users.ChangePasswordRequest true "Change Password Payload"
// @Success      200      {object}  users.LoginResponse        "Password changed successfully"
// @Header       200      {string}  Set-Cookie                 "Replacement refresh_token cookie; Secure except in development"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me/password [put]
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

	c.setRefreshTokenCookie(w, resp.RefreshToken)
	respondJSON(w, http.StatusOK, resp)
}

// ==========================================
// PROFILE HANDLERS
// ==========================================

// GetMe retrieves the authenticated user's profile.
// @Summary      Get My Profile
// @Description  Retrieves the profile of the currently authenticated user.
// @Tags         Profile
// @Produce      json
// @Security     BearerAuth
// @Success      200      {object}  users.GetUserResponse      "User profile"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me [get]
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

// UpdateMe updates the authenticated user's profile.
// @Summary      Update My Profile
// @Description  Updates the profile details of the currently authenticated user. Changing the email sets is_verified=false in the same database update. An unchanged email or a name-only update preserves verification. No new verification email is sent by this endpoint yet.
// @Description  Invalid names and email addresses return 400.
// @Tags         Profile
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      users.UpdateUserRequest    true "Update Profile Payload"
// @Success      200      {object}  users.GetUserResponse      "Profile updated"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me [put]
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

// DeleteMe logically deletes the authenticated user's account.
// @Summary      Delete My Account
// @Description  Performs a soft delete of the user's account.
// @Tags         Profile
// @Produce      json
// @Security     BearerAuth
// @Success      204      "No Content"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me [delete]
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

// AddPhone adds a new phone number to the user's profile.
// @Summary      Add Phone Number
// @Description  Adds a secondary phone number to the user.
// @Tags         Phone
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      users.AddPhoneRequest      true "Add Phone Payload"
// @Success      200      {object}  map[string]string          "Phone number added"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me/phones [post]
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

// RemovePhone removes a phone number from the user's profile.
// @Summary      Remove Phone Number
// @Description  Removes a secondary phone number from the user.
// @Tags         Phone
// @Produce      json
// @Security     BearerAuth
// @Param        phone    path      string                     true "Phone Number"
// @Success      200      {object}  map[string]string          "Phone number removed"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me/phones/{phone} [delete]
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
