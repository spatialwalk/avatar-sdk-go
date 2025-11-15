package avatarsdkgo

import "time"

// SessionConfig captures the configuration used to build an AvatarSession.
type SessionConfig struct {
	AvatarID           string
	APIKey             string
	AppID              string
	ExpireAt           time.Time
	SampleRate         float64
	TransportFrames    func([]byte)
	OnError            func(error)
	OnClose            func()
	ConsoleEndpointURL string
	IngressEndpointURL string
}

// SessionOption applies a configuration change to SessionConfig.
type SessionOption func(*SessionConfig)

func defaultSessionConfig() *SessionConfig {
	return &SessionConfig{
		TransportFrames: func([]byte) {},
		OnError:         func(error) {},
		OnClose:         func() {},
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

// WithExpireAt sets the expiration time of the session.
func WithExpireAt(expireAt time.Time) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.ExpireAt = expireAt
	}
}

// WithSampleRate sets the audio sample rate used for the session.
func WithSampleRate(sampleRate float64) SessionOption {
	return func(cfg *SessionConfig) {
		cfg.SampleRate = sampleRate
	}
}

// WithTransportFrames registers a handler invoked when transport frames are emitted.
func WithTransportFrames(handler func([]byte)) SessionOption {
	return func(cfg *SessionConfig) {
		if handler != nil {
			cfg.TransportFrames = handler
		} else {
			cfg.TransportFrames = func([]byte) {}
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
