package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	avatarsdkgo "github.com/spatialwalk/avatar-sdk-go"
)

const (
	audioFilePath     = "./audio.pcm"
	defaultListenAddr = ":8080"
	requestTimeout    = 45 * time.Second
	sessionTTL        = 2 * time.Minute
)

type serverConfig struct {
	APIKey     string
	ConsoleURL string
	IngressURL string
	AvatarID   string
	ListenAddr string
}

type mediaServer struct {
	cfg   *serverConfig
	audio []byte
}

type animationCollector struct {
	mu     sync.Mutex
	frames [][]byte
	last   bool
	err    error
	once   sync.Once
	done   chan struct{}
}

type mediaResponse struct {
	Audio      []byte   `json:"audio"`
	Animations [][]byte `json:"animations"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	audio, err := loadAudio(audioFilePath)
	if err != nil {
		log.Fatalf("audio fixture error: %v", err)
	}

	server := &mediaServer{
		cfg:   cfg,
		audio: audio,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/media", server.handleMedia)

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func loadConfig() (*serverConfig, error) {
	cfg := &serverConfig{
		APIKey:     strings.TrimSpace(os.Getenv("AVATAR_API_KEY")),
		ConsoleURL: strings.TrimSpace(os.Getenv("AVATAR_CONSOLE_ENDPOINT")),
		IngressURL: strings.TrimSpace(os.Getenv("AVATAR_INGRESS_ENDPOINT")),
		AvatarID:   strings.TrimSpace(os.Getenv("AVATAR_SESSION_AVATAR_ID")),
		ListenAddr: strings.TrimSpace(os.Getenv("LISTEN_ADDR")),
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = defaultListenAddr
	}

	var missing []string
	if cfg.APIKey == "" {
		missing = append(missing, "AVATAR_API_KEY")
	}
	if cfg.ConsoleURL == "" {
		missing = append(missing, "AVATAR_CONSOLE_ENDPOINT")
	}
	if cfg.IngressURL == "" {
		missing = append(missing, "AVATAR_INGRESS_ENDPOINT")
	}
	if cfg.AvatarID == "" {
		missing = append(missing, "AVATAR_SESSION_AVATAR_ID")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

func loadAudio(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read audio file %q: %w", path, err)
	}
	return data, nil
}

func (s *mediaServer) handleMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	collector := newAnimationCollector()
	session := avatarsdkgo.NewAvatarSession(
		avatarsdkgo.WithAPIKey(s.cfg.APIKey),
		avatarsdkgo.WithConsoleEndpointURL(s.cfg.ConsoleURL),
		avatarsdkgo.WithIngressEndpointURL(s.cfg.IngressURL),
		avatarsdkgo.WithAvatarID(s.cfg.AvatarID),
		avatarsdkgo.WithExpireAt(time.Now().Add(sessionTTL).UTC()),
		avatarsdkgo.WithTransportFrames(collector.transportFrame),
		avatarsdkgo.WithOnError(collector.onError),
		avatarsdkgo.WithOnClose(collector.onClose),
	)

	if err := session.Init(ctx); err != nil {
		http.Error(w, fmt.Sprintf("init session: %v", err), http.StatusBadGateway)
		return
	}

	connectionID, err := session.Start(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("start session: %v", err), http.StatusBadGateway)
		return
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			log.Printf("close session %s: %v", connectionID, closeErr)
		}
	}()

	requestID, err := session.SendAudio(s.audio, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("send audio: %v", err), http.StatusBadGateway)
		return
	}
	log.Printf("sent audio request %s on connection %s", requestID, connectionID)

	if err := collector.wait(ctx); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		case errors.Is(err, context.Canceled):
			status = http.StatusRequestTimeout
		}
		http.Error(w, fmt.Sprintf("collect animations: %v", err), status)
		return
	}

	animations := collector.framesCopy()
	response := mediaResponse{
		Audio:      append([]byte(nil), s.audio...),
		Animations: animations,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func newAnimationCollector() *animationCollector {
	return &animationCollector{
		done: make(chan struct{}),
	}
}

func (c *animationCollector) transportFrame(data []byte, last bool) {
	frameCopy := append([]byte(nil), data...)

	c.mu.Lock()
	c.frames = append(c.frames, frameCopy)
	if last {
		c.last = true
	}
	c.mu.Unlock()

	if last {
		c.finish(nil)
	}
}

func (c *animationCollector) onError(err error) {
	if err == nil {
		return
	}
	c.finish(fmt.Errorf("avatar session error: %w", err))
}

func (c *animationCollector) onClose() {
	c.mu.Lock()
	last := c.last
	c.mu.Unlock()

	if last {
		c.finish(nil)
		return
	}

	c.finish(errors.New("avatar session closed before final animation frame"))
}

func (c *animationCollector) finish(err error) {
	c.mu.Lock()
	if err != nil && c.err == nil {
		c.err = err
	}
	c.mu.Unlock()

	c.once.Do(func() {
		close(c.done)
	})
}

func (c *animationCollector) wait(ctx context.Context) error {
	select {
	case <-c.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *animationCollector) framesCopy() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	frames := make([][]byte, len(c.frames))
	for i := range c.frames {
		frames[i] = append([]byte(nil), c.frames[i]...)
	}
	return frames
}
