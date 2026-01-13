// Example: HTTP Service
//
// This example exposes a simple HTTP API that:
// - Accepts a POST request with a desired sample rate
// - Returns a PCM audio clip at that sample rate (loaded from audio_{rate}.pcm)
// - Uses the SDK to make a real request to the avatar service and returns:
//   - The audio bytes (base64 in JSON)
//   - The list of base64-encoded protobuf Message binaries received from the service
//     (typically MESSAGE_SERVER_RESPONSE_ANIMATION, with end=true on the final frame).
//
// Notes:
// - This is a *server-side* helper example.
// - The SDK does not need to unmarshal animation payloads; consumers receive the original
//   Message binary data via the callback in the websocket flow.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	avatarsdkgo "github.com/spatialwalk/avatar-sdk-go"
)

const (
	defaultListenAddr            = ":8080"
	defaultSessionTTLMinutes     = 2
	defaultRequestTimeoutSeconds = 45
)

var audioFilePattern = regexp.MustCompile(`^audio_(\d+)\.pcm$`)

type audioAsset struct {
	sampleRate int
	path       string
}

type sdkConfig struct {
	apiKey             string
	appID              string
	consoleEndpointURL string
	ingressEndpointURL string
	avatarID           string
}

type generateRequest struct {
	SampleRate int `json:"sample_rate"`
}

type generateResponse struct {
	SampleRate              int      `json:"sample_rate"`
	AudioFormat             string   `json:"audio_format"`
	AudioBase64             string   `json:"audio_base64"`
	ConnectionID            string   `json:"connection_id"`
	ReqID                   string   `json:"req_id"`
	AnimationMessagesBase64 []string `json:"animation_messages_base64"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

type collector struct {
	mu     sync.Mutex
	frames [][]byte
	last   bool
	err    error
	done   chan struct{}
	once   sync.Once
}

func newCollector() *collector {
	return &collector{
		done: make(chan struct{}),
	}
}

func (c *collector) transportFrames(frame []byte, last bool) {
	frameCopy := append([]byte(nil), frame...)
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

func (c *collector) onError(err error) {
	if c.err == nil && err != nil {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
	}
	c.finish(nil)
}

func (c *collector) onClose() {
	c.mu.Lock()
	last := c.last
	c.mu.Unlock()

	if !last && c.err == nil {
		c.finish(errors.New("session_closed_before_final_frame"))
	} else {
		c.finish(nil)
	}
}

func (c *collector) finish(err error) {
	if err != nil {
		c.mu.Lock()
		if c.err == nil {
			c.err = err
		}
		c.mu.Unlock()
	}
	c.once.Do(func() {
		close(c.done)
	})
}

func (c *collector) wait(ctx context.Context) error {
	select {
	case <-c.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *collector) getFrames() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	frames := make([][]byte, len(c.frames))
	for i, f := range c.frames {
		frames[i] = append([]byte(nil), f...)
	}
	return frames
}

type server struct {
	assets    map[int]audioAsset
	sdkConfig sdkConfig
}

func main() {
	repoRoot := "../../" // relative to examples/http-service/

	assets, err := findAudioAssets(repoRoot)
	if err != nil {
		log.Fatalf("failed to find audio assets: %v", err)
	}
	if len(assets) == 0 {
		log.Fatalf("no audio_{rate}.pcm files found in %s", repoRoot)
	}

	cfg, err := loadSDKConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	listenAddr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	srv := &server{
		assets:    assets,
		sdkConfig: cfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/generate", srv.handleGenerate)

	log.Printf("listening on %s", listenAddr)
	log.Printf("available sample rates: %v", getSampleRates(assets))
	if err := http.ListenAndServe(listenAddr, enableCORS(mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) // nolint:errcheck
}

func (s *server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if req.SampleRate == 0 {
		writeError(w, http.StatusBadRequest, "missing_sample_rate", "sample_rate is required")
		return
	}

	asset, ok := s.assets[req.SampleRate]
	if !ok {
		writeError(w, http.StatusNotFound, "unsupported_sample_rate",
			fmt.Sprintf("supported sample rates: %v", getSampleRates(s.assets)))
		return
	}

	audio, err := os.ReadFile(asset.path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_audio_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultRequestTimeoutSeconds*time.Second)
	defer cancel()

	coll := newCollector()

	session := avatarsdkgo.NewAvatarSession(
		avatarsdkgo.WithAPIKey(s.sdkConfig.apiKey),
		avatarsdkgo.WithAppID(s.sdkConfig.appID),
		avatarsdkgo.WithConsoleEndpointURL(s.sdkConfig.consoleEndpointURL),
		avatarsdkgo.WithIngressEndpointURL(s.sdkConfig.ingressEndpointURL),
		avatarsdkgo.WithAvatarID(s.sdkConfig.avatarID),
		avatarsdkgo.WithExpireAt(time.Now().Add(defaultSessionTTLMinutes*time.Minute).UTC()),
		avatarsdkgo.WithSampleRate(req.SampleRate),
		avatarsdkgo.WithBitrate(0),
		avatarsdkgo.WithTransportFrames(coll.transportFrames),
		avatarsdkgo.WithOnError(coll.onError),
		avatarsdkgo.WithOnClose(coll.onClose),
	)

	var connectionID, reqID string

	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			log.Printf("close session error: %v", closeErr)
		}
	}()

	if err := session.Init(ctx); err != nil {
		if sdkErr, ok := err.(*avatarsdkgo.AvatarSDKError); ok {
			writeError(w, http.StatusBadGateway, "sdk_error", sdkErr.Message)
			return
		}
		writeError(w, http.StatusBadGateway, "session_token_error", err.Error())
		return
	}

	connectionID, err = session.Start(ctx)
	if err != nil {
		if sdkErr, ok := err.(*avatarsdkgo.AvatarSDKError); ok {
			writeError(w, http.StatusBadGateway, "sdk_error", sdkErr.Message)
			return
		}
		writeError(w, http.StatusBadGateway, "start_session_error", err.Error())
		return
	}

	reqID, err = session.SendAudio(audio, true)
	if err != nil {
		writeError(w, http.StatusBadGateway, "send_audio_error", err.Error())
		return
	}

	if err := coll.wait(ctx); err != nil {
		writeError(w, http.StatusBadGateway, "request_failed", err.Error())
		return
	}

	frames := coll.getFrames()
	animationsBase64 := make([]string, len(frames))
	for i, f := range frames {
		animationsBase64[i] = base64.StdEncoding.EncodeToString(f)
	}

	resp := generateResponse{
		SampleRate:              req.SampleRate,
		AudioFormat:             "pcm_s16le_mono",
		AudioBase64:             base64.StdEncoding.EncodeToString(audio),
		ConnectionID:            connectionID,
		ReqID:                   reqID,
		AnimationMessagesBase64: animationsBase64,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{Error: code, Message: message}) // nolint:errcheck
}

func findAudioAssets(root string) (map[int]audioAsset, error) {
	assets := make(map[int]audioAsset)

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := audioFilePattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}
		rate, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		assets[rate] = audioAsset{
			sampleRate: rate,
			path:       filepath.Join(root, entry.Name()),
		}
	}

	return assets, nil
}

func getSampleRates(assets map[int]audioAsset) []int {
	rates := make([]int, 0, len(assets))
	for rate := range assets {
		rates = append(rates, rate)
	}
	return rates
}

func loadSDKConfig() (sdkConfig, error) {
	cfg := sdkConfig{
		apiKey:             strings.TrimSpace(os.Getenv("AVATAR_API_KEY")),
		appID:              strings.TrimSpace(os.Getenv("AVATAR_APP_ID")),
		consoleEndpointURL: strings.TrimSpace(os.Getenv("AVATAR_CONSOLE_ENDPOINT")),
		ingressEndpointURL: strings.TrimSpace(os.Getenv("AVATAR_INGRESS_ENDPOINT")),
		avatarID:           strings.TrimSpace(os.Getenv("AVATAR_SESSION_AVATAR_ID")),
	}

	var missing []string
	if cfg.apiKey == "" {
		missing = append(missing, "AVATAR_API_KEY")
	}
	if cfg.appID == "" {
		missing = append(missing, "AVATAR_APP_ID")
	}
	if cfg.consoleEndpointURL == "" {
		missing = append(missing, "AVATAR_CONSOLE_ENDPOINT")
	}
	if cfg.ingressEndpointURL == "" {
		missing = append(missing, "AVATAR_INGRESS_ENDPOINT")
	}
	if cfg.avatarID == "" {
		missing = append(missing, "AVATAR_SESSION_AVATAR_ID")
	}

	if len(missing) > 0 {
		return sdkConfig{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}
