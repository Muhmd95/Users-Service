package token

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-only-secret-at-least-32-characters"

func TestTokenPurpose(t *testing.T) {
	maker, err := NewJWTMaker(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []TokenType{AccessToken, RefreshToken} {
		t.Run(string(kind), func(t *testing.T) {
			raw, _, err := maker.CreateToken("user-1", kind, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := maker.VerifyToken(raw, kind)
			if err != nil || payload.UserID != "user-1" || payload.TokenType != kind {
				t.Fatalf("expected valid %s token, got payload=%v err=%v", kind, payload, err)
			}
			other := AccessToken
			if kind == AccessToken {
				other = RefreshToken
			}
			if _, err := maker.VerifyToken(raw, other); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("accepted %s token as %s: %v", kind, other, err)
			}
		})
	}
}

func TestRejectInvalidTokens(t *testing.T) {
	maker, err := NewJWTMaker(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(jwt.MapClaims)
		method jwt.SigningMethod
		secret string
		want   error
	}{
		{name: "legacy token without purpose", change: func(c jwt.MapClaims) { delete(c, "token_type") }},
		{name: "unknown purpose", change: func(c jwt.MapClaims) { c["token_type"] = "other" }},
		{name: "missing expiration", change: func(c jwt.MapClaims) { delete(c, "exp") }},
		{name: "missing user", change: func(c jwt.MapClaims) { delete(c, "user_id") }},
		{name: "mismatched subject", change: func(c jwt.MapClaims) { c["sub"] = "another-user" }},
		{name: "expired", change: func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, want: ErrExpiredToken},
		{name: "expired wrong purpose", change: func(c jwt.MapClaims) {
			c["exp"] = time.Now().Add(-time.Hour).Unix()
			c["token_type"] = "refresh"
		}},
		{name: "expired without purpose", change: func(c jwt.MapClaims) {
			c["exp"] = time.Now().Add(-time.Hour).Unix()
			delete(c, "token_type")
		}},
		{name: "expired and not yet valid", change: func(c jwt.MapClaims) {
			c["exp"] = time.Now().Add(-time.Hour).Unix()
			c["nbf"] = time.Now().Add(time.Hour).Unix()
		}},
		{name: "wrong signing algorithm", method: jwt.SigningMethodHS384},
		{name: "wrong signature", secret: "different-secret-at-least-32-characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := jwt.MapClaims{"user_id": "user-1", "sub": "user-1", "token_type": "access", "exp": time.Now().Add(time.Hour).Unix()}
			if tt.change != nil {
				tt.change(claims)
			}
			method := tt.method
			if method == nil {
				method = jwt.SigningMethodHS256
			}
			secret := tt.secret
			if secret == "" {
				secret = testSecret
			}
			raw, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
			if err != nil {
				t.Fatal(err)
			}
			want := tt.want
			if want == nil {
				want = ErrInvalidToken
			}
			if _, err := maker.VerifyToken(raw, AccessToken); !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
		})
	}
}
