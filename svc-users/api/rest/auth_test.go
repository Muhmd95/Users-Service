package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"svc-users/util/token"
)

func TestAuthMiddlewareRejectsRefreshToken(t *testing.T) {
	maker, err := token.NewJWTMaker("test-only-secret-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []token.TokenType{token.AccessToken, token.RefreshToken} {
		t.Run(string(kind), func(t *testing.T) {
			raw, _, err := maker.CreateToken("user-1", kind, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			handler := AuthMiddleware(maker)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if getUserID(r) != "user-1" {
					t.Error("authenticated user ID missing")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/v1/users/me", nil)
			req.Header.Set("Authorization", "Bearer "+raw)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			want := http.StatusUnauthorized
			if kind == token.AccessToken {
				want = http.StatusNoContent
			}
			if recorder.Code != want || called != (kind == token.AccessToken) {
				t.Fatalf("status=%d called=%v; expected status=%d", recorder.Code, called, want)
			}
		})
	}
}

func TestRefreshCookieAttributes(t *testing.T) {
	for _, secure := range []bool{false, true} {
		controller := NewController(nil, secure)
		for _, clear := range []bool{false, true} {
			recorder := httptest.NewRecorder()
			if clear {
				controller.clearRefreshTokenCookie(recorder)
			} else {
				controller.setRefreshTokenCookie(recorder, "refresh-token")
			}
			cookies := recorder.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("expected one cookie, got %d", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != "refresh_token" || cookie.Path != "/" || cookie.Secure != secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("unexpected cookie attributes: %+v", cookie)
			}
			if clear && (cookie.Value != "" || cookie.MaxAge != -1) {
				t.Fatalf("cookie was not cleared: %+v", cookie)
			}
			if !clear && (cookie.Value != "refresh-token" || cookie.MaxAge <= 0) {
				t.Fatalf("cookie was not set: %+v", cookie)
			}
		}
	}
}
