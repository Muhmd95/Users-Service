package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"svc-users/util/token"
)

type refreshRepository struct {
	Repository
	record       *RefreshToken
	lookups      int
	revocations  int
	revokedToken string
	revokeErr    error
}

func (r *refreshRepository) GetRefreshToken(_ context.Context, _ string) (*RefreshToken, error) {
	r.lookups++
	return r.record, nil
}

func (r *refreshRepository) RevokeRefreshToken(_ context.Context, raw string) error {
	r.revocations++
	r.revokedToken = raw
	if r.revokeErr == nil {
		r.record.Revoked = true
	}
	return r.revokeErr
}

func TestRefreshRequiresRefreshPurposeAndActiveRecord(t *testing.T) {
	maker, err := token.NewJWTMaker("test-only-secret-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	userID := primitive.NewObjectID()
	repositoryFailure := errors.New("database unavailable")
	for _, tt := range []struct {
		name            string
		kind            token.TokenType
		revoked         bool
		want            error
		duration        time.Duration
		revokeErr       error
		wantRevocations int
	}{
		{name: "access token rejected before database", kind: token.AccessToken, want: ErrInvalidToken},
		{name: "refresh issues access token", kind: token.RefreshToken},
		{name: "revoked refresh rejected", kind: token.RefreshToken, revoked: true, want: ErrTokenRevoked},
		{name: "expired refresh revoked", kind: token.RefreshToken, duration: -time.Hour, want: ErrTokenExpired, wantRevocations: 1},
		{name: "expired access rejected without revocation", kind: token.AccessToken, duration: -time.Hour, want: ErrInvalidToken},
		{name: "expired already revoked", kind: token.RefreshToken, revoked: true, duration: -time.Hour, want: ErrTokenExpired, wantRevocations: 1},
		{name: "expired record removed by TTL", kind: token.RefreshToken, duration: -time.Hour, revokeErr: ErrInvalidToken, want: ErrTokenExpired, wantRevocations: 1},
		{name: "expiry revocation failed", kind: token.RefreshToken, duration: -time.Hour, revokeErr: repositoryFailure, want: repositoryFailure, wantRevocations: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			duration := tt.duration
			if duration == 0 {
				duration = time.Hour
			}
			raw, _, err := maker.CreateToken(userID.Hex(), tt.kind, duration)
			if err != nil {
				t.Fatal(err)
			}
			repo := &refreshRepository{record: &RefreshToken{UserID: userID, Revoked: tt.revoked}, revokeErr: tt.revokeErr}
			svc := NewService(repo, nil, nil, maker, 15*time.Minute, time.Hour, nil)
			resp, err := svc.RefreshToken(context.Background(), &RefreshTokenRequest{RefreshToken: raw})
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
			if (tt.kind == token.AccessToken || duration < 0) && repo.lookups != 0 {
				t.Fatal("invalid or expired token reached the active-record lookup")
			}
			if repo.revocations != tt.wantRevocations {
				t.Fatalf("revocations = %d; want %d", repo.revocations, tt.wantRevocations)
			}
			if tt.wantRevocations != 0 {
				if repo.revokedToken != raw || resp != nil {
					t.Fatal("expiry cleanup revoked another token or issued an access token")
				}
				if tt.revokeErr == nil && !repo.record.Revoked {
					t.Fatal("expired token was not marked revoked")
				}
			}
			if tt.want == nil {
				if _, err := maker.VerifyToken(resp.AccessToken, token.AccessToken); err != nil {
					t.Fatalf("refresh did not issue a valid access token: %v", err)
				}
			}
		})
	}
}
