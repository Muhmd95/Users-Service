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

// CreateWallet proxies wallet creation to the Wallet Service.
// @Summary      Create Wallet
// @Description  Creates a wallet for the user via Wallet Service.
// @Tags         Wallet
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      users.CreateWalletProxyRequest true "Create Wallet Payload"
// @Success      201      {object}  users.CreateWalletProxyResponse "Wallet created"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me/wallets [post]
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

// GetUserWallets retrieves all wallets for the authenticated user.
// @Summary      Get My Wallets
// @Description  Retrieves a list of wallets owned by the user.
// @Tags         Wallet
// @Produce      json
// @Security     BearerAuth
// @Success      200      {object}  users.GetUserWalletsResponse "List of wallets"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me/wallets [get]
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

// GetWalletBalance retrieves the balance of a specific wallet.
// @Summary      Get Wallet Balance
// @Description  Retrieves the balance for a wallet owned by the user.
// @Tags         Wallet
// @Produce      json
// @Security     BearerAuth
// @Param        wallet_id path      string                     true "Wallet ID"
// @Success      200      {object}  users.GetWalletBalanceProxyResponse "Wallet balance"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      403      {object}  map[string]string          "Forbidden (Not Owner)"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me/wallets/{wallet_id}/balance [get]
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

// DeleteWallet proxies wallet deletion to the Wallet Service.
// @Summary      Delete Wallet
// @Description  Deletes a specific wallet owned by the user.
// @Tags         Wallet
// @Produce      json
// @Security     BearerAuth
// @Param        wallet_id path      string                     true "Wallet ID"
// @Success      204      "No Content"
// @Failure      400      {object}  map[string]string          "Bad Request"
// @Failure      401      {object}  map[string]string          "Unauthorized"
// @Failure      403      {object}  map[string]string          "Forbidden (Not Owner)"
// @Failure      500      {object}  map[string]string          "Internal Server Error"
// @Router       /v1/users/me/wallets/{wallet_id} [delete]
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
