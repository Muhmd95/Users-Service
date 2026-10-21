package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"svc-users/internal/users"
)

func TestTransactionPhoneOwnershipReturnsForbidden(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleTransactionError(recorder, users.ErrPhoneOwnership)
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusForbidden || body["error"] != "forbidden: phone number is not registered to your account" {
		t.Fatalf("unexpected response: %d %s", recorder.Code, recorder.Body.String())
	}
}
