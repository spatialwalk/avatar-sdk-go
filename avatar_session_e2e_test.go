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

// TestAvatarSessionStartEndToEnd performs an integration call against the real ingress websocket.
// It requires the environment variables AVATAR_API_KEY, AVATAR_CONSOLE_ENDPOINT, AVATAR_INGRESS_ENDPOINT,
// and AVATAR_SESSION_AVATAR_ID to be set. The ingress endpoint should be the base URL that hosts the
// websocket endpoint (without the /websocket suffix).
func TestAvatarSessionStartEndToEnd(t *testing.T) {
	apiKey, ok := os.LookupEnv("AVATAR_API_KEY")
	if !ok || apiKey == "" {
		t.Skip("AVATAR_API_KEY not set; skipping end-to-end test")
	}

	consoleEndpoint, ok := os.LookupEnv("AVATAR_CONSOLE_ENDPOINT")
	if !ok || consoleEndpoint == "" {
		t.Skip("AVATAR_CONSOLE_ENDPOINT not set; skipping end-to-end test")
	}

	ingressEndpoint, ok := os.LookupEnv("AVATAR_INGRESS_ENDPOINT")
	if !ok || ingressEndpoint == "" {
		t.Skip("AVATAR_INGRESS_ENDPOINT not set; skipping end-to-end test")
	}

	avatarID, ok := os.LookupEnv("AVATAR_SESSION_AVATAR_ID")
	if !ok || avatarID == "" {
		t.Skip("AVATAR_SESSION_AVATAR_ID not set; skipping end-to-end test")
	}

	expireAt := time.Now().Add(5 * time.Minute).UTC()

	session := NewAvatarSession(
		WithAPIKey(apiKey),
		WithConsoleEndpointURL(consoleEndpoint),
		WithIngressEndpointURL(ingressEndpoint),
		WithAvatarID(avatarID),
		WithExpireAt(expireAt),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := session.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if session.sessionToken == "" {
		t.Fatal("expected session token to be populated after Init")
	}

	if _, err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Logf("Close returned error: %v", err)
		} else {
			t.Log("session closed successfully")
		}
	}()

	if session.conn == nil {
		t.Fatal("expected websocket connection to be established")
	} else {
		t.Log("websocket connection established successfully")
	}
}
