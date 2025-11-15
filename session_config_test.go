package avatarsdkgo

import (
	"errors"
	"testing"
	"time"
)

func TestSessionOptionOverrides(t *testing.T) {
	cfg := defaultSessionConfig()

	expireAt := time.Now().Add(5 * time.Minute)
	var framesCalled bool
	var onErrorCalled bool
	var onCloseCalled bool

	frameHandler := func(data []byte) {
		framesCalled = true
		if string(data) != "payload" {
			t.Fatalf("unexpected frame payload: %s", string(data))
		}
	}

	errSentinel := errors.New("boom")
	errorHandler := func(err error) {
		onErrorCalled = err == errSentinel
	}

	closeHandler := func() {
		onCloseCalled = true
	}

	opts := []SessionOption{
		WithAvatarID("avatar-123"),
		WithAPIKey("api-key"),
		WithAppID("app-id"),
		WithExpireAt(expireAt),
		WithSampleRate(48000),
		WithTransportFrames(frameHandler),
		WithHeartbeatInterval(2 * time.Second),
		WithHeartbeatTimeout(3 * time.Second),
		WithOnError(errorHandler),
		WithOnClose(closeHandler),
		WithConsoleEndpointURL("https://console.test"),
		WithIngressEndpointURL("https://ingress.test"),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.AvatarID != "avatar-123" {
		t.Fatalf("expected AvatarID to be set, got %q", cfg.AvatarID)
	}
	if cfg.APIKey != "api-key" {
		t.Fatalf("expected APIKey to be set, got %q", cfg.APIKey)
	}
	if cfg.AppID != "app-id" {
		t.Fatalf("expected AppID to be set, got %q", cfg.AppID)
	}
	if !cfg.ExpireAt.Equal(expireAt) {
		t.Fatalf("expected ExpireAt to be %v, got %v", expireAt, cfg.ExpireAt)
	}
	if cfg.SampleRate != 48000 {
		t.Fatalf("expected SampleRate to be 48000, got %f", cfg.SampleRate)
	}
	if cfg.HeartbeatInterval != 2*time.Second {
		t.Fatalf("expected HeartbeatInterval to be 2s, got %v", cfg.HeartbeatInterval)
	}
	if cfg.HeartbeatTimeout != 3*time.Second {
		t.Fatalf("expected HeartbeatTimeout to be 3s, got %v", cfg.HeartbeatTimeout)
	}
	if cfg.ConsoleEndpointURL != "https://console.test" {
		t.Fatalf("expected ConsoleEndpointURL to be set, got %q", cfg.ConsoleEndpointURL)
	}
	if cfg.IngressEndpointURL != "https://ingress.test" {
		t.Fatalf("expected IngressEndpointURL to be set, got %q", cfg.IngressEndpointURL)
	}

	if cfg.TransportFrames == nil {
		t.Fatal("TransportFrames handler should not be nil")
	}
	cfg.TransportFrames([]byte("payload"))
	if !framesCalled {
		t.Fatal("TransportFrames handler was not invoked")
	}

	if cfg.OnError == nil {
		t.Fatal("OnError handler should not be nil")
	}
	cfg.OnError(errSentinel)
	if !onErrorCalled {
		t.Fatal("OnError handler was not invoked with sentinel error")
	}

	if cfg.OnClose == nil {
		t.Fatal("OnClose handler should not be nil")
	}
	cfg.OnClose()
	if !onCloseCalled {
		t.Fatal("OnClose handler was not invoked")
	}
}

func TestSessionOptionDefaults(t *testing.T) {
	cfg := defaultSessionConfig()

	if cfg.TransportFrames == nil {
		t.Fatal("default TransportFrames should be non-nil")
	}
	if cfg.OnError == nil {
		t.Fatal("default OnError should be non-nil")
	}
	if cfg.OnClose == nil {
		t.Fatal("default OnClose should be non-nil")
	}

	// Ensure default handlers do not panic.
	cfg.TransportFrames([]byte("noop"))
	cfg.OnError(nil)
	cfg.OnClose()
}

func TestNilHandlersUseNoopDefaults(t *testing.T) {
	cfg := defaultSessionConfig()

	WithTransportFrames(nil)(cfg)
	if cfg.TransportFrames == nil {
		t.Fatal("TransportFrames should default to a no-op handler")
	}
	safeInvoke(t, func() { cfg.TransportFrames(nil) })

	WithOnError(nil)(cfg)
	if cfg.OnError == nil {
		t.Fatal("OnError should default to a no-op handler")
	}
	safeInvoke(t, func() { cfg.OnError(nil) })

	WithOnClose(nil)(cfg)
	if cfg.OnClose == nil {
		t.Fatal("OnClose should default to a no-op handler")
	}
	safeInvoke(t, cfg.OnClose)
}

func safeInvoke(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler panic: %v", r)
		}
	}()
	fn()
}
