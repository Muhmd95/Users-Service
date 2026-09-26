package rest

import (
	"encoding/json"
	"go.opentelemetry.io/otel"
	"net/http"

	"svc-users/internal/users"
)

// ==========================================
// WALLET HANDLERS
// ==========================================

func (c *Controller) CreateWallet(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "CreateWallet")
	defer span.End()
	r = r.WithContext(ctx)
	var req users.CreateWalletProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	req.UserID = getUserID(r)
	resp, err := c.svc.CreateWallet(r.Context(), &req)
	if err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, resp)
}

func (c *Controller) GetUserWallets(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "GetUserWallets")
	defer span.End()
	r = r.WithContext(ctx)
	userID := getUserID(r)
	resp, err := c.svc.GetUserWallets(r.Context(), userID)
	if err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) GetWalletBalance(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "GetWalletBalance")
	defer span.End()
	r = r.WithContext(ctx)
	walletID := r.PathValue("wallet_id")
	if walletID == "" {
		respondError(w, http.StatusBadRequest, "wallet_id required in path")
		return
	}

	userID := getUserID(r) 
	resp, err := c.svc.GetWalletBalance(r.Context(), userID, walletID)
	if err != nil {
		handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) DeleteWallet(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "DeleteWallet")
	defer span.End()
	r = r.WithContext(ctx)
	walletID := r.PathValue("wallet_id")
	if walletID == "" {
		respondError(w, http.StatusBadRequest, "wallet_id required in path")
		return
	}

	userID := getUserID(r)
	req := &users.DeleteWalletRequest{WalletID: walletID}

	if err := c.svc.DeleteWallet(r.Context(), userID, req); err != nil {
		handleError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
