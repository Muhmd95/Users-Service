package users

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"svc-users/util/logger"
	"svc-users/util/token"
)

// EmailJob defines the data needed to verify an email
type EmailJob struct {
	Email string
	Token string
}

// WalletClient defines the interface for communicating with the Wallet Service
type WalletClient interface {
	CreateWallet(ctx context.Context, req *CreateWalletProxyRequest) (*CreateWalletProxyResponse, error)
	DeleteWallet(ctx context.Context, req *DeleteWalletRequest) error
	DeleteUserWallets(ctx context.Context, userID string) error
	GetUserWallets(ctx context.Context, userID string) (*GetUserWalletsResponse, error)
	GetWalletBalance(ctx context.Context, walletID string) (*GetWalletBalanceProxyResponse, error)
}

type Service struct {
	repo                 Repository
	walletClient         WalletClient
	emailJobs            chan<- EmailJob
	tokenMaker           token.Maker
	accessTokenDuration  time.Duration
	refreshTokenDuration time.Duration
}

func NewService(repo Repository, walletClient WalletClient, emailJobs chan<- EmailJob, tokenMaker token.Maker, accessDuration, refreshDuration time.Duration) *Service {
	return &Service{
		repo:                 repo,
		walletClient:         walletClient,
		emailJobs:            emailJobs,
		tokenMaker:           tokenMaker,
		accessTokenDuration:  accessDuration,
		refreshTokenDuration: refreshDuration,
	}
}

// deriveBirthDate is a helper to extract the birth date from an Egyptian National ID
func deriveBirthDate(nationalID string) time.Time {
	centuryPrefix := nationalID[0]
	year, _ := strconv.Atoi(nationalID[1:3])
	month, _ := strconv.Atoi(nationalID[3:5])
	day, _ := strconv.Atoi(nationalID[5:7])

	fullYear := 1900 + year
	if centuryPrefix == '3' {
		fullYear = 2000 + year
	}
	return time.Date(fullYear, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// ==========================================
// AUTH METHODS
// ==========================================

func (s *Service) RegisterUser(ctx context.Context, req *RegisterUserRequest) (*RegisterUserResponse, error) {
	log := logger.Ctx(ctx)

	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Error().Err(err).Msg("failed to hash password")
		return nil, fmt.Errorf("failed to process password")
	}

	birthDate := deriveBirthDate(req.NationalID)

	user := &User{
		NationalID:   req.NationalID,
		FullName:     req.FullName,
		BirthDate:    birthDate,
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		PhoneNumbers: []string{req.PhoneNumber},
		IsVerified:   false,
		Status:       "ACTIVE",
		WalletIDs:    []string{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err = s.repo.CreateUser(ctx, user)
	if err != nil {
		// Handle re-registration logic here if needed (status=DELETED)
		// For simplicity, we just pass the repo error up
		return nil, err
	}

	// Generate a verification token (for simplicity, using a UUID or objectID string)
	token := primitive.NewObjectID().Hex()

	// Enqueue the email verification job
	if s.emailJobs != nil {
		select {
		case s.emailJobs <- EmailJob{Email: user.Email, Token: token}:
			log.Info().Str("email", user.Email).Msg("Email verification job queued")
		default:
			log.Warn().Str("email", user.Email).Msg("Email job queue is full, dropping job")
		}
	}

	return &RegisterUserResponse{
		UserID:    user.ID.Hex(),
		Message:   "User registered successfully. Please verify your email.",
		CreatedAt: user.CreatedAt,
	}, nil
}

func (s *Service) VerifyEmail(ctx context.Context, req *VerifyEmailRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	// In a real implementation, you'd validate the token against a database or cache.
	// For this skeleton, we assume the token IS the user ID or we just mark them verified.
	// We will skip full token mapping logic here and just assume we verified it successfully.
	return nil
}

func (s *Service) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	// Find user by phone number
	user, err := s.repo.GetUserByPhoneNumber(ctx, req.PhoneNumber)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if user.Status == "SUSPENDED" {
		return nil, ErrUserSuspended
	}
	if user.Status == "DELETED" {
		return nil, ErrUserDeleted
	}
	if !user.IsVerified {
		return nil, ErrEmailNotVerified
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Generate Access Token
	accessToken, _, err := s.tokenMaker.CreateToken(user.ID.Hex(), s.accessTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to create access token: %w", err)
	}

	// Generate Refresh Token
	refreshToken, refreshPayload, err := s.tokenMaker.CreateToken(user.ID.Hex(), s.refreshTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh token: %w", err)
	}

	// Store Refresh Token in DB
	rt := &RefreshToken{
		UserID:    user.ID,
		TokenHash: refreshToken, // ideally hash this, but we store the string for now or let repo handle it
		Revoked:   false,
		ExpiresAt: refreshPayload.ExpiredAt,
		CreatedAt: time.Now(),
	}
	if err := s.repo.StoreRefreshToken(ctx, rt); err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return &LoginResponse{
		UserID:       user.ID.Hex(),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.accessTokenDuration.Seconds()),
	}, nil
}

func (s *Service) RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	// Verify the provided refresh token
	refreshPayload, err := s.tokenMaker.VerifyToken(req.RefreshToken)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Check if it exists and is not revoked in the DB
	rt, err := s.repo.GetRefreshToken(ctx, req.RefreshToken)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if rt.Revoked {
		return nil, ErrTokenRevoked
	}
	if rt.UserID.Hex() != refreshPayload.UserID {
		return nil, ErrInvalidToken
	}

	// Issue new access token
	accessToken, accessPayload, err := s.tokenMaker.CreateToken(refreshPayload.UserID, s.accessTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to create access token: %w", err)
	}

	return &RefreshTokenResponse{
		AccessToken: accessToken,
		ExpiresIn:   int64(accessPayload.ExpiredAt.Sub(time.Now()).Seconds()),
	}, nil
}

func (s *Service) Logout(ctx context.Context, userID string) error {
	// For Phase 5 we will revoke the specific refresh token, but for now
	// we will just revoke all tokens for this user.
	return s.repo.RevokeAllUserTokens(ctx, userID)
}

func (s *Service) ChangePassword(ctx context.Context, userID string, oldPassword, newPassword string) error {
	log := logger.Ctx(ctx)

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword))
	if err != nil {
		return ErrInvalidCredentials
	}

	if len(newPassword) < 8 {
		return ErrInvalidPassword
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Error().Err(err).Msg("failed to hash password")
		return fmt.Errorf("failed to process password")
	}

	updates := map[string]interface{}{
		"password_hash": string(hashedPassword),
		"updated_at":    time.Now(),
	}

	_, err = s.repo.UpdateUser(ctx, userID, updates)
	if err != nil {
		return err
	}

	// Revoke ALL refresh tokens so all active sessions are logged out
	if err := s.repo.RevokeAllUserTokens(ctx, userID); err != nil {
		log.Warn().Err(err).Str("user_id", userID).Msg("Failed to revoke tokens on password change")
	}

	return nil
}

// ==========================================
// PROFILE METHODS
// ==========================================

func (s *Service) GetUser(ctx context.Context, userID string) (*GetUserResponse, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if user.Status == "DELETED" {
		return nil, ErrUserNotFound
	}

	return &GetUserResponse{
		UserID:       user.ID.Hex(),
		FullName:     user.FullName,
		NationalID:   user.NationalID,
		Email:        user.Email,
		PhoneNumbers: user.PhoneNumbers,
		BirthDate:    user.BirthDate,
		IsVerified:   user.IsVerified,
		WalletIDs:    user.WalletIDs,
		Status:       user.Status,
		CreatedAt:    user.CreatedAt,
	}, nil
}

func (s *Service) UpdateUser(ctx context.Context, userID string, req *UpdateUserRequest) (*GetUserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	updates := make(map[string]interface{})
	if req.FullName != nil {
		updates["full_name"] = *req.FullName
	}
	if req.Email != nil {
		updates["email"] = *req.Email
	}
	updates["updated_at"] = time.Now()

	user, err := s.repo.UpdateUser(ctx, userID, updates)
	if err != nil {
		return nil, err
	}

	return &GetUserResponse{
		UserID:       user.ID.Hex(),
		FullName:     user.FullName,
		NationalID:   user.NationalID,
		Email:        user.Email,
		PhoneNumbers: user.PhoneNumbers,
		BirthDate:    user.BirthDate,
		IsVerified:   user.IsVerified,
		WalletIDs:    user.WalletIDs,
		Status:       user.Status,
		CreatedAt:    user.CreatedAt,
	}, nil
}

func (s *Service) SoftDeleteUser(ctx context.Context, userID string) error {
	log := logger.Ctx(ctx)

	// 1. Soft delete the user
	if err := s.repo.SoftDeleteUser(ctx, userID); err != nil {
		return err
	}

	// 2. Revoke all refresh tokens
	if err := s.repo.RevokeAllUserTokens(ctx, userID); err != nil {
		log.Warn().Err(err).Str("user_id", userID).Msg("Failed to revoke tokens on delete")
	}

	// 3. Delete all wallets for this user via gRPC
	if s.walletClient != nil {
		if err := s.walletClient.DeleteUserWallets(ctx, userID); err != nil {
			log.Error().Err(err).Str("user_id", userID).Msg("Failed to delete user wallets in Wallet Service")
		}
	}

	return nil
}

// ==========================================
// PHONE METHODS
// ==========================================

func (s *Service) AddPhoneNumber(ctx context.Context, userID string, phone string) error {
	if err := ValidatePhoneNumber(&phone); err != nil {
		return err
	}
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if len(user.PhoneNumbers) >= MaxPhonesPerUser {
		return ErrMaxPhonesReached
	}
	return s.repo.AddPhoneNumber(ctx, userID, phone)
}

func (s *Service) RemovePhoneNumber(ctx context.Context, userID string, phone string) error {
	if err := ValidatePhoneNumber(&phone); err != nil {
		return err
	}
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Cannot remove the last phone number
	if len(user.PhoneNumbers) <= 1 {
		return errors.New("cannot remove the last phone number")
	}

	// Check if a wallet uses this phone
	if s.walletClient != nil {
		wallets, err := s.walletClient.GetUserWallets(ctx, userID)
		if err == nil && wallets != nil {
			for _, w := range wallets.Wallets {
				if w.PhoneNumber == phone {
					return ErrPhoneHasWallet
				}
			}
		}
	}

	return s.repo.RemovePhoneNumber(ctx, userID, phone)
}

// ==========================================
// WALLET PROXY METHODS
// ==========================================

func (s *Service) CreateWallet(ctx context.Context, userID string, req *CreateWalletProxyRequest) (*CreateWalletProxyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	log := logger.Ctx(ctx)
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(user.WalletIDs) >= MaxWalletsPerUser {
		return nil, ErrMaxWalletsReached
	}

	// Check if phone belongs to user
	phoneFound := false
	for _, p := range user.PhoneNumbers {
		if p == req.PhoneNumber {
			phoneFound = true
			break
		}
	}
	if !phoneFound {
		return nil, errors.New("phone number does not belong to user")
	}

	if s.walletClient == nil {
		return nil, errors.New("wallet client not configured")
	}

	resp, err := s.walletClient.CreateWallet(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := s.repo.AddWalletID(ctx, userID, resp.WalletID); err != nil {
		log.Error().Err(err).Msg("Failed to add wallet ID to user document after creation")
	}

	return resp, nil
}

func (s *Service) DeleteWallet(ctx context.Context, userID string, req *DeleteWalletRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if s.walletClient == nil {
		return errors.New("wallet client not configured")
	}

	// 1. Remove the wallet ID from MongoDB first (using filtering for ownership)
	err := s.repo.RemoveWalletIDIfOwns(ctx, userID, req.WalletID)
	if err != nil {
		return err
	}

	// 2. Perform gRPC call
	if err := s.walletClient.DeleteWallet(ctx, req); err != nil {
		// Rollback on failure
		_ = s.repo.AddWalletID(ctx, userID, req.WalletID)
		return err
	}

	return nil
}

func (s *Service) GetUserWallets(ctx context.Context, userID string) (*GetUserWalletsResponse, error) {
	if s.walletClient == nil {
		return nil, errors.New("wallet client not configured")
	}
	return s.walletClient.GetUserWallets(ctx, userID)
}

func (s *Service) GetWalletBalance(ctx context.Context, userID string, walletID string) (*GetWalletBalanceProxyResponse, error) {
	if s.walletClient == nil {
		return nil, errors.New("wallet client not configured")
	}

	ownsWallet, err := s.repo.HasWallet(ctx, userID, walletID)
	if err != nil {
		return nil, errors.New("failed to verify wallet ownership")
	}
	if !ownsWallet {
		return nil, ErrWalletOwnership
	}

	return s.walletClient.GetWalletBalance(ctx, walletID)
}
