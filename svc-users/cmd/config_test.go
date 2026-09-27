package main

import "testing"

func TestSecureCookiesOnlyDisabledInDevelopment(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("WALLET_GRPC_URL", "localhost:50051")
	t.Setenv("JWT_SECRET", "test-only-secret-at-least-32-characters")
	for _, env := range []string{"development", "production", "staging", "", "developmnt"} {
		t.Run("env="+env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			cfg, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SecureCookies != (env != "development") {
				t.Fatalf("unexpected secure cookie setting for APP_ENV=%q", env)
			}
		})
	}
}
