package users

import (
	"context"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"svc-users/external/redis"
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
	Close() error
}

type Service struct {
	repo                 Repository
	walletClient         WalletClient
	emailJobs            chan<- EmailJob
	tokenMaker           token.Maker
	accessTokenDuration  time.Duration
	refreshTokenDuration time.Duration
	rdb                  *redis.Client
}

func NewService(repo Repository, walletClient WalletClient, emailJobs chan<- EmailJob, tokenMaker token.Maker, accessDuration, refreshDuration time.Duration, rdb *redis.Client) *Service {
	return &Service{
		repo:                 repo,
		walletClient:         walletClient,
		emailJobs:            emailJobs,
		tokenMaker:           tokenMaker,
		accessTokenDuration:  accessDuration,
		refreshTokenDuration: refreshDuration,
		rdb:                  rdb,
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
	ctx, span := otel.Tracer("users-service").Start(ctx, "RegisterUser")
	defer span.End()
	log := logger.Ctx(ctx).With().Str("layer", "service").Logger()

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
		if err == ErrDuplicateNationalID {
			existingUser, fetchErr := s.repo.GetUserByNationalID(ctx, req.NationalID)
			if fetchErr == nil && existingUser.Status == "DELETED" {
				updates := map[string]interface{}{
					"full_name":     req.FullName,
					"email":         req.Email,
					"password_hash": string(hashedPassword),
					"phone_numbers": []string{req.PhoneNumber},
					"is_verified":   false,
					"wallet_ids":    []string{},
				}
				reactivateErr := s.repo.ReactivateUser(ctx, existingUser.ID.Hex(), updates)
				if reactivateErr != nil {
					return nil, reactivateErr
				}
				user.ID = existingUser.ID
				err = nil
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
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
	ctx, span := otel.Tracer("users-service").Start(ctx, "VerifyEmail")
	defer span.End()
	if err := req.Validate(); err != nil {
		return err
	}

	email, err := s.rdb.Get(ctx, "email_verify:"+req.Token)
	if err != nil {
		return errors.New("invalid or expired verification token")
	}

	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return errors.New("user not found for this token")
	}

	_, err = s.repo.UpdateUser(ctx, user.ID.Hex(), map[string]interface{}{
		"is_verified": true,
		"updated_at":  time.Now(),
	})
	if err != nil {
		return err
	}

	// Delete token after successful verification
	_ = s.rdb.Del(ctx, "email_verify:"+req.Token)

	return nil
}

func (s *Service) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	ctx, span := otel.Tracer("users-service").Start(ctx, "Login")
	defer span.End()
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
	accessToken, _, err := s.tokenMaker.CreateToken(user.ID.Hex(), token.AccessToken, s.accessTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to create access token: %w", err)
	}

	// Generate Refresh Token
	refreshToken, refreshPayload, err := s.tokenMaker.CreateToken(user.ID.Hex(), token.RefreshToken, s.refreshTokenDuration)
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
	ctx, span := otel.Tracer("users-service").Start(ctx, "RefreshToken")
	defer span.End()
	if err := req.Validate(); err != nil {
		return nil, err
	}
	// Verify the provided refresh token
	refreshPayload, err := s.tokenMaker.VerifyToken(req.RefreshToken, token.RefreshToken)
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
	accessToken, accessPayload, err := s.tokenMaker.CreateToken(refreshPayload.UserID, token.AccessToken, s.accessTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to create access token: %w", err)
	}

	return &RefreshTokenResponse{
		AccessToken: accessToken,
		ExpiresIn:   int64(time.Until(accessPayload.ExpiredAt).Seconds()),
	}, nil
}

func (s *Service) Logout(ctx context.Context, userID string) error {
	ctx, span := otel.Tracer("users-service").Start(ctx, "Logout")
	defer span.End()
	// For Phase 5 we will revoke the specific refresh token, but for now
	// we will just revoke all tokens for this user.
	return s.repo.RevokeAllUserTokens(ctx, userID)
}

func (s *Service) ChangePassword(ctx context.Context, userID string, oldPassword, newPassword string) (*LoginResponse, error) {
	ctx, span := otel.Tracer("users-service").Start(ctx, "ChangePassword")
	defer span.End()
	log := logger.Ctx(ctx).With().Str("layer", "service").Logger()

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword))
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if len(newPassword) < 8 {
		return nil, ErrInvalidPassword
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Error().Err(err).Msg("failed to hash password")
		return nil, fmt.Errorf("failed to process password")
	}

	updates := map[string]interface{}{
		"password_hash": string(hashedPassword),
		"updated_at":    time.Now(),
	}

	_, err = s.repo.UpdateUser(ctx, userID, updates)
	if err != nil {
		return nil, err
	}

	// Revoke ALL refresh tokens so all active sessions are logged out
	if err := s.repo.RevokeAllUserTokens(ctx, userID); err != nil {
		log.Warn().Err(err).Str("user_id", userID).Msg("Failed to revoke tokens on password change")
	}

	// Generate Access Token
	accessToken, _, err := s.tokenMaker.CreateToken(user.ID.Hex(), token.AccessToken, s.accessTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to create access token: %w", err)
	}

	// Generate Refresh Token
	refreshToken, refreshPayload, err := s.tokenMaker.CreateToken(user.ID.Hex(), token.RefreshToken, s.refreshTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh token: %w", err)
	}

	// Store Refresh Token in DB
	rt := &RefreshToken{
		UserID:    user.ID,
		TokenHash: refreshToken,
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

// ==========================================
// PROFILE METHODS
// ==========================================

func (s *Service) GetUser(ctx context.Context, userID string) (*GetUserResponse, error) {
	ctx, span := otel.Tracer("users-service").Start(ctx, "GetUser")
	defer span.End()
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
	ctx, span := otel.Tracer("users-service").Start(ctx, "UpdateUser")
	defer span.End()
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
	ctx, span := otel.Tracer("users-service").Start(ctx, "SoftDeleteUser")
	defer span.End()
	log := logger.Ctx(ctx).With().Str("layer", "service").Logger()

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
	ctx, span := otel.Tracer("users-service").Start(ctx, "AddPhoneNumber")
	defer span.End()
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
	ctx, span := otel.Tracer("users-service").Start(ctx, "RemovePhoneNumber")
	defer span.End()
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

func (s *Service) CreateWallet(ctx context.Context, req *CreateWalletProxyRequest) (*CreateWalletProxyResponse, error) {
	ctx, span := otel.Tracer("users-service").Start(ctx, "CreateWallet")
	defer span.End()
	if err := req.Validate(); err != nil {
		return nil, err
	}
	log := logger.Ctx(ctx).With().Str("layer", "service").Logger()
	user, err := s.repo.GetUserByID(ctx, req.UserID)
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

	// 4. Update local state: add the newly created wallet ID to the user's denormalized array in MongoDB.
	// dual write problem
	// solved by connecting the users database with a CDC to the wallets database
	if err := s.repo.AddWalletID(ctx, req.UserID, resp.WalletID); err != nil {
		log.Error().Err(err).Msg("Failed to add wallet ID to user document after creation")
	}

	return resp, nil
}

func (s *Service) DeleteWallet(ctx context.Context, userID string, req *DeleteWalletRequest) error {
	ctx, span := otel.Tracer("users-service").Start(ctx, "DeleteWallet")
	defer span.End()
	if err := req.Validate(); err != nil {
		return err
	}
	if s.walletClient == nil {
		return errors.New("wallet client not configured")
	}

	// 1. One-Trip Check & Delete: Remove the wallet ID from the user's denormalized array in MongoDB.
	// We use filtering {"_id": userID, "wallet_ids": req.WalletID} so it only succeeds if the user actually owns the wallet.
	err := s.repo.RemoveWalletIDIfOwns(ctx, userID, req.WalletID)
	if err != nil {
		return err
	}

	// 2. Perform gRPC call to actual Wallet Service
	if err := s.walletClient.DeleteWallet(ctx, req); err != nil {
		// 3. Saga Rollback: If the Wallet Service failed to delete it, we must revert the local state by putting the ID back.
		_ = s.repo.AddWalletID(ctx, userID, req.WalletID)
		return err
	}

	return nil
}

func (s *Service) GetUserWallets(ctx context.Context, userID string) (*GetUserWalletsResponse, error) {
	ctx, span := otel.Tracer("users-service").Start(ctx, "GetUserWallets")
	defer span.End()
	if s.walletClient == nil {
		return nil, errors.New("wallet client not configured")
	}
	return s.walletClient.GetUserWallets(ctx, userID)
}

func (s *Service) GetWalletBalance(ctx context.Context, userID string, walletID string) (*GetWalletBalanceProxyResponse, error) {
	ctx, span := otel.Tracer("users-service").Start(ctx, "GetWalletBalance")
	defer span.End()
	if s.walletClient == nil {
		return nil, errors.New("wallet client not configured")
	}

	// 1. Security check: Instantly verify ownership using the user's local denormalized wallet_ids array.
	// This completely avoids sending a redundant gRPC round-trip to the Wallet Service just for authorization.
	ownsWallet, err := s.repo.HasWallet(ctx, userID, walletID)
	if err != nil {
		return nil, errors.New("failed to verify wallet ownership")
	}
	if !ownsWallet {
		return nil, ErrWalletOwnership
	}

	// 2. Forward the balance request safely.
	return s.walletClient.GetWalletBalance(ctx, walletID)
}
