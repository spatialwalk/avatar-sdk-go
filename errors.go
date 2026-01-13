package avatarsdkgo

import "fmt"

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
	// ErrorCodeUnknown indicates an unknown error.
	ErrorCodeUnknown AvatarSDKErrorCode = "unknown"
)

// AvatarSDKError is an SDK error with a stable error code.
type AvatarSDKError struct {
	Code    AvatarSDKErrorCode
	Message string
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
