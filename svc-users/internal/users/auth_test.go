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
	record  *RefreshToken
	lookups int
}

func (r *refreshRepository) GetRefreshToken(_ context.Context, _ string) (*RefreshToken, error) {
	r.lookups++
	return r.record, nil
}

func TestRefreshRequiresRefreshPurposeAndActiveRecord(t *testing.T) {
	maker, err := token.NewJWTMaker("test-only-secret-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	userID := primitive.NewObjectID()
	for _, tt := range []struct {
		name    string
		kind    token.TokenType
		revoked bool
		want    error
	}{
		{name: "access token rejected before database", kind: token.AccessToken, want: ErrInvalidToken},
		{name: "refresh issues access token", kind: token.RefreshToken},
		{name: "revoked refresh rejected", kind: token.RefreshToken, revoked: true, want: ErrTokenRevoked},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, _, err := maker.CreateToken(userID.Hex(), tt.kind, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			repo := &refreshRepository{record: &RefreshToken{UserID: userID, Revoked: tt.revoked}}
			svc := NewService(repo, nil, nil, maker, 15*time.Minute, time.Hour, nil)
			resp, err := svc.RefreshToken(context.Background(), &RefreshTokenRequest{RefreshToken: raw})
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
			if tt.kind == token.AccessToken && repo.lookups != 0 {
				t.Fatal("access token reached the refresh-token repository")
			}
			if tt.want == nil {
				if _, err := maker.VerifyToken(resp.AccessToken, token.AccessToken); err != nil {
					t.Fatalf("refresh did not issue a valid access token: %v", err)
				}
			}
		})
	}
}
