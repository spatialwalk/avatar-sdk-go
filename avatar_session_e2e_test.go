package avatarsdkgo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAvatarSessionInitEndToEnd performs an integration call against the real console API.
// It requires the environment variables AVATAR_API_KEY and AVATAR_CONSOLE_ENDPOINT to be set.
// The endpoint should include the /v1/console prefix, e.g. https://api.example.com/v1/console.
func TestAvatarSessionInitEndToEnd(t *testing.T) {
	apiKey := envOrSkip(t, "AVATAR_API_KEY")
	consoleEndpoint := envOrSkip(t, "AVATAR_CONSOLE_ENDPOINT")

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
	}
}

// TestAvatarSessionStartEndToEnd performs an integration call against the real ingress websocket.
// It requires the environment variables AVATAR_API_KEY, AVATAR_CONSOLE_ENDPOINT, AVATAR_INGRESS_ENDPOINT,
// and AVATAR_SESSION_AVATAR_ID to be set. The ingress endpoint should be the base URL that hosts the
// websocket endpoint (without the /websocket suffix).
func TestAvatarSessionStartEndToEnd(t *testing.T) {
	apiKey := envOrSkip(t, "AVATAR_API_KEY")
	consoleEndpoint := envOrSkip(t, "AVATAR_CONSOLE_ENDPOINT")
	ingressEndpoint := envOrSkip(t, "AVATAR_INGRESS_ENDPOINT")
	avatarID := envOrSkip(t, "AVATAR_SESSION_AVATAR_ID")

	expireAt := time.Now().Add(5 * time.Minute).UTC()

	session := NewAvatarSession(
		WithAPIKey(apiKey),
		WithConsoleEndpointURL(consoleEndpoint),
		WithIngressEndpointURL(ingressEndpoint),
		WithAvatarID(avatarID),
		WithExpireAt(expireAt),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := session.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if session.sessionToken == "" {
		t.Fatal("expected session token to be populated after Init")
	}

	connectionID, err := session.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if connectionID == "" {
		t.Fatal("expected non-empty connection id")
	}

	defer func() {
		if err := session.Close(); err != nil {
			t.Logf("Close returned error: %v", err)
		}
	}()

	if s := session.conn; s == nil {
		t.Fatal("expected websocket connection to be established")
	}
}

func TestAvatarSessionEndToEnd(t *testing.T) {
	apiKey := envOrSkip(t, "AVATAR_API_KEY")
	consoleEndpoint := envOrSkip(t, "AVATAR_CONSOLE_ENDPOINT")
	ingressEndpoint := envOrSkip(t, "AVATAR_INGRESS_ENDPOINT")
	avatarID := envOrSkip(t, "AVATAR_SESSION_AVATAR_ID")

	audioPath := filepath.Join("audio.pcm")
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read audio fixture %q: %v", audioPath, err)
	}

	session := NewAvatarSession(
		WithAPIKey(apiKey),
		WithConsoleEndpointURL(consoleEndpoint),
		WithIngressEndpointURL(ingressEndpoint),
		WithAvatarID(avatarID),
		WithExpireAt(time.Now().Add(5*time.Second).UTC()),
		WithTransportFrames(func(data []byte) {
			t.Logf("received transport frame of %d bytes", len(data))
		}),
		WithOnError(func(err error) {
			t.Logf("received error: %v", err)
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := session.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if session.sessionToken == "" {
		t.Fatal("expected session token to be populated after Init")
	}

	connectionID, err := session.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if connectionID == "" {
		t.Fatal("expected non-empty connection id")
	}

	defer func() {
		if err := session.Close(); err != nil {
			t.Logf("Close returned error: %v", err)
		}
	}()

	reqID, err := session.SendAudio(audioData, true)
	if err != nil {
		t.Fatalf("SendAudio failed: %v", err)
	}
	if reqID == "" {
		t.Fatal("expected non-empty request id")
	}
	t.Logf("sent audio with request id %q", reqID)

	<-ctx.Done()
}

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		t.Skipf("%s not set; skipping end-to-end test", key)
	}
	return value
}
