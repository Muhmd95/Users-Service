package rest

import (
	"encoding/json"
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

// helper to extract user id from context
func getUserID(r *http.Request) string {
	uid, _ := r.Context().Value(UserIDContextKey).(string)
	return uid
}

// ==========================================
// AUTH HANDLERS
// ==========================================

func (c *Controller) Register(w http.ResponseWriter, r *http.Request) {
	var req users.RegisterUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	resp, err := c.svc.RegisterUser(r.Context(), &req)
	if err != nil {
		logger.Ctx(r.Context()).Warn().Err(err).Msg("Registration failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, resp)
}

func (c *Controller) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "token query parameter is required")
		return
	}

	req := &users.VerifyEmailRequest{Token: token}
	if err := c.svc.VerifyEmail(r.Context(), req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

func (c *Controller) Login(w http.ResponseWriter, r *http.Request) {
	var req users.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	resp, err := c.svc.Login(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req users.RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	resp, err := c.svc.RefreshToken(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) Logout(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if err := c.svc.Logout(r.Context(), userID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to logout")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *Controller) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	userID := getUserID(r)
	if err := c.svc.ChangePassword(r.Context(), userID, req.OldPassword, req.NewPassword); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "password changed successfully, all sessions logged out"})
}

// ==========================================
// PROFILE HANDLERS
// ==========================================

func (c *Controller) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	resp, err := c.svc.GetUser(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) UpdateMe(w http.ResponseWriter, r *http.Request) {
	var req users.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	userID := getUserID(r)
	resp, err := c.svc.UpdateUser(r.Context(), userID, &req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) DeleteMe(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if err := c.svc.SoftDeleteUser(r.Context(), userID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ==========================================
// PHONE HANDLERS
// ==========================================

func (c *Controller) AddPhone(w http.ResponseWriter, r *http.Request) {
	var req users.AddPhoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	userID := getUserID(r)
	if err := c.svc.AddPhoneNumber(r.Context(), userID, req.PhoneNumber); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "phone number added"})
}

func (c *Controller) RemovePhone(w http.ResponseWriter, r *http.Request) {
	// The path will be /v1/users/me/phones/{phone_number}
	// Extract phone number from URL path
	phone := r.PathValue("phone")
	if phone == "" {
		respondError(w, http.StatusBadRequest, "phone number required in path")
		return
	}

	userID := getUserID(r)
	if err := c.svc.RemovePhoneNumber(r.Context(), userID, phone); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "phone number removed"})
}
