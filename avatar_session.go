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
	"time"

	"github.com/gorilla/websocket"
	message "github.com/spatialwalk/avatar-sdk-go/proto/generated"
	"google.golang.org/protobuf/proto"
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
	currentReqID string
	connectionID string
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

// Start establishes WebSocket connection to the ingress endpoint and performs v2 handshake.
// Returns the connection ID for tracking this session.
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
	if cfg.AppID == "" {
		return "", errors.New("start avatar session: missing app ID")
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

	// v2 auth: mobile uses headers; web uses query params.
	headers := http.Header{}
	if cfg.UseQueryAuth {
		q.Set("appId", cfg.AppID)
		q.Set("sessionKey", s.sessionToken)
	} else {
		headers.Set("X-App-ID", cfg.AppID)
		headers.Set("X-Session-Key", s.sessionToken)
	}

	u.RawQuery = q.Encode()

	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		if resp != nil {
			// Map HTTP status to SDK error code
			if code := mapWSConnectErrorToCode(resp.StatusCode); code != nil {
				return "", NewAvatarSDKError(*code, fmt.Sprintf("WebSocket auth failed (HTTP %d)", resp.StatusCode))
			}
			if resp.Body != nil {
				defer resp.Body.Close() // nolint:errcheck
				if body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096)); readErr == nil && len(body) > 0 {
					return "", fmt.Errorf("start avatar session: dial websocket failed with code %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
				}
			}
		}
		return "", fmt.Errorf("start avatar session: dial websocket: %w", err)
	}

	s.conn = conn

	// v2 handshake:
	// 1) client sends ClientConfigureSession
	// 2) server responds with ServerConfirmSession (connection_id) OR ServerError
	if err := s.sendClientConfigureSession(); err != nil {
		_ = conn.Close()
		s.conn = nil
		return "", err
	}

	connectionID, err := s.awaitServerConfirmSession(ctx)
	if err != nil {
		_ = conn.Close()
		s.conn = nil
		return "", err
	}

	s.connectionID = connectionID

	// Start read loop in background
	go s.readLoop(ctx)

	return connectionID, nil
}

// sendClientConfigureSession sends the v2 handshake configuration message.
func (s *AvatarSession) sendClientConfigureSession() error {
	if s.conn == nil {
		return errors.New("websocket connection is not established")
	}

	clientConfig := &message.ClientConfigureSession{
		SampleRate:           int32(s.config.SampleRate),
		Bitrate:              int32(s.config.Bitrate),
		AudioFormat:          message.AudioFormat_AUDIO_FORMAT_PCM_S16LE,
		TransportCompression: message.TransportCompression_TRANSPORT_COMPRESSION_NONE,
	}

	// Add LiveKit egress configuration if provided
	if s.config.LiveKitEgress != nil {
		clientConfig.EgressType = message.EgressType_EGRESS_TYPE_LIVEKIT
		clientConfig.LivekitEgress = &message.LiveKitEgressConfig{
			Url:         s.config.LiveKitEgress.URL,
			ApiKey:      s.config.LiveKitEgress.APIKey,
			ApiSecret:   s.config.LiveKitEgress.APISecret,
			RoomName:    s.config.LiveKitEgress.RoomName,
			PublisherId: s.config.LiveKitEgress.PublisherID,
		}
	}

	msg := &message.Message{
		Type: message.MessageType_MESSAGE_CLIENT_CONFIGURE_SESSION,
		Data: &message.Message_ClientConfigureSession{
			ClientConfigureSession: clientConfig,
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("start avatar session: marshal configure session message: %w", err)
	}

	if err := s.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("start avatar session: send configure session message: %w", err)
	}

	return nil
}

// awaitServerConfirmSession waits for the server's handshake response.
func (s *AvatarSession) awaitServerConfirmSession(ctx context.Context) (string, error) {
	if s.conn == nil {
		return "", errors.New("websocket connection is not established")
	}

	// Set read deadline based on context
	if deadline, ok := ctx.Deadline(); ok {
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return "", fmt.Errorf("start avatar session: set read deadline: %w", err)
		}
		defer s.conn.SetReadDeadline(time.Time{}) // nolint:errcheck
	}

	messageType, payload, err := s.conn.ReadMessage()
	if err != nil {
		return "", fmt.Errorf("start avatar session: failed during websocket handshake: %w", err)
	}

	if messageType != websocket.BinaryMessage {
		return "", errors.New("start avatar session: failed during websocket handshake: expected binary protobuf message")
	}

	var envelope message.Message
	if err := proto.Unmarshal(payload, &envelope); err != nil {
		return "", fmt.Errorf("start avatar session: failed during websocket handshake: invalid protobuf payload: %w", err)
	}

	switch envelope.GetType() {
	case message.MessageType_MESSAGE_SERVER_CONFIRM_SESSION:
		confirm := envelope.GetServerConfirmSession()
		if confirm == nil || confirm.GetConnectionId() == "" {
			return "", errors.New("start avatar session: handshake succeeded but server_confirm_session.connection_id is empty")
		}
		return confirm.GetConnectionId(), nil

	case message.MessageType_MESSAGE_SERVER_ERROR:
		serverErr := envelope.GetServerError()
		if serverErr == nil {
			return "", errors.New("start avatar session: ServerError during handshake (missing payload)")
		}
		return "", fmt.Errorf("start avatar session: ServerError during handshake (connection_id=%s, req_id=%s, code=%d): %s",
			serverErr.GetConnectionId(), serverErr.GetReqId(), serverErr.GetCode(), serverErr.GetMessage())

	default:
		return "", fmt.Errorf("start avatar session: unexpected message during handshake: type=%v", envelope.GetType())
	}
}

// SendAudio sends audio data to the server.
// Currently supports 16kHz mono 16-bit PCM audio by default.
func (s *AvatarSession) SendAudio(audio []byte, end bool) (string, error) {
	if s.conn == nil {
		return "", errors.New("send audio: websocket connection is not established")
	}

	var err error
	if s.currentReqID == "" {
		s.currentReqID, err = GenerateLogID()
		if err != nil {
			return "", fmt.Errorf("send audio: generate request id: %w", err)
		}
	}

	reqID := s.currentReqID

	msg := &message.Message{
		Type: message.MessageType_MESSAGE_CLIENT_AUDIO_INPUT,
		Data: &message.Message_ClientAudioInput{
			ClientAudioInput: &message.ClientAudioInput{
				ReqId: reqID,
				Audio: audio,
				End:   end,
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("send audio: marshal message: %w", err)
	}

	if err := s.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return "", fmt.Errorf("send audio: write message: %w", err)
	}

	if end {
		s.currentReqID = ""
	}

	return reqID, nil
}

// Close closes the WebSocket connection and cleans up resources.
func (s *AvatarSession) Close() error {
	if s == nil {
		return nil
	}
	if s.conn != nil {
		err := s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			_ = s.conn.Close()
			s.conn = nil
			return fmt.Errorf("close avatar session: send close message: %w", err)
		}
		err = s.conn.Close()
		if err != nil {
			s.conn = nil
			return fmt.Errorf("close avatar session: close connection: %w", err)
		}
		s.conn = nil
	}
	if s.config != nil && s.config.OnClose != nil {
		go s.config.OnClose()
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

func (s *AvatarSession) readLoop(ctx context.Context) {
	if s == nil {
		return
	}

	conn := s.conn
	if conn == nil {
		return
	}

	cfg := s.config

	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx != nil && ctx.Err() != nil {
				return
			}

			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}

			if cfg != nil && cfg.OnError != nil {
				asyncErr := fmt.Errorf("avatar session read loop: read message: %w", err)
				go cfg.OnError(asyncErr)
			}

			_ = s.Close()
			return
		}

		if messageType != websocket.BinaryMessage {
			continue
		}

		var envelope message.Message
		if err := proto.Unmarshal(payload, &envelope); err != nil {
			if cfg != nil && cfg.OnError != nil {
				asyncErr := fmt.Errorf("avatar session read loop: decode message: %w", err)
				go cfg.OnError(asyncErr)
			}
			continue
		}

		switch envelope.GetType() {
		case message.MessageType_MESSAGE_SERVER_RESPONSE_ANIMATION:
			if cfg != nil && cfg.TransportFrames != nil {
				frame := append([]byte(nil), payload...)
				anim := envelope.GetServerResponseAnimation()
				last := anim != nil && anim.GetEnd()
				go cfg.TransportFrames(frame, last)
			}
		case message.MessageType_MESSAGE_SERVER_ERROR:
			if cfg != nil && cfg.OnError != nil {
				serverErr := envelope.GetServerError()
				if serverErr == nil {
					go cfg.OnError(errors.New("avatar session read loop: error message missing payload"))
					continue
				}
				report := fmt.Errorf("avatar session error (connection_id=%s, req_id=%s, code=%d): %s",
					serverErr.GetConnectionId(), serverErr.GetReqId(), serverErr.GetCode(), serverErr.GetMessage())
				go cfg.OnError(report)
			}
		}
	}
}
