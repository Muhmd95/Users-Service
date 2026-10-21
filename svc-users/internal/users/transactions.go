package users

import (
	"context"
	"fmt"
	"slices"
	"time"
)

type TransactionRequest struct {
	PhoneNumber string `json:"phone_number"`
	Amount      int64  `json:"amount"`
}

type TransferRequest struct {
	SenderPhone   string `json:"sender_phone"`
	ReceiverPhone string `json:"receiver_phone"`
	Amount        int64  `json:"amount"`
}

type TransactionResponse struct {
	TransactionID string    `json:"transaction_id"`
	WalletID      string    `json:"wallet_id"`
	Status        string    `json:"status"`
	Balance       int64     `json:"balance"`
	CreatedAt     time.Time `json:"created_at"`
}

type TransferResponse struct {
	TransactionID  string `json:"transaction_id"`
	SenderWalletID string `json:"sender_wallet_id"`
	Status         string `json:"status"`
	BalanceAfter   int64  `json:"balance_after"`
	BalanceBefore  int64  `json:"balance_before"`
}

type TransactionsClient interface {
	Deposit(context.Context, *TransactionRequest, string) (*TransactionResponse, error)
	Withdraw(context.Context, *TransactionRequest, string) (*TransactionResponse, error)
	Transfer(context.Context, *TransferRequest, string) (*TransferResponse, error)
}

// The deadline includes ownership lookup as well as the transaction RPC.
const transactionTimeout = 10 * time.Second

func (s *Service) authorizeTransaction(ctx context.Context, userID, phone string) error {
	if userID == "" {
		return ErrInvalidToken
	}
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.Status == "DELETED" {
		return ErrUserDeleted
	}
	if user.Status != "ACTIVE" {
		return ErrUserSuspended
	}
	// The normalized phone must still be registered to this account, even if
	// Wallet retains an older ownership association.
	if !slices.Contains(user.PhoneNumbers, phone) {
		return ErrPhoneOwnership
	}
	wallets, err := s.walletClient.GetUserWallets(ctx, userID)
	if err != nil {
		return err // Fail closed: a lookup failure never authorizes a transaction.
	}
	for _, wallet := range wallets.Wallets {
		if wallet.PhoneNumber == phone {
			return nil
		}
	}
	return ErrWalletOwnership
}

func validateTransaction(amount int64, phones ...*string) error {
	if amount <= 0 {
		return fmt.Errorf("%w: amount must be greater than zero", ErrInvalidInput)
	}
	for _, phone := range phones {
		if err := ValidatePhoneNumber(phone); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Deposit(ctx context.Context, userID string, req *TransactionRequest, key string) (*TransactionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, transactionTimeout)
	defer cancel()
	if err := validateTransaction(req.Amount, &req.PhoneNumber); err != nil {
		return nil, err
	}
	if err := s.authorizeTransaction(ctx, userID, req.PhoneNumber); err != nil {
		return nil, err
	}
	return s.transactionsClient.Deposit(ctx, req, key)
}

func (s *Service) Withdraw(ctx context.Context, userID string, req *TransactionRequest, key string) (*TransactionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, transactionTimeout)
	defer cancel()
	if err := validateTransaction(req.Amount, &req.PhoneNumber); err != nil {
		return nil, err
	}
	if err := s.authorizeTransaction(ctx, userID, req.PhoneNumber); err != nil {
		return nil, err
	}
	return s.transactionsClient.Withdraw(ctx, req, key)
}

func (s *Service) Transfer(ctx context.Context, userID string, req *TransferRequest, key string) (*TransferResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, transactionTimeout)
	defer cancel()
	if err := validateTransaction(req.Amount, &req.SenderPhone, &req.ReceiverPhone); err != nil {
		return nil, err
	}
	if req.SenderPhone == req.ReceiverPhone {
		return nil, fmt.Errorf("%w: sender and receiver must differ", ErrInvalidInput)
	}
	if err := s.authorizeTransaction(ctx, userID, req.SenderPhone); err != nil {
		return nil, err
	}
	return s.transactionsClient.Transfer(ctx, req, key)
}
