package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	pb "github.com/Muhmd95/Contracts/transactions/v1"
	"go.opentelemetry.io/otel"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"svc-users/internal/users"
)

func decodeTransaction(w http.ResponseWriter, r *http.Request, dst any) (string, bool) {
	defer r.Body.Close()
	key := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(key) == "" {
		respondError(w, http.StatusBadRequest, "Missing Idempotency-Key header")
		return "", false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request payload")
		return "", false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		respondError(w, http.StatusBadRequest, "Invalid request payload")
		return "", false
	}
	return key, true
}

// Deposit proxies a deposit into an authenticated user's wallet.
// @Summary Deposit into my wallet
// @Tags Transactions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "Reuse only for the identical request"
// @Param request body users.TransactionRequest true "Deposit"
// @Success 201 {object} users.TransactionResponse
// @Failure 400,401,403,404,409,429,500,503,504 {object} map[string]string
// @Router /v1/users/me/transactions/deposit [post]
func (c *Controller) Deposit(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "Deposit")
	defer span.End()
	var req users.TransactionRequest
	key, ok := decodeTransaction(w, r, &req)
	if !ok { return }
	resp, err := c.svc.Deposit(ctx, getUserID(r), &req, key)
	if err != nil { handleTransactionError(w, err); return }
	respondJSON(w, http.StatusCreated, resp)
}

// Withdraw proxies a withdrawal from an authenticated user's wallet.
// @Summary Withdraw from my wallet
// @Tags Transactions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "Reuse only for the identical request"
// @Param request body users.TransactionRequest true "Withdrawal"
// @Success 201 {object} users.TransactionResponse
// @Failure 400,401,403,404,409,429,500,503,504 {object} map[string]string
// @Router /v1/users/me/transactions/withdraw [post]
func (c *Controller) Withdraw(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "Withdraw")
	defer span.End()
	var req users.TransactionRequest
	key, ok := decodeTransaction(w, r, &req)
	if !ok { return }
	resp, err := c.svc.Withdraw(ctx, getUserID(r), &req, key)
	if err != nil { handleTransactionError(w, err); return }
	respondJSON(w, http.StatusCreated, resp)
}

// Transfer proxies a transfer from an authenticated user's wallet.
// @Summary Transfer from my wallet
// @Tags Transactions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "Reuse only for the identical request"
// @Param request body users.TransferRequest true "Transfer"
// @Success 201 {object} users.TransferResponse
// @Failure 400,401,403,404,409,422,429,500,503,504 {object} map[string]string
// @Router /v1/users/me/transactions/transfer [post]
func (c *Controller) Transfer(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("users-api").Start(r.Context(), "Transfer")
	defer span.End()
	var req users.TransferRequest
	key, ok := decodeTransaction(w, r, &req)
	if !ok { return }
	resp, err := c.svc.Transfer(ctx, getUserID(r), &req, key)
	if err != nil { handleTransactionError(w, err); return }
	respondJSON(w, http.StatusCreated, resp)
}

func handleTransactionError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		respondError(w, http.StatusGatewayTimeout, "request timed out; retry with the same Idempotency-Key")
		return
	}
	if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
		respondError(w, http.StatusRequestTimeout, "request canceled")
		return
	}
	st, ok := status.FromError(err)
	if !ok { handleError(w, err); return }
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.Domain != "transactions" { continue }
		code := http.StatusInternalServerError
		switch info.Reason {
		case pb.ErrorReason_INVALID_REQUEST.String(), pb.ErrorReason_INSUFFICIENT_BALANCE.String(),
			pb.ErrorReason_EXCEEDS_MAX_BALANCE.String():
			code = http.StatusBadRequest
		case pb.ErrorReason_WALLET_NOT_FOUND.String():
			code = http.StatusNotFound
		case pb.ErrorReason_IDEMPOTENCY_CONFLICT.String():
			code = http.StatusConflict
		case pb.ErrorReason_TRANSFER_REJECTED.String():
			code = http.StatusUnprocessableEntity
		case pb.ErrorReason_HIGH_FREQUENCY.String():
			code = http.StatusTooManyRequests
		}
		if code != http.StatusInternalServerError {
			respondError(w, code, st.Message())
			return
		}
	}
	if st.Code() == codes.Unavailable {
		respondError(w, http.StatusServiceUnavailable, "service unavailable; retry with the same Idempotency-Key")
		return
	}
	respondError(w, http.StatusInternalServerError, "internal server error")
}
