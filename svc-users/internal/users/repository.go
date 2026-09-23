package users

import (
	"context"
)

type Repository interface {
	// --- Users Collection ---
	CreateUser(ctx context.Context, user *User) error
	GetUserByID(ctx context.Context, userID string) (*User, error)
	GetUserByPhoneNumber(ctx context.Context, phoneNumber string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByNationalID(ctx context.Context, nationalID string) (*User, error)
	UpdateUser(ctx context.Context, userID string, updates map[string]interface{}) (*User, error)
	SoftDeleteUser(ctx context.Context, userID string) error
	ReactivateUser(ctx context.Context, userID string, updates map[string]interface{}) error
	AddPhoneNumber(ctx context.Context, userID string, phone string) error
	RemovePhoneNumber(ctx context.Context, userID string, phone string) error
	AddWalletID(ctx context.Context, userID string, walletID string) error
	RemoveWalletIDIfOwns(ctx context.Context, userID string, walletID string) error
	HasWallet(ctx context.Context, userID string, walletID string) (bool, error)

	// --- Refresh Tokens Collection ---
	StoreRefreshToken(ctx context.Context, token *RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	RevokeAllUserTokens(ctx context.Context, userID string) error
}
