package users

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type transactionOwnerRepository struct {
	Repository
	user          User
	queriedUserID string
}

func (r *transactionOwnerRepository) GetUserByID(_ context.Context, userID string) (*User, error) {
	r.queriedUserID = userID
	return &r.user, nil
}

type transactionOwnerWallets struct {
	WalletClient
	wallets       []WalletSummary
	calls         int
	queriedUserID string
}

func (w *transactionOwnerWallets) GetUserWallets(_ context.Context, userID string) (*GetUserWalletsResponse, error) {
	w.calls++
	w.queriedUserID = userID
	return &GetUserWalletsResponse{Wallets: w.wallets}, nil
}

type transactionRecorder struct {
	calls                int
	phone, receiver, key string
}

func (r *transactionRecorder) Deposit(_ context.Context, req *TransactionRequest, key string) (*TransactionResponse, error) {
	r.calls++
	r.phone, r.key = req.PhoneNumber, key
	return &TransactionResponse{}, nil
}

func (r *transactionRecorder) Withdraw(ctx context.Context, req *TransactionRequest, key string) (*TransactionResponse, error) {
	return r.Deposit(ctx, req, key)
}

func (r *transactionRecorder) Transfer(_ context.Context, req *TransferRequest, key string) (*TransferResponse, error) {
	r.calls++
	r.phone, r.receiver, r.key = req.SenderPhone, req.ReceiverPhone, key
	return &TransferResponse{}, nil
}

func TestTransactionPhoneOwnership(t *testing.T) {
	const primary, secondary, outsider, receiver = "01012345678", "01112345678", "01212345678", "01512345678"
	for _, operation := range []string{"deposit", "withdraw", "transfer"} {
		for _, tt := range []struct {
			name, phone     string
			phones          []string
			wallet          bool
			wantErr         error
			wantWalletCalls int
		}{
			{"primary", primary, []string{primary, secondary}, true, nil, 1},
			{"secondary", secondary, []string{primary, secondary}, true, nil, 1},
			{"normalized", "  " + secondary + "  ", []string{primary, secondary}, true, nil, 1},
			{"unregistered despite wallet ownership", outsider, []string{primary, secondary}, true, ErrPhoneOwnership, 0},
			{"empty phone list", primary, nil, true, ErrPhoneOwnership, 0},
			{"registered without wallet ownership", primary, []string{primary}, false, ErrWalletOwnership, 1},
		} {
			t.Run(operation+"/"+tt.name, func(t *testing.T) {
				repo := &transactionOwnerRepository{user: User{Status: "ACTIVE", PhoneNumbers: tt.phones}}
				wallets := &transactionOwnerWallets{}
				if tt.wallet {
					wallets.wallets = []WalletSummary{{PhoneNumber: strings.TrimSpace(tt.phone)}}
				}
				client := &transactionRecorder{}
				svc := &Service{repo: repo, walletClient: wallets, transactionsClient: client}
				var err error
				switch operation {
				case "deposit":
					_, err = svc.Deposit(context.Background(), "authenticated-user", &TransactionRequest{PhoneNumber: tt.phone, Amount: 100}, "same-key")
				case "withdraw":
					_, err = svc.Withdraw(context.Background(), "authenticated-user", &TransactionRequest{PhoneNumber: tt.phone, Amount: 100}, "same-key")
				case "transfer":
					// The receiver is deliberately absent from this user's registered phones.
					_, err = svc.Transfer(context.Background(), "authenticated-user", &TransferRequest{SenderPhone: tt.phone, ReceiverPhone: receiver, Amount: 100}, "same-key")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if repo.queriedUserID != "authenticated-user" {
					t.Fatal("lookup did not use authenticated identity")
				}
				if wallets.calls != tt.wantWalletCalls {
					t.Fatalf("wallet calls = %d, want %d", wallets.calls, tt.wantWalletCalls)
				}
				if wallets.calls > 0 && wallets.queriedUserID != "authenticated-user" {
					t.Fatal("wallet lookup used wrong identity")
				}
				if tt.wantErr != nil {
					if client.calls != 0 {
						t.Fatal("rejected request reached Transactions")
					}
					return
				}
				if client.calls != 1 || client.phone != strings.TrimSpace(tt.phone) || client.key != "same-key" {
					t.Fatalf("unexpected forwarded request: %+v", client)
				}
				if operation == "transfer" && client.receiver != receiver {
					t.Fatal("receiver was changed")
				}
			})
		}
	}
}
