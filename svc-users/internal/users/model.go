package users

import (
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	NationalID   string             `bson:"national_id"`
	FullName     string             `bson:"full_name"`
	BirthDate    time.Time          `bson:"birth_date"`
	Email        string             `bson:"email"`
	PasswordHash string             `bson:"password_hash"`
	PhoneNumbers []string           `bson:"phone_numbers"` // multikey unique index
	IsVerified   bool               `bson:"is_verified"`
	Status       string             `bson:"status"` // ACTIVE | SUSPENDED | DELETED
	WalletIDs    []string           `bson:"wallet_ids"`
	CreatedAt    time.Time          `bson:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

type RefreshToken struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	UserID    primitive.ObjectID `bson:"user_id"`
	TokenHash string             `bson:"token_hash"`
	Revoked   bool               `bson:"revoked"`
	ExpiresAt time.Time          `bson:"expires_at"` // TTL index
	CreatedAt time.Time          `bson:"created_at"`
}

const MaxWalletsPerUser = 3
const MaxPhonesPerUser = 5

var (
	ErrInvalidInput             = errors.New("invalid input")
	ErrInvalidVerificationToken = errors.New("invalid or expired verification token")
	ErrUserNotFound             = errors.New("user not found")
	ErrDuplicatePhone           = errors.New("phone number is already registered")
	ErrDuplicateNationalID      = errors.New("national ID is already registered")
	ErrDuplicateEmail           = errors.New("email is already registered")
	ErrInvalidCredentials       = errors.New("invalid credentials")
	ErrUserSuspended            = errors.New("user account is suspended")
	ErrUserDeleted              = errors.New("user account is deleted")
	ErrInvalidPhoneNumber       = errors.New("invalid phone number format")
	ErrInvalidNationalID        = errors.New("invalid national id format")
	ErrInvalidUserID            = errors.New("invalid user id format")
	ErrInvalidPassword          = errors.New("password must be at least 8 characters")
	ErrEmailNotVerified         = errors.New("email not verified")
	ErrMaxWalletsReached        = errors.New("maximum number of wallets reached")
	ErrMaxPhonesReached         = errors.New("maximum number of phone numbers reached")
	ErrInvalidToken             = errors.New("invalid or expired token")
	ErrTokenExpired             = errors.New("token has expired")
	ErrTokenRevoked             = errors.New("refresh token has been revoked")
	ErrPhoneHasWallet           = errors.New("cannot remove phone number associated with a wallet")
	ErrWalletOwnership          = errors.New("forbidden: you do not own this wallet")
	ErrPhoneOwnership           = errors.New("forbidden: phone number is not registered to your account")
)
