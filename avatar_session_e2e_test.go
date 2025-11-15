package avatarsdkgo

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestAvatarSessionInitEndToEnd performs an integration call against the real console API.
// It requires the environment variables AVATAR_API_KEY and AVATAR_CONSOLE_ENDPOINT to be set.
// The endpoint should include the /v1/console prefix, e.g. https://api.example.com/v1/console.
func TestAvatarSessionInitEndToEnd(t *testing.T) {
	apiKey, ok := os.LookupEnv("AVATAR_API_KEY")
	if !ok || apiKey == "" {
		t.Skip("AVATAR_API_KEY not set; skipping end-to-end test")
	}

	consoleEndpoint, ok := os.LookupEnv("AVATAR_CONSOLE_ENDPOINT")
	if !ok || consoleEndpoint == "" {
		t.Skip("AVATAR_CONSOLE_ENDPOINT not set; skipping end-to-end test")
	}

	expireAt := time.Now().Add(5 * time.Minute).UTC()

	session := NewAvatarSession(
		WithAPIKey(apiKey),
		WithConsoleEndpointURL(consoleEndpoint),
		WithExpireAt(expireAt),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := session.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if session.sessionToken == "" {
		t.Fatal("expected session token to be populated")
	} else {
		t.Logf("obtained session token: %s", session.sessionToken)
	}
}
