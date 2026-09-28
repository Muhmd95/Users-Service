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
	RefreshToken string `json:"-"`
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
	WalletIDs    []string  `json:"wallet_ids"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type UpdateUserRequest struct {
	FullName *string `json:"full_name,omitempty"`
	Email    *string `json:"email,omitempty"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// --- Phone Management DTOs ---

type AddPhoneRequest struct {
	PhoneNumber string `json:"phone_number"`
}

// --- Wallet Proxy DTOs ---

type CreateWalletProxyRequest struct {
	UserID       string `json:"user_id"`
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
	WalletID     string    `json:"wallet_id"`
	PhoneNumber  string    `json:"phone_number"`
	Balance      int64     `json:"balance"`
	CurrencyCode string    `json:"currency_code"`
	CreatedAt    time.Time `json:"created_at"`
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
		return fmt.Errorf("%w: full name is required and must be at least 3 characters", ErrInvalidInput)
	}
	r.Email = strings.TrimSpace(r.Email)
	addr, err := mail.ParseAddress(r.Email)
	if err != nil || addr.Address != r.Email {
		return fmt.Errorf("%w: invalid email format", ErrInvalidInput)
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

func (r *VerifyEmailRequest) Validate() error {
	r.Token = strings.TrimSpace(r.Token)
	if r.Token == "" {
		return fmt.Errorf("%w: token is required", ErrInvalidInput)
	}
	return nil
}

func (r *LoginRequest) Validate() error {
	r.PhoneNumber = strings.TrimSpace(r.PhoneNumber)
	if err := ValidatePhoneNumber(&r.PhoneNumber); err != nil {
		return err
	}
	if len(r.Password) == 0 {
		return fmt.Errorf("%w: password is required", ErrInvalidInput)
	}
	return nil
}

func (r *RefreshTokenRequest) Validate() error {
	r.RefreshToken = strings.TrimSpace(r.RefreshToken)
	if r.RefreshToken == "" {
		return fmt.Errorf("%w: refresh token is required", ErrInvalidInput)
	}
	return nil
}

func (r *UpdateUserRequest) Validate() error {
	if r.FullName != nil {
		*r.FullName = strings.TrimSpace(*r.FullName)
		if len(*r.FullName) < 3 {
			return fmt.Errorf("%w: full name must be at least 3 characters", ErrInvalidInput)
		}
	}
	if r.Email != nil {
		*r.Email = strings.TrimSpace(*r.Email)
		addr, err := mail.ParseAddress(*r.Email)
		if err != nil || addr.Address != *r.Email {
			return fmt.Errorf("%w: invalid email format", ErrInvalidInput)
		}
	}
	return nil
}

func (r *ChangePasswordRequest) Validate() error {
	if len(r.OldPassword) == 0 {
		return fmt.Errorf("%w: old password is required", ErrInvalidInput)
	}
	if len(r.NewPassword) < 8 {
		return ErrInvalidPassword
	}
	return nil
}

func (r *AddPhoneRequest) Validate() error {
	r.PhoneNumber = strings.TrimSpace(r.PhoneNumber)
	return ValidatePhoneNumber(&r.PhoneNumber)
}

func (r *CreateWalletProxyRequest) Validate() error {
	r.PhoneNumber = strings.TrimSpace(r.PhoneNumber)
	if err := ValidatePhoneNumber(&r.PhoneNumber); err != nil {
		return err
	}
	r.CurrencyCode = strings.TrimSpace(r.CurrencyCode)
	r.CurrencyCode = strings.ToUpper(r.CurrencyCode)
	if r.CurrencyCode != "EGP" {
		return fmt.Errorf("%w: unsupported currency code: only EGP is supported", ErrInvalidInput)
	}
	return nil
}

func (r *DeleteWalletRequest) Validate() error {
	r.WalletID = strings.TrimSpace(r.WalletID)
	if r.WalletID == "" {
		return fmt.Errorf("%w: wallet id is required", ErrInvalidInput)
	}
	return nil
}
