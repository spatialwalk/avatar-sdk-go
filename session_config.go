package avatarsdkgo

import "time"

// SessionConfig captures the configuration used to build an AvatarSession.
type SessionConfig struct {
	AvatarID           string
	APIKey             string
	AppID              string
	UseQueryAuth       bool // If true, send app/session credentials as URL query params (web-style auth). If false (default), send them as headers (mobile-style auth).
	ExpireAt           time.Time
	SampleRate         int
	Bitrate            int
	TransportFrames    func([]byte, bool)
	OnError            func(error)
	OnClose            func()
	ConsoleEndpointURL string
	IngressEndpointURL string
	LiveKitEgress      *LiveKitEgressConfig // If set, enables LiveKit egress mode - audio and animation are streamed to a LiveKit room via the egress service
}

// LiveKitEgressConfig contains configuration for streaming to a LiveKit room.
type LiveKitEgressConfig struct {
	// URL is the LiveKit server URL (e.g., wss://livekit.example.com)
	URL string
	// APIKey is the LiveKit API key
	APIKey string
	// APISecret is the LiveKit API secret
	APISecret string
	// RoomName is the LiveKit room name to join
	RoomName string
	// PublisherID is the publisher identity in the room
	PublisherID string
}

// SessionOption applies a configuration change to SessionConfig.
type SessionOption func(*SessionConfig)

func defaultSessionConfig() *SessionConfig {
	return &SessionConfig{
		TransportFrames: func([]byte, bool) {},
		OnError:         func(error) {},
		OnClose:         func() {},
		SampleRate:      16000,
		Bitrate:         0,
	}
}

// WithAvatarID sets the avatar identifier used for the session.
func WithAvatarID(avatarID string) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.AvatarID = avatarID
	}
}

// WithAPIKey sets the API key used for authenticating the session.
func WithAPIKey(apiKey string) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.APIKey = apiKey
	}
}

// WithAppID sets the application identifier associated with the session.
func WithAppID(appID string) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.AppID = appID
	}
}

// WithUseQueryAuth chooses whether websocket auth is sent via URL query params (web) or headers (mobile).
func WithUseQueryAuth(useQueryAuth bool) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.UseQueryAuth = useQueryAuth
	}
}

// WithExpireAt sets the expiration time of the session.
func WithExpireAt(expireAt time.Time) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.ExpireAt = expireAt
	}
}

// WithSampleRate sets the audio sample rate in Hz.
func WithSampleRate(sampleRate int) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.SampleRate = sampleRate
	}
}

// WithBitrate sets the audio bitrate (if applicable to the selected audio format).
func WithBitrate(bitrate int) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.Bitrate = bitrate
	}
}

// WithTransportFrames registers a handler invoked when transport frames are emitted.
func WithTransportFrames(handler func([]byte, bool)) SessionOption {
	return func(cfg *SessionConfig) {
		if handler != nil {
			cfg.TransportFrames = handler
		} else {
			cfg.TransportFrames = func([]byte, bool) {}
		}
	}
}

// WithOnError registers a handler that receives errors emitted by the session.
func WithOnError(handler func(error)) SessionOption {
	return func(cfg *SessionConfig) {
		if handler != nil {
			cfg.OnError = handler
		} else {
			cfg.OnError = func(error) {}
		}
	}
}

// WithOnClose registers a handler that is called when the session closes.
func WithOnClose(handler func()) SessionOption {
	return func(cfg *SessionConfig) {
		if handler != nil {
			cfg.OnClose = handler
		} else {
			cfg.OnClose = func() {}
		}
	}
}

// WithConsoleEndpointURL overrides the default console endpoint URL used by the session.
func WithConsoleEndpointURL(endpointURL string) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.ConsoleEndpointURL = endpointURL
	}
}

// WithIngressEndpointURL overrides the default ingress endpoint URL used by the session.
func WithIngressEndpointURL(endpointURL string) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.IngressEndpointURL = endpointURL
	}
}

// WithLiveKitEgress enables LiveKit egress mode for the session.
// When set, audio and animation data are streamed to a LiveKit room via the egress service
// instead of being returned through the WebSocket connection.
func WithLiveKitEgress(config *LiveKitEgressConfig) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.LiveKitEgress = config
	}
}
