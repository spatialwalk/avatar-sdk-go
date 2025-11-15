package avatarsdkgo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAvatarSessionInitSuccess(t *testing.T) {
	expireAt := time.Unix(1754824283, 0).UTC()

	var requestReceived bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true

		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}
		if r.URL.Path != sessionTokenPath {
			t.Fatalf("expected path %s, got %s", sessionTokenPath, r.URL.Path)
		}
		if apiKey := r.Header.Get("X-Api-Key"); apiKey != "api-key" {
			t.Fatalf("expected X-Api-Key header to be %s, got %s", "api-key", apiKey)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %s", contentType)
		}

		var payload sessionTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request payload: %v", err)
		}
		if payload.ExpireAt != expireAt.Unix() {
			t.Fatalf("expected expireAt %d, got %d", expireAt.Unix(), payload.ExpireAt)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sessionTokenResponse{SessionToken: "session-token-123"})
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAPIKey("api-key"),
		WithExpireAt(expireAt),
		WithConsoleEndpointURL(server.URL),
	)

	if err := session.Init(context.Background()); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	if !requestReceived {
		t.Fatal("expected Init to issue a request to the console endpoint")
	}
	if session.sessionToken != "session-token-123" {
		t.Fatalf("expected session token to be set, got %q", session.sessionToken)
	}
}

func TestAvatarSessionInitFailure(t *testing.T) {
	expireAt := time.Unix(1754824283, 0).UTC()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sessionTokenResponse{
			Errors: []struct {
				ID     string `json:"id"`
				Status int    `json:"status"`
				Code   string `json:"code"`
				Title  string `json:"title"`
				Detail string `json:"detail"`
			}{
				{
					ID:     "INVALID_ARGUMENT",
					Status: http.StatusUnauthorized,
					Code:   "INVALID_ARGUMENT",
					Title:  "Invalid Argument",
					Detail: "invalid api key",
				},
			},
		})
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAPIKey("bad-key"),
		WithExpireAt(expireAt),
		WithConsoleEndpointURL(server.URL),
	)

	err := session.Init(context.Background())
	if err == nil {
		t.Fatal("expected Init to return error for failed request")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("expected error message to include response detail, got %v", err)
	}
	if session.sessionToken != "" {
		t.Fatalf("expected session token to remain unset on failure, got %q", session.sessionToken)
	}
}

func TestAvatarSessionInitMissingConfig(t *testing.T) {
	session := NewAvatarSession()

	err := session.Init(context.Background())
	if err == nil {
		t.Fatal("expected Init to fail due to missing configuration")
	}
	if !strings.Contains(err.Error(), "missing API key") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}
