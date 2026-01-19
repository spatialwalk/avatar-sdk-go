# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go SDK for connecting to an avatar service via WebSocket. Sends PCM audio input and receives animation frame data through protocol buffer messaging.

## Build & Test Commands

```bash
# Run all tests
go test ./...

# Run tests with coverage (used in CI)
go test ./... -covermode=atomic -coverprofile=coverage.out

# Regenerate protobuf code (requires buf CLI)
cd proto && buf generate
```

## Architecture

### Session Lifecycle

1. **NewAvatarSession(opts...)** - Create session with functional options
2. **Init(ctx)** - HTTP POST to console endpoint to get session token
3. **Start(ctx)** - Dial WebSocket, perform v2 handshake (ClientConfigureSession → ServerConfirmSession)
4. **SendAudio(audio, end)** - Send PCM audio chunks; `end=true` marks stream end
5. **Close()** - Graceful WebSocket close, triggers OnClose callback

### Key Files

- `avatar_session.go` - Core session implementation (Init, Start, SendAudio, Close, readLoop)
- `session_config.go` - SessionConfig struct and functional options (WithAPIKey, WithAvatarID, etc.)
- `errors.go` - AvatarSDKError type with stable error codes
- `proto/message.proto` - Protocol buffer message definitions
- `proto/generated/` - Auto-generated protobuf Go code

### Message Types (proto/message.proto)

- `MESSAGE_CLIENT_CONFIGURE_SESSION` - Handshake: client sends audio config
- `MESSAGE_SERVER_CONFIRM_SESSION` - Handshake: server returns connection_id
- `MESSAGE_CLIENT_AUDIO_INPUT` - Client sends audio chunks with req_id
- `MESSAGE_SERVER_RESPONSE_ANIMATION` - Server sends animation frames
- `MESSAGE_SERVER_ERROR` - Server error with connection_id, req_id, code, message

### Async Callbacks

The `readLoop()` goroutine processes incoming messages and invokes:
- `TransportFrames(payload []byte, isLast bool)` - Animation frame received
- `OnError(error)` - Server error or connection error
- `OnClose()` - Session closed

Callbacks execute in separate goroutines to avoid blocking the read loop.

### WebSocket Auth Modes

- **Mobile (default)**: `X-App-ID` and `X-Session-Key` headers
- **Web**: `appId` and `sessionKey` query params (enable with `WithUseQueryAuth(true)`)

## Environment Variables (for examples)

```bash
AVATAR_API_KEY              # Console API key
AVATAR_APP_ID               # Application identifier
AVATAR_CONSOLE_ENDPOINT     # Console API URL for session tokens
AVATAR_INGRESS_ENDPOINT     # WebSocket ingress URL
AVATAR_SESSION_AVATAR_ID    # Avatar to use
```
