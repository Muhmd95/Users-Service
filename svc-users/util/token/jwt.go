package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken = errors.New("token is invalid")
	ErrExpiredToken = errors.New("token has expired")
)

type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

type Maker interface {
	CreateToken(userID string, tokenType TokenType, duration time.Duration) (string, *Payload, error)
	VerifyToken(token string, expectedType TokenType) (*Payload, error)
}

type Payload struct {
	UserID    string    `json:"user_id"`
	TokenType TokenType `json:"token_type"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiredAt time.Time `json:"expired_at"`
	jwt.RegisteredClaims
}

type JWTMaker struct {
	secretKey string
}

func NewJWTMaker(secretKey string) (Maker, error) {
	if len(secretKey) < 32 {
		return nil, errors.New("invalid key size: must be at least 32 characters")
	}
	return &JWTMaker{secretKey: secretKey}, nil
}

func (maker *JWTMaker) CreateToken(userID string, tokenType TokenType, duration time.Duration) (string, *Payload, error) {
	if userID == "" || (tokenType != AccessToken && tokenType != RefreshToken) {
		return "", nil, ErrInvalidToken
	}
	now := time.Now()
	expiresAt := now.Add(duration)
	payload := &Payload{
		UserID:    userID,
		TokenType: tokenType,
		IssuedAt:  now,
		ExpiredAt: expiresAt,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   userID,
		},
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, payload)
	tokenString, err := jwtToken.SignedString([]byte(maker.secretKey))
	if err != nil {
		return "", nil, err
	}

	return tokenString, payload, nil
}

func (maker *JWTMaker) VerifyToken(tokenString string, expectedType TokenType) (*Payload, error) {
	if expectedType != AccessToken && expectedType != RefreshToken {
		return nil, ErrInvalidToken
	}
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		return []byte(maker.secretKey), nil
	}

	jwtToken, err := jwt.ParseWithClaims(tokenString, &Payload{}, keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	payload, ok := jwtToken.Claims.(*Payload)
	if !ok || !jwtToken.Valid || payload.TokenType != expectedType || payload.UserID == "" || payload.Subject != payload.UserID {
		return nil, ErrInvalidToken
	}

	return payload, nil
}
