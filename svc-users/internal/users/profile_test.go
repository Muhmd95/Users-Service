package users

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"svc-users/external/redis"
)

type profileRepository struct {
	Repository
	user    User
	updates map[string]interface{}
	findErr error
}

func (r *profileRepository) GetUserByID(context.Context, string) (*User, error) {
	return &r.user, r.findErr
}

func (r *profileRepository) GetUserByEmail(context.Context, string) (*User, error) {
	return &r.user, r.findErr
}

func (r *profileRepository) UpdateUser(_ context.Context, _ string, updates map[string]interface{}) (*User, error) {
	r.updates = updates
	if email, ok := updates["email"].(string); ok {
		r.user.Email = email
	}
	if verified, ok := updates["is_verified"].(bool); ok {
		r.user.IsVerified = verified
	}
	return &r.user, nil
}

func TestProfileEmailVerification(t *testing.T) {
	for _, tt := range []struct {
		name         string
		email        string
		includeEmail bool
		verified     bool
		wantEmail    string
		wantVerified bool
	}{
		{"new email resets verification", "new@example.com", true, true, "new@example.com", false},
		{"same email preserves verification", "old@example.com", true, true, "old@example.com", true},
		{"trimmed same email preserves verification", " old@example.com ", true, true, "old@example.com", true},
		{"name-only update preserves verification", "", false, true, "old@example.com", true},
		{"unverified account stays unverified", "new@example.com", true, false, "new@example.com", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &profileRepository{user: User{ID: primitive.NewObjectID(), Email: "old@example.com", IsVerified: tt.verified}}
			svc := &Service{repo: repo}
			name := "Updated Name"
			req := &UpdateUserRequest{FullName: &name}
			if tt.includeEmail {
				req.Email = &tt.email
			}
			resp, err := svc.UpdateUser(context.Background(), repo.user.ID.Hex(), req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Email != tt.wantEmail || resp.IsVerified != tt.wantVerified {
				t.Fatalf("unexpected result: email=%s verified=%v", resp.Email, resp.IsVerified)
			}
			_, hasEmail := repo.updates["email"]
			_, hasVerification := repo.updates["is_verified"]
			changed := tt.wantEmail != "old@example.com"
			if hasEmail != changed || hasVerification != changed {
				t.Fatalf("email and verification must be updated together only on change: %v", repo.updates)
			}
		})
	}
}

func TestInvalidProfileDoesNotWrite(t *testing.T) {
	badEmail := "not-an-email"
	repo := &profileRepository{}
	svc := &Service{repo: repo}
	_, err := svc.UpdateUser(context.Background(), "user-1", &UpdateUserRequest{Email: &badEmail})
	if !errors.Is(err, ErrInvalidInput) || repo.updates != nil {
		t.Fatalf("invalid email must fail before update: err=%v updates=%v", err, repo.updates)
	}
}

type verificationStore struct {
	err     error
	deleted bool
}

func (s *verificationStore) Get(context.Context, string) (string, error) {
	return "old@example.com", s.err
}

func (s *verificationStore) Del(context.Context, string) error {
	s.deleted = true
	return nil
}

func TestVerificationDistinguishesBadTokensFromOutages(t *testing.T) {
	outage := errors.New("backend unavailable")
	for _, tt := range []struct {
		name     string
		storeErr error
		findErr  error
		want     error
	}{
		{"expired or missing token", redis.ErrNotFound, nil, ErrInvalidVerificationToken},
		{"orphaned token", nil, ErrUserNotFound, ErrInvalidVerificationToken},
		{"Redis outage", outage, nil, outage},
		{"database outage", nil, outage, outage},
		{"valid token", nil, nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &verificationStore{err: tt.storeErr}
			repo := &profileRepository{user: User{ID: primitive.NewObjectID()}, findErr: tt.findErr}
			svc := &Service{repo: repo, rdb: store}
			err := svc.VerifyEmail(context.Background(), &VerifyEmailRequest{Token: "test-token"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
			if tt.want == outage && errors.Is(err, ErrInvalidVerificationToken) {
				t.Fatal("backend failure was mislabeled as an invalid token")
			}
			if tt.want != nil && (repo.updates != nil || store.deleted) {
				t.Fatal("failed verification changed user or token state")
			}
			if tt.want == nil && (!repo.user.IsVerified || !store.deleted) {
				t.Fatal("successful verification must verify user and consume token")
			}
		})
	}
}
