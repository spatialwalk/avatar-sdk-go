package avatarsdkgo

import (
	"testing"
)

func TestAvatarSDKErrorError(t *testing.T) {
	err := &AvatarSDKError{
		Code:    ErrorCodeSessionTokenExpired,
		Message: "token has expired",
	}

	expected := "sessionTokenExpired: token has expired"
	if got := err.Error(); got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestNewAvatarSDKError(t *testing.T) {
	err := NewAvatarSDKError(ErrorCodeSessionTokenInvalid, "invalid token format")

	if err.Code != ErrorCodeSessionTokenInvalid {
		t.Fatalf("expected code %q, got %q", ErrorCodeSessionTokenInvalid, err.Code)
	}
	if err.Message != "invalid token format" {
		t.Fatalf("expected message %q, got %q", "invalid token format", err.Message)
	}
}

func TestMapWSConnectErrorToCode(t *testing.T) {
	tests := []struct {
		statusCode   int
		expectedCode *AvatarSDKErrorCode
	}{
		{401, ptr(ErrorCodeSessionTokenExpired)},
		{400, ptr(ErrorCodeSessionTokenInvalid)},
		{404, ptr(ErrorCodeAppIDUnrecognized)},
		{500, nil},
		{502, nil},
		{200, nil},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.statusCode)), func(t *testing.T) {
			got := mapWSConnectErrorToCode(tt.statusCode)
			if tt.expectedCode == nil {
				if got != nil {
					t.Fatalf("expected nil for status %d, got %v", tt.statusCode, *got)
				}
			} else {
				if got == nil {
					t.Fatalf("expected %v for status %d, got nil", *tt.expectedCode, tt.statusCode)
				}
				if *got != *tt.expectedCode {
					t.Fatalf("expected %v for status %d, got %v", *tt.expectedCode, tt.statusCode, *got)
				}
			}
		})
	}
}

func TestErrorCodeConstants(t *testing.T) {
	// Verify the string values of error code constants
	if ErrorCodeSessionTokenExpired != "sessionTokenExpired" {
		t.Fatalf("unexpected value for ErrorCodeSessionTokenExpired: %q", ErrorCodeSessionTokenExpired)
	}
	if ErrorCodeSessionTokenInvalid != "sessionTokenInvalid" {
		t.Fatalf("unexpected value for ErrorCodeSessionTokenInvalid: %q", ErrorCodeSessionTokenInvalid)
	}
	if ErrorCodeAppIDUnrecognized != "appIDUnrecognized" {
		t.Fatalf("unexpected value for ErrorCodeAppIDUnrecognized: %q", ErrorCodeAppIDUnrecognized)
	}
	if ErrorCodeInvalidRequest != "invalidRequest" {
		t.Fatalf("unexpected value for ErrorCodeInvalidRequest: %q", ErrorCodeInvalidRequest)
	}
	if ErrorCodeInvalidEgressConfig != "invalidEgressConfig" {
		t.Fatalf("unexpected value for ErrorCodeInvalidEgressConfig: %q", ErrorCodeInvalidEgressConfig)
	}
	if ErrorCodeEgressUnavailable != "egressUnavailable" {
		t.Fatalf("unexpected value for ErrorCodeEgressUnavailable: %q", ErrorCodeEgressUnavailable)
	}
	if ErrorCodeProtocolError != "protocolError" {
		t.Fatalf("unexpected value for ErrorCodeProtocolError: %q", ErrorCodeProtocolError)
	}
	if ErrorCodeUnknown != "unknown" {
		t.Fatalf("unexpected value for ErrorCodeUnknown: %q", ErrorCodeUnknown)
	}
}

func TestClassifyServerErrorCode(t *testing.T) {
	tests := []struct {
		name       string
		serverCode string
		detail     string
		want       AvatarSDKErrorCode
	}{
		{
			name:       "invalid argument egress",
			serverCode: "3",
			detail:     "livekit egress configuration is invalid",
			want:       ErrorCodeInvalidEgressConfig,
		},
		{
			name:       "invalid argument request",
			serverCode: "3",
			detail:     "request payload is invalid",
			want:       ErrorCodeInvalidRequest,
		},
		{
			name:       "unauthenticated token",
			serverCode: "16",
			detail:     "unauthorized: invalid token",
			want:       ErrorCodeInvalidEgressConfig,
		},
		{
			name:       "egress unavailable",
			serverCode: "14",
			detail:     "failed to create egress connection",
			want:       ErrorCodeEgressUnavailable,
		},
		{
			name:       "unknown",
			serverCode: "500",
			detail:     "internal server error",
			want:       ErrorCodeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyServerErrorCode(tt.serverCode, tt.detail)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func ptr(code AvatarSDKErrorCode) *AvatarSDKErrorCode {
	return &code
}
