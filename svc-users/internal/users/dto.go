package users

import (
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

// --- Auth DTOs ---

type RegisterUserRequest struct {
	FullName    string `json:"full_name"`
	PhoneNumber string `json:"phone_number"`
	NationalID  string `json:"national_id"`
	Email       string `json:"email"`
	Password    string `json:"password"`
}

type RegisterUserResponse struct {
	UserID    string    `json:"user_id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type VerifyEmailRequest struct {
	Token string `json:"token"`
}

type LoginRequest struct {
	PhoneNumber string `json:"phone_number"` // using phone number for login
	Password    string `json:"password"`
}

type LoginResponse struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// --- Profile DTOs ---

type GetUserResponse struct {
	UserID       string    `json:"user_id"`
	FullName     string    `json:"full_name"`
	NationalID   string    `json:"national_id"`
	Email        string    `json:"email"`
	PhoneNumbers []string  `json:"phone_numbers"`
	BirthDate    time.Time `json:"birth_date"`
	IsVerified   bool      `json:"is_verified"`
	WalletCount  int       `json:"wallet_count"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type UpdateUserRequest struct {
	FullName *string `json:"full_name,omitempty"`
	Email    *string `json:"email,omitempty"`
}

// --- Phone Management DTOs ---

type AddPhoneRequest struct {
	PhoneNumber string `json:"phone_number"`
}

// --- Wallet Proxy DTOs ---

type CreateWalletProxyRequest struct {
	PhoneNumber  string `json:"phone_number"`
	CurrencyCode string `json:"currency_code"`
}

type CreateWalletProxyResponse struct {
	WalletID  string    `json:"wallet_id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

type GetUserWalletsResponse struct {
	Wallets []WalletSummary `json:"wallets"`
	Count   int             `json:"count"`
}

type WalletSummary struct {
	WalletID     string `json:"wallet_id"`
	PhoneNumber  string `json:"phone_number"`
	Balance      int64  `json:"balance"`
	CurrencyCode string `json:"currency_code"`
}

type DeleteWalletRequest struct {
	WalletID string `json:"wallet_id"`
}

type GetWalletBalanceProxyResponse struct {
	WalletID  string    `json:"wallet_id"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}

// --- Validation Functions ---

func (r *RegisterUserRequest) Validate() error {
	r.FullName = strings.TrimSpace(r.FullName)
	if len(r.FullName) < 3 {
		return fmt.Errorf("full name is required and must be at least 3 characters")
	}
	r.Email = strings.TrimSpace(r.Email)
	addr, err := mail.ParseAddress(r.Email)
	if err != nil || addr.Address != r.Email {
		return fmt.Errorf("invalid email format")
	}
	if len(r.Password) < 8 {
		return ErrInvalidPassword
	}
	r.PhoneNumber = strings.TrimSpace(r.PhoneNumber)
	if err := ValidatePhoneNumber(&r.PhoneNumber); err != nil {
		return err
	}
	if err := ValidateNationalID(r.NationalID); err != nil {
		return err
	}
	return nil
}

func ValidatePhoneNumber(phoneNumber *string) error {
	*phoneNumber = strings.TrimSpace(*phoneNumber)
	if !(strings.HasPrefix(*phoneNumber, "010") || strings.HasPrefix(*phoneNumber, "011") || strings.HasPrefix(*phoneNumber, "012") || strings.HasPrefix(*phoneNumber, "015")) || len(*phoneNumber) != 11 {
		return ErrInvalidPhoneNumber
	}
	for _, ch := range *phoneNumber {
		if ch < '0' || ch > '9' {
			return ErrInvalidPhoneNumber
		}
	}
	return nil
}

func ValidateNationalID(nationalID string) error {
	if len(nationalID) != 14 {
		return ErrInvalidNationalID
	}
	for _, ch := range nationalID {
		if ch < '0' || ch > '9' {
			return ErrInvalidNationalID
		}
	}
	if nationalID[0] != '2' && nationalID[0] != '3' {
		return ErrInvalidNationalID
	}
	month, err := strconv.Atoi(nationalID[3:5])
	if err != nil {
		return err
	}
	if month <= 0 || month > 12 {
		return ErrInvalidNationalID
	}
	day, err := strconv.Atoi(nationalID[5:7])
	if err != nil {
		return err
	}
	if day <= 0 || day > 31 {
		return ErrInvalidNationalID
	}
	return nil
}
