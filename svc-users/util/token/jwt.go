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

type Maker interface {
	CreateToken(userID string, duration time.Duration) (string, *Payload, error)
	VerifyToken(token string) (*Payload, error)
}

type Payload struct {
	UserID    string    `json:"user_id"`
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

func (maker *JWTMaker) CreateToken(userID string, duration time.Duration) (string, *Payload, error) {
	payload := &Payload{
		UserID:    userID,
		IssuedAt:  time.Now(),
		ExpiredAt: time.Now().Add(duration),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
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

func (maker *JWTMaker) VerifyToken(tokenString string) (*Payload, error) {
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		_, ok := token.Method.(*jwt.SigningMethodHMAC)
		if !ok {
			return nil, ErrInvalidToken
		}
		return []byte(maker.secretKey), nil
	}

	jwtToken, err := jwt.ParseWithClaims(tokenString, &Payload{}, keyFunc)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	payload, ok := jwtToken.Claims.(*Payload)
	if !ok {
		return nil, ErrInvalidToken
	}

	return payload, nil
}
