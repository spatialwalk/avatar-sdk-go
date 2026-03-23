package avatarsdkgo

import (
	"fmt"
	"strings"
)

// AvatarSDKErrorCode represents stable error codes surfaced by the SDK.
// These codes are referenced by the v2 websocket API documentation.
type AvatarSDKErrorCode string

const (
	// ErrorCodeSessionTokenExpired indicates the session token has expired.
	ErrorCodeSessionTokenExpired AvatarSDKErrorCode = "sessionTokenExpired"
	// ErrorCodeSessionTokenInvalid indicates the session token is invalid.
	ErrorCodeSessionTokenInvalid AvatarSDKErrorCode = "sessionTokenInvalid"
	// ErrorCodeAppIDUnrecognized indicates the app ID is not recognized.
	ErrorCodeAppIDUnrecognized AvatarSDKErrorCode = "appIDUnrecognized"
	// ErrorCodeInvalidRequest indicates the request payload is invalid.
	ErrorCodeInvalidRequest AvatarSDKErrorCode = "invalidRequest"
	// ErrorCodeInvalidEgressConfig indicates the egress configuration is invalid.
	ErrorCodeInvalidEgressConfig AvatarSDKErrorCode = "invalidEgressConfig"
	// ErrorCodeEgressUnavailable indicates the egress service is unavailable.
	ErrorCodeEgressUnavailable AvatarSDKErrorCode = "egressUnavailable"
	// ErrorCodeProtocolError indicates the websocket protocol exchange was invalid.
	ErrorCodeProtocolError AvatarSDKErrorCode = "protocolError"
	// ErrorCodeUnknown indicates an unknown error.
	ErrorCodeUnknown AvatarSDKErrorCode = "unknown"
)

// AvatarSDKError is an SDK error with a stable error code.
type AvatarSDKError struct {
	Code         AvatarSDKErrorCode
	Message      string
	Phase        string
	ServerCode   string
	ConnectionID string
	ReqID        string
}

// Error implements the error interface.
func (e *AvatarSDKError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewAvatarSDKError creates a new AvatarSDKError.
func NewAvatarSDKError(code AvatarSDKErrorCode, message string) *AvatarSDKError {
	return &AvatarSDKError{
		Code:    code,
		Message: message,
	}
}

// mapWSConnectErrorToCode maps websocket HTTP upgrade failures to stable SDK error codes.
// v2 spec mapping:
// - 401 -> sessionTokenExpired
// - 400 -> sessionTokenInvalid
// - 404 -> appIDUnrecognized
func mapWSConnectErrorToCode(statusCode int) *AvatarSDKErrorCode {
	switch statusCode {
	case 401:
		code := ErrorCodeSessionTokenExpired
		return &code
	case 400:
		code := ErrorCodeSessionTokenInvalid
		return &code
	case 404:
		code := ErrorCodeAppIDUnrecognized
		return &code
	default:
		return nil
	}
}

func classifyServerErrorCode(serverCode string, detail string) AvatarSDKErrorCode {
	detailText := normalizeErrorText(serverCode, detail)

	switch serverCode {
	case "3", "400":
		if strings.Contains(detailText, "livekit") || strings.Contains(detailText, "agora") || strings.Contains(detailText, "egress") {
			return ErrorCodeInvalidEgressConfig
		}
		return ErrorCodeInvalidRequest
	case "14":
		return ErrorCodeEgressUnavailable
	case "16":
		return ErrorCodeInvalidEgressConfig
	}

	if strings.Contains(detailText, "livekit_egress") || strings.Contains(detailText, "agora_egress") {
		return ErrorCodeInvalidEgressConfig
	}
	if strings.Contains(detailText, "missing livekit credentials") ||
		strings.Contains(detailText, "provide api_token or both api_key and api_secret") ||
		strings.Contains(detailText, "unauthorized") {
		return ErrorCodeInvalidEgressConfig
	}
	if strings.Contains(detailText, "egress client is not configured on server") ||
		strings.Contains(detailText, "failed to create egress connection") {
		return ErrorCodeEgressUnavailable
	}

	return ErrorCodeUnknown
}

func newServerAvatarSDKError(phase string, serverCode int32, detail string, connectionID string, reqID string) *AvatarSDKError {
	code := classifyServerErrorCode(fmt.Sprintf("%d", serverCode), detail)
	if code == ErrorCodeUnknown {
		code = ErrorCodeUnknown
	}

	return &AvatarSDKError{
		Code:         code,
		Message:      detail,
		Phase:        phase,
		ServerCode:   fmt.Sprintf("%d", serverCode),
		ConnectionID: connectionID,
		ReqID:        reqID,
	}
}

func normalizeErrorText(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(strings.ToLower(value))
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " | ")
}
