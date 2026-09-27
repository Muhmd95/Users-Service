package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"svc-users/internal/users"
)

func TestClientValidationErrorsReturnBadRequest(t *testing.T) {
	badName := "x"
	badEmail := "invalid-email"
	validRegistration := users.RegisterUserRequest{FullName: "Example User", Email: "user@example.com", Password: "test-password", PhoneNumber: "01012345678", NationalID: "29901011234567"}
	badNameRegistration := validRegistration
	badNameRegistration.FullName = badName
	badEmailRegistration := validRegistration
	badEmailRegistration.Email = badEmail
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"registration name", badNameRegistration.Validate()},
		{"registration email", badEmailRegistration.Validate()},
		{"profile name", (&users.UpdateUserRequest{FullName: &badName}).Validate()},
		{"profile email", (&users.UpdateUserRequest{Email: &badEmail}).Validate()},
		{"missing verification token", (&users.VerifyEmailRequest{}).Validate()},
		{"invalid verification token", users.ErrInvalidVerificationToken},
		{"wrapped verification token", fmt.Errorf("verification: %w", users.ErrInvalidVerificationToken)},
		{"missing login password", (&users.LoginRequest{PhoneNumber: "01012345678"}).Validate()},
		{"missing refresh token", (&users.RefreshTokenRequest{}).Validate()},
		{"missing old password", (&users.ChangePasswordRequest{NewPassword: "test-password"}).Validate()},
		{"unsupported currency", (&users.CreateWalletProxyRequest{PhoneNumber: "01012345678", CurrencyCode: "USD"}).Validate()},
		{"missing wallet ID", (&users.DeleteWalletRequest{}).Validate()},
		{"last phone removal", fmt.Errorf("%w: cannot remove the last phone number", users.ErrInvalidInput)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatal("expected a validation error")
			}
			recorder := httptest.NewRecorder()
			handleError(recorder, tt.err)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Fatalf("expected JSON error, got %s", recorder.Body.String())
			}
		})
	}
}

func TestUnexpectedErrorRemainsMasked(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleError(recorder, errors.New("database connection contains private details"))
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusInternalServerError || body["error"] != "internal server error" {
		t.Fatalf("unexpected error leaked or incorrectly classified: %d %s", recorder.Code, recorder.Body.String())
	}
}
