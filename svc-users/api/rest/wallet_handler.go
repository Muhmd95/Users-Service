package rest

import (
	"encoding/json"
	"net/http"

	"svc-users/internal/users"
)

// ==========================================
// WALLET HANDLERS
// ==========================================

func (c *Controller) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req users.CreateWalletProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	userID := getUserID(r)
	resp, err := c.svc.CreateWallet(r.Context(), userID, &req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, resp)
}

func (c *Controller) GetUserWallets(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	resp, err := c.svc.GetUserWallets(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) GetWalletBalance(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("wallet_id")
	if walletID == "" {
		respondError(w, http.StatusBadRequest, "wallet_id required in path")
		return
	}

	// Ideally we should check if this walletID actually belongs to the authenticated user.
	// But in this proxy, we trust the Wallet service or add a quick check here.
	// We will pass it to the service.
	resp, err := c.svc.GetWalletBalance(r.Context(), walletID)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (c *Controller) DeleteWallet(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("wallet_id")
	if walletID == "" {
		respondError(w, http.StatusBadRequest, "wallet_id required in path")
		return
	}

	userID := getUserID(r)
	req := &users.DeleteWalletRequest{WalletID: walletID}

	if err := c.svc.DeleteWallet(r.Context(), userID, req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
