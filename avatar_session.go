package avatarsdkgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

const (
	sessionTokenPath     = "/session-tokens"
	ingressWebSocketPath = "/websocket"
)

// AvatarSession represents an active avatar session configured via SessionOptions.
type AvatarSession struct {
	config       *SessionConfig
	sessionToken string
	conn         *websocket.Conn
}

// NewAvatarSession creates a new AvatarSession using the provided SessionOptions.
func NewAvatarSession(opts ...SessionOption) *AvatarSession {
	cfg := defaultSessionConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return &AvatarSession{config: cfg}
}

// Config returns a copy of the session configuration.
func (s *AvatarSession) Config() SessionConfig {
	if s == nil || s.config == nil {
		return SessionConfig{}
	}
	return *s.config
}

// Init exchanges configuration credentials for a session token against the console API.
func (s *AvatarSession) Init(ctx context.Context) error {
	if s == nil {
		return errors.New("init avatar session: session is nil")
	}
	if s.config == nil {
		return errors.New("init avatar session: session config is nil")
	}

	cfg := s.config
	if cfg.APIKey == "" {
		return errors.New("init avatar session: missing API key")
	}
	if cfg.ConsoleEndpointURL == "" {
		return errors.New("init avatar session: missing console endpoint URL")
	}
	if cfg.ExpireAt.IsZero() {
		return errors.New("init avatar session: missing expireAt")
	}

	endpoint := strings.TrimRight(cfg.ConsoleEndpointURL, "/") + sessionTokenPath

	payload := sessionTokenRequest{
		ExpireAt: cfg.ExpireAt.UTC().Unix(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("init avatar session: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("init avatar session: create request: %w", err)
	}
	req.Header.Set("X-Api-Key", cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("init avatar session: request session token: %w", err)
	}
	defer resp.Body.Close() // nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("init avatar session: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("init avatar session: request failed with status %d", resp.StatusCode)
	}

	var tokenResp sessionTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return fmt.Errorf("init avatar session: decode response: %w", err)
	}
	if len(tokenResp.Errors) > 0 {
		return fmt.Errorf("init avatar session: %s", formatSessionTokenError(resp.StatusCode, &tokenResp))
	}
	if tokenResp.SessionToken == "" {
		return errors.New("init avatar session: empty session token in response")
	}

	s.sessionToken = tokenResp.SessionToken
	return nil
}

func (s *AvatarSession) Start(ctx context.Context) (string, error) {
	if s == nil {
		return "", errors.New("start avatar session: session is nil")
	}
	if s.config == nil {
		return "", errors.New("start avatar session: session config is nil")
	}
	if s.conn != nil {
		return "", errors.New("start avatar session: session already started")
	}
	if s.sessionToken == "" {
		return "", errors.New("start avatar session: session not initialized")
	}

	cfg := s.config
	if cfg.IngressEndpointURL == "" {
		return "", errors.New("start avatar session: missing ingress endpoint URL")
	}
	if cfg.AvatarID == "" {
		return "", errors.New("start avatar session: missing avatar ID")
	}

	endpoint := strings.TrimRight(cfg.IngressEndpointURL, "/") + ingressWebSocketPath

	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("start avatar session: parse ingress endpoint: %w", err)
	}

	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already websocket scheme
	case "":
		return "", errors.New("start avatar session: ingress endpoint scheme missing")
	default:
		return "", fmt.Errorf("start avatar session: unsupported scheme %q", u.Scheme)
	}

	q := u.Query()
	q.Set("id", cfg.AvatarID)
	u.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("X-Session-Key", s.sessionToken)

	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close() // nolint:errcheck
			if body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096)); readErr == nil && len(body) > 0 {
				return "", fmt.Errorf("start avatar session: dial websocket failed with code %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
		}
		return "", fmt.Errorf("start avatar session: dial websocket: %w", err)
	}

	s.conn = conn
	// TODO: generate connection id
	return "", nil
}

func (s *AvatarSession) SendAudio(audio []byte, end bool) (string, error) {
	return "", nil
}

func (s *AvatarSession) Close() error {
	if s == nil {
		return nil
	}
	if s.conn != nil {
		_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = s.conn.Close()
		s.conn = nil
	}
	return nil
}

type sessionTokenRequest struct {
	ExpireAt     int64  `json:"expireAt"`
	ModelVersion string `json:"modelVersion,omitempty"`
}

type sessionTokenResponse struct {
	SessionToken string `json:"sessionToken"`
	Errors       []struct {
		ID     string `json:"id"`
		Status int    `json:"status"`
		Code   string `json:"code"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

func formatSessionTokenError(status int, resp *sessionTokenResponse) string {
	// format resp.Errors[0] as "Error <status> (<code>): <title> - <detail>"
	if len(resp.Errors) == 0 {
		return fmt.Sprintf("unknown error with status %d", status)
	}
	err := resp.Errors[0]
	return fmt.Sprintf("Error %d (%s): %s - %s", err.Status, err.Code, err.Title, err.Detail)
}
