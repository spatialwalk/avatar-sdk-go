package avatarsdkgo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	message "github.com/spatialwalk/avatar-sdk-go/proto/generated"
	"google.golang.org/protobuf/proto"
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

func TestAvatarSessionStartSuccess(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var receivedAvatarID string
	var receivedSessionKey string
	var receivedAppID string
	var serverConn *websocket.Conn

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ingressWebSocketPath {
			t.Fatalf("expected websocket path %s, got %s", ingressWebSocketPath, r.URL.Path)
		}
		receivedAvatarID = r.URL.Query().Get("id")
		receivedSessionKey = r.Header.Get("X-Session-Key")
		receivedAppID = r.Header.Get("X-App-ID")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("failed to upgrade connection: %v", err)
		}
		serverConn = conn

		// v2 handshake: read ClientConfigureSession, send ServerConfirmSession
		go func() {
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if messageType != websocket.BinaryMessage {
				return
			}

			var envelope message.Message
			if err := proto.Unmarshal(payload, &envelope); err != nil {
				return
			}

			if envelope.GetType() != message.MessageType_MESSAGE_CLIENT_CONFIGURE_SESSION {
				return
			}

			// Send ServerConfirmSession
			confirmMsg := &message.Message{
				Type: message.MessageType_MESSAGE_SERVER_CONFIRM_SESSION,
				Data: &message.Message_ServerConfirmSession{
					ServerConfirmSession: &message.ServerConfirmSession{
						ConnectionId: "conn-id-123",
					},
				},
			}
			confirmData, _ := proto.Marshal(confirmMsg)
			_ = conn.WriteMessage(websocket.BinaryMessage, confirmData)
		}()
	}))
	defer server.Close()
	defer func() {
		if serverConn != nil {
			_ = serverConn.Close()
		}
	}()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)

	session.sessionToken = "session-token-123"

	connectionID, err := session.Start(context.Background())
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if receivedAvatarID != "avatar-123" {
		t.Fatalf("expected avatar id to be sent, got %q", receivedAvatarID)
	}
	if receivedSessionKey != "session-token-123" {
		t.Fatalf("expected X-Session-Key header, got %q", receivedSessionKey)
	}
	if receivedAppID != "app-123" {
		t.Fatalf("expected X-App-ID header, got %q", receivedAppID)
	}
	if connectionID != "conn-id-123" {
		t.Fatalf("expected connection ID from handshake, got %q", connectionID)
	}
	if session.conn == nil {
		t.Fatal("expected websocket connection to be established")
	}

	if err := session.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if session.conn != nil {
		t.Fatal("expected connection to be cleared after Close")
	}
}

func TestAvatarSessionStartMissingToken(t *testing.T) {
	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL("wss://example.com"),
	)

	_, err := session.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session not initialized") {
		t.Fatalf("expected session not initialized error, got %v", err)
	}
}

func TestAvatarSessionStartDialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "session-token-123"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return error on dial failure")
	}
	// v2 maps 401 to sessionTokenExpired error code
	if !strings.Contains(err.Error(), "sessionTokenExpired") {
		t.Fatalf("expected error to include sessionTokenExpired code, got %v", err)
	}
}

func TestAvatarSessionStartMissingAppID(t *testing.T) {
	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithIngressEndpointURL("wss://example.com"),
	)
	session.sessionToken = "session-token-123"

	_, err := session.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing app ID") {
		t.Fatalf("expected missing app ID error, got %v", err)
	}
}

func TestAvatarSessionConfigNilSession(t *testing.T) {
	var session *AvatarSession
	cfg := session.Config()
	if cfg.AvatarID != "" {
		t.Fatal("expected empty config for nil session")
	}
}

func TestAvatarSessionConfigNilConfig(t *testing.T) {
	session := &AvatarSession{config: nil}
	cfg := session.Config()
	if cfg.AvatarID != "" {
		t.Fatal("expected empty config for nil config")
	}
}

func TestAvatarSessionInitNilSession(t *testing.T) {
	var session *AvatarSession
	err := session.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session is nil") {
		t.Fatalf("expected session is nil error, got %v", err)
	}
}

func TestAvatarSessionInitNilConfig(t *testing.T) {
	session := &AvatarSession{config: nil}
	err := session.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session config is nil") {
		t.Fatalf("expected session config is nil error, got %v", err)
	}
}

func TestAvatarSessionInitMissingConsoleEndpoint(t *testing.T) {
	session := NewAvatarSession(
		WithAPIKey("api-key"),
		WithExpireAt(time.Now().Add(5*time.Minute)),
	)
	err := session.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing console endpoint URL") {
		t.Fatalf("expected missing console endpoint URL error, got %v", err)
	}
}

func TestAvatarSessionInitMissingExpireAt(t *testing.T) {
	session := NewAvatarSession(
		WithAPIKey("api-key"),
		WithConsoleEndpointURL("https://console.test"),
	)
	err := session.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing expireAt") {
		t.Fatalf("expected missing expireAt error, got %v", err)
	}
}

func TestAvatarSessionInitNon200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAPIKey("api-key"),
		WithExpireAt(time.Now().Add(5*time.Minute)),
		WithConsoleEndpointURL(server.URL),
	)

	err := session.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "request failed with status 500") {
		t.Fatalf("expected status 500 error, got %v", err)
	}
}

func TestAvatarSessionInitInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAPIKey("api-key"),
		WithExpireAt(time.Now().Add(5*time.Minute)),
		WithConsoleEndpointURL(server.URL),
	)

	err := session.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected decode response error, got %v", err)
	}
}

func TestAvatarSessionInitEmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sessionTokenResponse{SessionToken: ""})
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAPIKey("api-key"),
		WithExpireAt(time.Now().Add(5*time.Minute)),
		WithConsoleEndpointURL(server.URL),
	)

	err := session.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "empty session token in response") {
		t.Fatalf("expected empty session token error, got %v", err)
	}
}

func TestAvatarSessionStartNilSession(t *testing.T) {
	var session *AvatarSession
	_, err := session.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session is nil") {
		t.Fatalf("expected session is nil error, got %v", err)
	}
}

func TestAvatarSessionStartNilConfig(t *testing.T) {
	session := &AvatarSession{config: nil}
	session.sessionToken = "token"
	_, err := session.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session config is nil") {
		t.Fatalf("expected session config is nil error, got %v", err)
	}
}

func TestAvatarSessionStartAlreadyStarted(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		select {}
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http", "ws", 1)
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL("wss://example.com"),
	)
	session.sessionToken = "token"
	session.conn = conn

	_, err := session.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session already started") {
		t.Fatalf("expected session already started error, got %v", err)
	}
}

func TestAvatarSessionStartMissingIngressEndpoint(t *testing.T) {
	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing ingress endpoint URL") {
		t.Fatalf("expected missing ingress endpoint URL error, got %v", err)
	}
}

func TestAvatarSessionStartMissingAvatarID(t *testing.T) {
	session := NewAvatarSession(
		WithAppID("app-123"),
		WithIngressEndpointURL("wss://example.com"),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing avatar ID") {
		t.Fatalf("expected missing avatar ID error, got %v", err)
	}
}

func TestAvatarSessionStartSchemeVariations(t *testing.T) {
	tests := []struct {
		scheme      string
		expectError bool
		errorMsg    string
	}{
		{"http://example.com", false, ""},
		{"https://example.com", false, ""},
		{"ws://example.com", false, ""},
		{"wss://example.com", false, ""},
		{"example.com", true, "scheme missing"},
		{"ftp://example.com", true, "unsupported scheme"},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			session := NewAvatarSession(
				WithAvatarID("avatar-123"),
				WithAppID("app-123"),
				WithIngressEndpointURL(tt.scheme),
			)
			session.sessionToken = "token"

			_, err := session.Start(context.Background())
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errorMsg)
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Fatalf("expected error containing %q, got %v", tt.errorMsg, err)
				}
			}
			// Note: for valid schemes, we'll get dial errors, which is expected
		})
	}
}

func TestAvatarSessionStartWithQueryAuth(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var receivedAppId string
	var receivedSessionKey string
	var serverConn *websocket.Conn

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAppId = r.URL.Query().Get("appId")
		receivedSessionKey = r.URL.Query().Get("sessionKey")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn = conn

		// v2 handshake
		go func() {
			messageType, _, err := conn.ReadMessage()
			if err != nil || messageType != websocket.BinaryMessage {
				return
			}

			confirmMsg := &message.Message{
				Type: message.MessageType_MESSAGE_SERVER_CONFIRM_SESSION,
				Data: &message.Message_ServerConfirmSession{
					ServerConfirmSession: &message.ServerConfirmSession{
						ConnectionId: "conn-id-query-auth",
					},
				},
			}
			confirmData, _ := proto.Marshal(confirmMsg)
			_ = conn.WriteMessage(websocket.BinaryMessage, confirmData)
		}()
	}))
	defer server.Close()
	defer func() {
		if serverConn != nil {
			_ = serverConn.Close()
		}
	}()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithUseQueryAuth(true),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "session-token-123"

	connectionID, err := session.Start(context.Background())
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if receivedAppId != "app-123" {
		t.Fatalf("expected appId query param, got %q", receivedAppId)
	}
	if receivedSessionKey != "session-token-123" {
		t.Fatalf("expected sessionKey query param, got %q", receivedSessionKey)
	}
	if connectionID != "conn-id-query-auth" {
		t.Fatalf("expected connection ID, got %q", connectionID)
	}

	_ = session.Close()
}

func TestAvatarSessionStartHandshakeServerError(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read ClientConfigureSession
		_, _, _ = conn.ReadMessage()

		// Send ServerError
		errMsg := &message.Message{
			Type: message.MessageType_MESSAGE_SERVER_ERROR,
			Data: &message.Message_ServerError{
				ServerError: &message.ServerError{
					ConnectionId: "conn-123",
					ReqId:        "req-456",
					Code:         400,
					Message:      "invalid configuration",
				},
			},
		}
		errData, _ := proto.Marshal(errMsg)
		_ = conn.WriteMessage(websocket.BinaryMessage, errData)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from ServerError response")
	}
	if !strings.Contains(err.Error(), "ServerError during handshake") {
		t.Fatalf("expected ServerError during handshake, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("expected error message, got %v", err)
	}
}

func TestAvatarSessionStartHandshakeUnexpectedMessageType(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read ClientConfigureSession
		_, _, _ = conn.ReadMessage()

		// Send unexpected message type (e.g., MESSAGE_CLIENT_AUDIO_INPUT)
		unexpectedMsg := &message.Message{
			Type: message.MessageType_MESSAGE_CLIENT_AUDIO_INPUT,
		}
		unexpectedData, _ := proto.Marshal(unexpectedMsg)
		_ = conn.WriteMessage(websocket.BinaryMessage, unexpectedData)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from unexpected message type")
	}
	if !strings.Contains(err.Error(), "unexpected message during handshake") {
		t.Fatalf("expected unexpected message error, got %v", err)
	}
}

func TestAvatarSessionStartHandshakeTextMessage(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read ClientConfigureSession
		_, _, _ = conn.ReadMessage()

		// Send text message instead of binary
		_ = conn.WriteMessage(websocket.TextMessage, []byte("not binary"))
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from text message")
	}
	if !strings.Contains(err.Error(), "expected binary protobuf message") {
		t.Fatalf("expected binary protobuf message error, got %v", err)
	}
}

func TestAvatarSessionStartHandshakeInvalidProtobuf(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read ClientConfigureSession
		_, _, _ = conn.ReadMessage()

		// Send invalid protobuf
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte("not valid protobuf"))
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from invalid protobuf")
	}
	if !strings.Contains(err.Error(), "invalid protobuf payload") {
		t.Fatalf("expected invalid protobuf payload error, got %v", err)
	}
}

func TestAvatarSessionStartHandshakeEmptyConnectionId(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read ClientConfigureSession
		_, _, _ = conn.ReadMessage()

		// Send ServerConfirmSession with empty connection_id
		confirmMsg := &message.Message{
			Type: message.MessageType_MESSAGE_SERVER_CONFIRM_SESSION,
			Data: &message.Message_ServerConfirmSession{
				ServerConfirmSession: &message.ServerConfirmSession{
					ConnectionId: "",
				},
			},
		}
		confirmData, _ := proto.Marshal(confirmMsg)
		_ = conn.WriteMessage(websocket.BinaryMessage, confirmData)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from empty connection_id")
	}
	if !strings.Contains(err.Error(), "connection_id is empty") {
		t.Fatalf("expected empty connection_id error, got %v", err)
	}
}

func TestAvatarSessionCloseNilSession(t *testing.T) {
	var session *AvatarSession
	err := session.Close()
	if err != nil {
		t.Fatalf("expected nil error for nil session, got %v", err)
	}
}

func TestAvatarSessionCloseWithNoConnection(t *testing.T) {
	session := NewAvatarSession()
	err := session.Close()
	if err != nil {
		t.Fatalf("expected nil error for session without connection, got %v", err)
	}
}

func TestAvatarSessionSendAudioNoConnection(t *testing.T) {
	session := NewAvatarSession()
	_, err := session.SendAudio([]byte{0x01}, true)
	if err == nil || !strings.Contains(err.Error(), "websocket connection is not established") {
		t.Fatalf("expected websocket connection error, got %v", err)
	}
}

func TestFormatSessionTokenErrorEmpty(t *testing.T) {
	resp := &sessionTokenResponse{Errors: nil}
	result := formatSessionTokenError(500, resp)
	if !strings.Contains(result, "unknown error with status 500") {
		t.Fatalf("expected unknown error message, got %q", result)
	}
}

func TestReadLoopServerError(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	errorReceived := make(chan error, 1)
	handshakeComplete := make(chan struct{})
	serverDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Read ClientConfigureSession
		_, _, _ = conn.ReadMessage()

		// Send ServerConfirmSession
		confirmMsg := &message.Message{
			Type: message.MessageType_MESSAGE_SERVER_CONFIRM_SESSION,
			Data: &message.Message_ServerConfirmSession{
				ServerConfirmSession: &message.ServerConfirmSession{
					ConnectionId: "conn-123",
				},
			},
		}
		confirmData, _ := proto.Marshal(confirmMsg)
		_ = conn.WriteMessage(websocket.BinaryMessage, confirmData)

		// Wait for handshake to complete before sending more messages
		<-handshakeComplete

		// Send ServerError
		errMsg := &message.Message{
			Type: message.MessageType_MESSAGE_SERVER_ERROR,
			Data: &message.Message_ServerError{
				ServerError: &message.ServerError{
					ConnectionId: "conn-123",
					ReqId:        "req-456",
					Code:         500,
					Message:      "internal server error",
				},
			},
		}
		errData, _ := proto.Marshal(errMsg)
		_ = conn.WriteMessage(websocket.BinaryMessage, errData)

		// Close from server side
		_ = conn.Close()
		close(serverDone)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
		WithOnError(func(err error) {
			select {
			case errorReceived <- err:
			default:
			}
		}),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Signal that handshake is complete
	close(handshakeComplete)

	select {
	case err := <-errorReceived:
		if !strings.Contains(err.Error(), "internal server error") {
			t.Fatalf("expected error to contain message, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error callback")
	}

	// Wait for server to close connection
	<-serverDone
}

func TestReadLoopAnimationFrame(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	framesReceived := make(chan struct {
		data []byte
		last bool
	}, 2)
	handshakeComplete := make(chan struct{})
	serverDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Read ClientConfigureSession
		_, _, _ = conn.ReadMessage()

		// Send ServerConfirmSession
		confirmMsg := &message.Message{
			Type: message.MessageType_MESSAGE_SERVER_CONFIRM_SESSION,
			Data: &message.Message_ServerConfirmSession{
				ServerConfirmSession: &message.ServerConfirmSession{
					ConnectionId: "conn-123",
				},
			},
		}
		confirmData, _ := proto.Marshal(confirmMsg)
		_ = conn.WriteMessage(websocket.BinaryMessage, confirmData)

		// Wait for handshake to complete before sending more messages
		<-handshakeComplete

		// Send animation frames
		for i := 0; i < 2; i++ {
			animMsg := &message.Message{
				Type: message.MessageType_MESSAGE_SERVER_RESPONSE_ANIMATION,
				Data: &message.Message_ServerResponseAnimation{
					ServerResponseAnimation: &message.ServerResponseAnimation{
						ConnectionId: "conn-123",
						ReqId:        "req-456",
						End:          i == 1,
					},
				},
			}
			animData, _ := proto.Marshal(animMsg)
			_ = conn.WriteMessage(websocket.BinaryMessage, animData)
		}

		// Close from server side to trigger clean exit of read loop
		_ = conn.Close()
		close(serverDone)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
		WithTransportFrames(func(data []byte, last bool) {
			framesReceived <- struct {
				data []byte
				last bool
			}{data: data, last: last}
		}),
	)
	session.sessionToken = "token"

	_, err := session.Start(context.Background())
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Signal that handshake is complete
	close(handshakeComplete)

	// Wait for both frames
	for i := 0; i < 2; i++ {
		select {
		case frame := <-framesReceived:
			if i == 1 && !frame.last {
				t.Fatal("expected last frame to have last=true")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for animation frame")
		}
	}

	// Wait for server to close connection
	<-serverDone
}

func TestAvatarSessionStartDial400Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "session-token-123"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return error on dial failure")
	}
	if !strings.Contains(err.Error(), "sessionTokenInvalid") {
		t.Fatalf("expected error to include sessionTokenInvalid code, got %v", err)
	}
}

func TestAvatarSessionStartDial404Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	session := NewAvatarSession(
		WithAvatarID("avatar-123"),
		WithAppID("app-123"),
		WithIngressEndpointURL(strings.Replace(server.URL, "http", "ws", 1)),
	)
	session.sessionToken = "session-token-123"

	_, err := session.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return error on dial failure")
	}
	if !strings.Contains(err.Error(), "appIDUnrecognized") {
		t.Fatalf("expected error to include appIDUnrecognized code, got %v", err)
	}
}

func TestReqIDGeneration(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	serverConnCh := make(chan *websocket.Conn, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("failed to upgrade connection: %v", err)
		}
		serverConnCh <- conn
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http", "ws", 1)

	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket server: %v", err)
	}
	defer clientConn.Close() // nolint:errcheck

	session := NewAvatarSession()
	session.conn = clientConn
	defer func() {
		if err := session.Close(); err != nil {
			t.Fatalf("failed to close session: %v", err)
		}
	}()

	serverConn := <-serverConnCh
	defer serverConn.Close() // nolint:errcheck

	reqIDs := make(chan string, 8)
	go func() {
		for {
			messageType, payload, err := serverConn.ReadMessage()
			if err != nil {
				return
			}
			if messageType != websocket.BinaryMessage {
				continue
			}

			var envelope message.Message
			if err := proto.Unmarshal(payload, &envelope); err != nil {
				continue
			}

			input := envelope.GetClientAudioInput()
			if input == nil {
				continue
			}

			reqIDs <- input.GetReqId()
		}
	}()

	waitForReqID := func() string {
		select {
		case reqID := <-reqIDs:
			return reqID
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for req id")
		}
		return ""
	}

	firstChunk := []byte{0x01, 0x02, 0x03, 0x04}
	firstReqID, err := session.SendAudio(firstChunk, false)
	if err != nil {
		t.Fatalf("SendAudio returned error for first chunk: %v", err)
	}
	if firstReqID == "" {
		t.Fatal("expected first chunk to return a req id")
	}
	if received := waitForReqID(); received != firstReqID {
		t.Fatalf("expected server to receive req id %q, got %q", firstReqID, received)
	}

	secondChunk := []byte{0x05, 0x06}
	secondReqID, err := session.SendAudio(secondChunk, true)
	if err != nil {
		t.Fatalf("SendAudio returned error for second chunk: %v", err)
	}
	if secondReqID != firstReqID {
		t.Fatalf("expected second chunk to reuse req id %q, got %q", firstReqID, secondReqID)
	}
	if received := waitForReqID(); received != firstReqID {
		t.Fatalf("expected server to receive req id %q for second chunk, got %q", firstReqID, received)
	}

	thirdChunk := []byte{0x07, 0x08, 0x09}
	thirdReqID, err := session.SendAudio(thirdChunk, false)
	if err != nil {
		t.Fatalf("SendAudio returned error for third chunk: %v", err)
	}
	if thirdReqID == "" {
		t.Fatal("expected third chunk to return a req id")
	}
	if thirdReqID == firstReqID {
		t.Fatalf("expected third chunk to have a new req id distinct from %q", firstReqID)
	}
	if received := waitForReqID(); received != thirdReqID {
		t.Fatalf("expected server to receive req id %q for third chunk, got %q", thirdReqID, received)
	}

	fourthChunk := []byte{0x0A, 0x0B}
	fourthReqID, err := session.SendAudio(fourthChunk, true)
	if err != nil {
		t.Fatalf("SendAudio returned error for fourth chunk: %v", err)
	}
	if fourthReqID != thirdReqID {
		t.Fatalf("expected fourth chunk to reuse req id %q, got %q", thirdReqID, fourthReqID)
	}
	if received := waitForReqID(); received != thirdReqID {
		t.Fatalf("expected server to receive req id %q for fourth chunk, got %q", thirdReqID, received)
	}
}
