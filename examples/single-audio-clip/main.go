// Example: Single Audio Clip
//
// This example demonstrates how to:
// 1. Initialize an avatar session
// 2. Connect to the avatar service
// 3. Send audio data
// 4. Receive animation frames
// 5. Properly close the session

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	avatarsdkgo "github.com/spatialwalk/avatar-sdk-go"
)

const (
	audioFilePath  = "../../audio.pcm"
	requestTimeout = 45 * time.Second
	sessionTTL     = 2 * time.Minute
)

type serverConfig struct {
	APIKey       string
	AppID        string
	UseQueryAuth bool
	ConsoleURL   string
	IngressURL   string
	AvatarID     string
}

// AnimationCollector collects animation frames from the avatar session.
type AnimationCollector struct {
	mu     sync.Mutex
	frames [][]byte
	last   bool
	err    error
	once   sync.Once
	done   chan struct{}
}

type mediaResponse struct {
	Audio           []byte `json:"audio"`
	AnimationsCount int    `json:"animations_count"`
	AnimationsSizes []int  `json:"animations_sizes"`
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

	fmt.Printf("Loaded audio file: %d bytes\n", len(audio))

	// Create animation collector
	collector := newAnimationCollector()

	// Create avatar session
	session := avatarsdkgo.NewAvatarSession(
		avatarsdkgo.WithAPIKey(cfg.APIKey),
		avatarsdkgo.WithAppID(cfg.AppID),
		avatarsdkgo.WithUseQueryAuth(cfg.UseQueryAuth),
		avatarsdkgo.WithConsoleEndpointURL(cfg.ConsoleURL),
		avatarsdkgo.WithIngressEndpointURL(cfg.IngressURL),
		avatarsdkgo.WithAvatarID(cfg.AvatarID),
		avatarsdkgo.WithExpireAt(time.Now().Add(sessionTTL).UTC()),
		avatarsdkgo.WithTransportFrames(collector.transportFrame),
		avatarsdkgo.WithOnError(collector.onError),
		avatarsdkgo.WithOnClose(collector.onClose),
	)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	// Initialize session (get token)
	fmt.Println("Initializing session...")
	if err := session.Init(ctx); err != nil {
		log.Fatalf("Session token error: %v", err)
	}
	fmt.Println("Session initialized")

	// Start WebSocket connection
	fmt.Println("Starting WebSocket connection...")
	connectionID, err := session.Start(ctx)
	if err != nil {
		log.Fatalf("Start session error: %v", err)
	}
	fmt.Printf("Connected with connection ID: %s\n", connectionID)

	// Send audio
	fmt.Println("Sending audio...")
	requestID, err := session.SendAudio(audio, true)
	if err != nil {
		log.Fatalf("Send audio error: %v", err)
	}
	fmt.Printf("Sent audio request: %s\n", requestID)

	// Wait for animation frames
	fmt.Println("Waiting for animation frames...")
	if err := collector.wait(ctx); err != nil {
		log.Fatalf("Wait for animations error: %v", err)
	}

	// Get results
	animations := collector.framesCopy()
	fmt.Printf("Received %d animation frames\n", len(animations))

	// Create response (similar to the Python example)
	audioPreview := audio
	if len(audioPreview) > 100 {
		audioPreview = audio[:100]
	}

	animSizes := make([]int, len(animations))
	for i, anim := range animations {
		animSizes[i] = len(anim)
	}

	response := mediaResponse{
		Audio:           audioPreview, // Just first 100 bytes for demo
		AnimationsCount: len(animations),
		AnimationsSizes: animSizes,
	}

	fmt.Println("\nResponse summary:")
	respJSON, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(respJSON))

	// Close session
	fmt.Println("\nClosing session...")
	if err := session.Close(); err != nil {
		log.Printf("close session error: %v", err)
	}
	fmt.Println("Session closed")
}

func loadConfig() (*serverConfig, error) {
	cfg := &serverConfig{
		APIKey:       strings.TrimSpace(os.Getenv("AVATAR_API_KEY")),
		AppID:        strings.TrimSpace(os.Getenv("AVATAR_APP_ID")),
		UseQueryAuth: strings.ToLower(strings.TrimSpace(os.Getenv("AVATAR_USE_QUERY_AUTH"))) == "true" || os.Getenv("AVATAR_USE_QUERY_AUTH") == "1",
		ConsoleURL:   strings.TrimSpace(os.Getenv("AVATAR_CONSOLE_ENDPOINT")),
		IngressURL:   strings.TrimSpace(os.Getenv("AVATAR_INGRESS_ENDPOINT")),
		AvatarID:     strings.TrimSpace(os.Getenv("AVATAR_SESSION_AVATAR_ID")),
	}

	var missing []string
	if cfg.APIKey == "" {
		missing = append(missing, "AVATAR_API_KEY")
	}
	if cfg.AppID == "" {
		missing = append(missing, "AVATAR_APP_ID")
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

func newAnimationCollector() *AnimationCollector {
	return &AnimationCollector{
		done: make(chan struct{}),
	}
}

func (c *AnimationCollector) transportFrame(data []byte, last bool) {
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

func (c *AnimationCollector) onError(err error) {
	if err == nil {
		return
	}
	c.finish(fmt.Errorf("avatar session error: %w", err))
}

func (c *AnimationCollector) onClose() {
	c.mu.Lock()
	last := c.last
	c.mu.Unlock()

	if last {
		c.finish(nil)
		return
	}

	c.finish(errors.New("avatar session closed before final animation frame"))
}

func (c *AnimationCollector) finish(err error) {
	c.mu.Lock()
	if err != nil && c.err == nil {
		c.err = err
	}
	c.mu.Unlock()

	c.once.Do(func() {
		close(c.done)
	})
}

func (c *AnimationCollector) wait(ctx context.Context) error {
	select {
	case <-c.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *AnimationCollector) framesCopy() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	frames := make([][]byte, len(c.frames))
	for i := range c.frames {
		frames[i] = append([]byte(nil), c.frames[i]...)
	}
	return frames
}
