// Example: Connection Pool with Concurrent Audio Processing
//
// This example demonstrates how to:
// 1. Maintain a connection pool of avatar sessions for efficient reuse
// 2. Handle multiple concurrent audio inputs simultaneously
// 3. Properly manage connection lifecycle and error handling
// 4. Use sync primitives for safe resource cleanup
//
// The pool pre-initializes a configurable number of connections and provides
// a way to borrow/return them for concurrent request processing.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	avatarsdkgo "github.com/spatialwalk/avatar-sdk-go"
)

// Configuration
const (
	poolSize           = 100              // Number of connections to maintain
	concurrentRequests = 5                // Number of concurrent audio requests per round
	numRounds          = 10               // Number of rounds to run
	roundInterval      = 30 * time.Second // Seconds between rounds (total ~5 minutes with 10 rounds)
	audioFilePath      = "../../audio.pcm"
	requestTimeout     = 45 * time.Second
	sessionTTL         = 10 * time.Minute // Longer for pool reuse over multiple rounds
)

type sdkConfig struct {
	apiKey       string
	appID        string
	useQueryAuth bool
	consoleURL   string
	ingressURL   string
	avatarID     string
}

// RequestResult represents the result of a single audio request.
type RequestResult struct {
	RequestID    string
	ConnectionID string
	FrameCount   int
	DurationMS   float64
	Success      bool
	Error        string
}

// AnimationCollector collects animation frames from an avatar session.
type AnimationCollector struct {
	mu     sync.Mutex
	frames [][]byte
	last   bool
	err    error
	done   chan struct{}
	once   sync.Once
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
	if err != nil && c.err == nil {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
	}
	c.finish(nil)
}

func (c *AnimationCollector) onClose() {
	c.mu.Lock()
	last := c.last
	c.mu.Unlock()

	if !last && c.err == nil {
		c.finish(errors.New("session closed before final animation frame"))
	} else {
		c.finish(nil)
	}
}

func (c *AnimationCollector) finish(err error) {
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

func (c *AnimationCollector) reset() {
	c.mu.Lock()
	c.frames = nil
	c.last = false
	c.err = nil
	c.mu.Unlock()
	c.done = make(chan struct{})
	c.once = sync.Once{}
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

func (c *AnimationCollector) getFrames() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	frames := make([][]byte, len(c.frames))
	for i, f := range c.frames {
		frames[i] = append([]byte(nil), f...)
	}
	return frames
}

// PooledConnection represents a pooled avatar session with its collector.
type PooledConnection struct {
	Session      *avatarsdkgo.AvatarSession
	Collector    *AnimationCollector
	ConnectionID string
	CreatedAt    time.Time
	RequestCount int
}

// AvatarConnectionPool manages a pool of avatar session connections.
type AvatarConnectionPool struct {
	poolSize      int
	configFactory func(*AnimationCollector) []avatarsdkgo.SessionOption
	sessionTTL    time.Duration

	available      chan *PooledConnection
	allConnections []*PooledConnection
	mu             sync.Mutex
	initialized    bool
	closing        bool
}

// NewAvatarConnectionPool creates a new connection pool.
func NewAvatarConnectionPool(
	poolSize int,
	configFactory func(*AnimationCollector) []avatarsdkgo.SessionOption,
	sessionTTL time.Duration,
) *AvatarConnectionPool {
	return &AvatarConnectionPool{
		poolSize:      poolSize,
		configFactory: configFactory,
		sessionTTL:    sessionTTL,
		available:     make(chan *PooledConnection, poolSize),
	}
}

// Initialize creates and connects all sessions in the pool.
func (p *AvatarConnectionPool) Initialize(ctx context.Context) error {
	p.mu.Lock()
	if p.initialized {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	fmt.Printf("Initializing connection pool with %d connections...\n", p.poolSize)

	type result struct {
		index int
		conn  *PooledConnection
		err   error
	}

	results := make(chan result, p.poolSize)
	var wg sync.WaitGroup

	for i := 0; i < p.poolSize; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			conn, err := p.createConnection(ctx, index)
			results <- result{index: index, conn: conn, err: err}
		}(i)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	successCount := 0
	for r := range results {
		if r.err != nil {
			fmt.Printf("  Connection %d: FAILED - %v\n", r.index, r.err)
		} else {
			p.mu.Lock()
			p.allConnections = append(p.allConnections, r.conn)
			p.mu.Unlock()
			p.available <- r.conn
			successCount++
			fmt.Printf("  Connection %d: OK (connection_id=%s)\n", r.index, r.conn.ConnectionID)
		}
	}

	if successCount == 0 {
		return errors.New("failed to create any connections")
	}

	fmt.Printf("Pool initialized with %d/%d connections\n", successCount, p.poolSize)

	p.mu.Lock()
	p.initialized = true
	p.mu.Unlock()

	return nil
}

func (p *AvatarConnectionPool) createConnection(ctx context.Context, index int) (*PooledConnection, error) {
	collector := newAnimationCollector()
	opts := p.configFactory(collector)

	// Override expire_at with pool TTL
	opts = append(opts, avatarsdkgo.WithExpireAt(time.Now().Add(p.sessionTTL).UTC()))

	session := avatarsdkgo.NewAvatarSession(opts...)

	if err := session.Init(ctx); err != nil {
		return nil, err
	}

	connectionID, err := session.Start(ctx)
	if err != nil {
		return nil, err
	}

	return &PooledConnection{
		Session:      session,
		Collector:    collector,
		ConnectionID: connectionID,
		CreatedAt:    time.Now(),
	}, nil
}

// Borrow borrows a connection from the pool.
func (p *AvatarConnectionPool) Borrow(ctx context.Context, timeout time.Duration) (*PooledConnection, error) {
	p.mu.Lock()
	if !p.initialized {
		p.mu.Unlock()
		return nil, errors.New("pool not initialized")
	}
	if p.closing {
		p.mu.Unlock()
		return nil, errors.New("pool is closing")
	}
	p.mu.Unlock()

	select {
	case conn := <-p.available:
		conn.Collector.reset()
		return conn, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for available connection (waited %v)", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Return returns a connection to the pool.
func (p *AvatarConnectionPool) Return(conn *PooledConnection) {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	conn.RequestCount++
	p.available <- conn
}

// Close closes all connections in the pool.
func (p *AvatarConnectionPool) Close() {
	p.mu.Lock()
	p.closing = true
	conns := p.allConnections
	p.allConnections = nil
	p.mu.Unlock()

	fmt.Println("Closing connection pool...")

	for _, conn := range conns {
		if err := conn.Session.Close(); err != nil {
			fmt.Printf("  Error closing connection %s: %v\n", conn.ConnectionID, err)
		}
	}

	// Drain the channel
	close(p.available)
	for range p.available {
	}

	p.mu.Lock()
	p.initialized = false
	p.mu.Unlock()

	fmt.Println("Connection pool closed")
}

// AvailableCount returns the number of currently available connections.
func (p *AvatarConnectionPool) AvailableCount() int {
	return len(p.available)
}

// TotalCount returns the total number of connections in the pool.
func (p *AvatarConnectionPool) TotalCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.allConnections)
}

// GetStats returns pool statistics.
func (p *AvatarConnectionPool) GetStats() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	conns := make([]map[string]interface{}, len(p.allConnections))
	totalRequests := 0
	for i, c := range p.allConnections {
		totalRequests += c.RequestCount
		conns[i] = map[string]interface{}{
			"connection_id": c.ConnectionID,
			"request_count": c.RequestCount,
			"age_seconds":   time.Since(c.CreatedAt).Seconds(),
		}
	}

	return map[string]interface{}{
		"total_connections":     len(p.allConnections),
		"available_connections": len(p.available),
		"total_requests_served": totalRequests,
		"connections":           conns,
	}
}

// RoundResult represents the result of a single round of concurrent requests.
type RoundResult struct {
	RoundNum   int
	StartTime  float64
	DurationMS float64
	Successful int
	Failed     int
	Results    []RequestResult
}

func processAudioRequest(
	ctx context.Context,
	pool *AvatarConnectionPool,
	audio []byte,
	requestNum int,
) RequestResult {
	start := time.Now()

	conn, err := pool.Borrow(ctx, 30*time.Second)
	if err != nil {
		return RequestResult{
			DurationMS: float64(time.Since(start).Milliseconds()),
			Success:    false,
			Error:      err.Error(),
		}
	}
	defer pool.Return(conn)

	requestID, err := conn.Session.SendAudio(audio, true)
	if err != nil {
		return RequestResult{
			ConnectionID: conn.ConnectionID,
			DurationMS:   float64(time.Since(start).Milliseconds()),
			Success:      false,
			Error:        err.Error(),
		}
	}

	if err := conn.Collector.wait(ctx); err != nil {
		return RequestResult{
			RequestID:    requestID,
			ConnectionID: conn.ConnectionID,
			DurationMS:   float64(time.Since(start).Milliseconds()),
			Success:      false,
			Error:        err.Error(),
		}
	}

	frames := conn.Collector.getFrames()
	return RequestResult{
		RequestID:    requestID,
		ConnectionID: conn.ConnectionID,
		FrameCount:   len(frames),
		DurationMS:   float64(time.Since(start).Milliseconds()),
		Success:      true,
	}
}

func runMultipleRounds(
	ctx context.Context,
	pool *AvatarConnectionPool,
	audio []byte,
	numRounds int,
	requestsPerRound int,
	intervalSeconds time.Duration,
) []RoundResult {
	totalExpectedDuration := time.Duration(numRounds-1) * intervalSeconds
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Println("STARTING MULTI-ROUND TEST")
	fmt.Printf("%s\n", strings.Repeat("=", 60))
	fmt.Printf("Rounds: %d\n", numRounds)
	fmt.Printf("Requests per round: %d\n", requestsPerRound)
	fmt.Printf("Interval between rounds: %v\n", intervalSeconds)
	fmt.Printf("Expected total duration: ~%.1f minutes\n", totalExpectedDuration.Minutes())
	fmt.Printf("Pool size: %d connections\n", pool.TotalCount())
	fmt.Printf("%s\n", strings.Repeat("=", 60))

	overallStart := time.Now()
	var roundResults []RoundResult

	for roundNum := 0; roundNum < numRounds; roundNum++ {
		roundStart := time.Now()
		elapsedTotal := time.Since(overallStart).Seconds()

		fmt.Printf("\n[Round %d/%d] (elapsed: %.1fs, pool: %d/%d available)\n",
			roundNum+1, numRounds, elapsedTotal,
			pool.AvailableCount(), pool.TotalCount())

		// Run concurrent requests
		var wg sync.WaitGroup
		resultsChan := make(chan RequestResult, requestsPerRound)

		for i := 0; i < requestsPerRound; i++ {
			wg.Add(1)
			go func(reqNum int) {
				defer wg.Done()
				result := processAudioRequest(ctx, pool, audio, reqNum)
				resultsChan <- result
			}(i)
		}

		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		var results []RequestResult
		for r := range resultsChan {
			results = append(results, r)
		}

		roundDuration := float64(time.Since(roundStart).Milliseconds())
		successful := 0
		failed := 0
		for _, r := range results {
			if r.Success {
				successful++
			} else {
				failed++
			}
		}

		roundResult := RoundResult{
			RoundNum:   roundNum + 1,
			StartTime:  elapsedTotal,
			DurationMS: roundDuration,
			Successful: successful,
			Failed:     failed,
			Results:    results,
		}
		roundResults = append(roundResults, roundResult)

		fmt.Printf("  Completed: %d OK, %d FAILED in %.1fms\n", successful, failed, roundDuration)

		// Show any errors
		for _, r := range results {
			if !r.Success {
				fmt.Printf("    ERROR: %s\n", r.Error)
			}
		}

		// Wait before next round (except for the last round)
		if roundNum < numRounds-1 {
			fmt.Printf("  Waiting %v until next round...\n", intervalSeconds)
			time.Sleep(intervalSeconds)
		}
	}

	overallDuration := time.Since(overallStart)
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Println("MULTI-ROUND TEST COMPLETE")
	fmt.Printf("Total duration: %.1fs (%.1f minutes)\n", overallDuration.Seconds(), overallDuration.Minutes())
	fmt.Printf("%s\n", strings.Repeat("=", 60))

	return roundResults
}

func printMultiRoundSummary(roundResults []RoundResult) {
	totalRequests := 0
	totalSuccessful := 0
	totalFailed := 0
	var allResults []RequestResult

	for _, rr := range roundResults {
		totalRequests += rr.Successful + rr.Failed
		totalSuccessful += rr.Successful
		totalFailed += rr.Failed
		allResults = append(allResults, rr.Results...)
	}

	var successfulResults []RequestResult
	for _, r := range allResults {
		if r.Success {
			successfulResults = append(successfulResults, r)
		}
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Println("MULTI-ROUND SUMMARY")
	fmt.Printf("%s\n", strings.Repeat("=", 60))

	fmt.Println("\nOverall Statistics:")
	fmt.Printf("  Total rounds: %d\n", len(roundResults))
	fmt.Printf("  Total requests: %d\n", totalRequests)
	fmt.Printf("  Successful: %d (%.1f%%)\n", totalSuccessful, 100*float64(totalSuccessful)/float64(totalRequests))
	fmt.Printf("  Failed: %d (%.1f%%)\n", totalFailed, 100*float64(totalFailed)/float64(totalRequests))

	if len(successfulResults) > 0 {
		var totalDuration float64
		var totalFrames int
		minDuration := successfulResults[0].DurationMS
		maxDuration := successfulResults[0].DurationMS

		for _, r := range successfulResults {
			totalDuration += r.DurationMS
			totalFrames += r.FrameCount
			if r.DurationMS < minDuration {
				minDuration = r.DurationMS
			}
			if r.DurationMS > maxDuration {
				maxDuration = r.DurationMS
			}
		}

		fmt.Println("\nRequest Performance:")
		fmt.Printf("  Avg duration: %.2fms\n", totalDuration/float64(len(successfulResults)))
		fmt.Printf("  Min duration: %.2fms\n", minDuration)
		fmt.Printf("  Max duration: %.2fms\n", maxDuration)
		fmt.Printf("  Avg frames: %.1f\n", float64(totalFrames)/float64(len(successfulResults)))
	}

	// Per-round breakdown
	fmt.Println("\nPer-Round Breakdown:")
	fmt.Printf("  %-6s %-10s %-14s %-6s %-6s\n", "Round", "Time(s)", "Duration(ms)", "OK", "FAIL")
	fmt.Printf("  %s %s %s %s %s\n",
		strings.Repeat("-", 6), strings.Repeat("-", 10),
		strings.Repeat("-", 14), strings.Repeat("-", 6), strings.Repeat("-", 6))
	for _, rr := range roundResults {
		fmt.Printf("  %-6d %-10.1f %-14.1f %-6d %-6d\n",
			rr.RoundNum, rr.StartTime, rr.DurationMS, rr.Successful, rr.Failed)
	}

	// Connection usage distribution
	connUsage := make(map[string]int)
	for _, r := range successfulResults {
		connUsage[r.ConnectionID]++
	}

	if len(connUsage) > 0 {
		fmt.Println("\nConnection Usage Distribution:")
		for connID, count := range connUsage {
			displayID := connID
			if len(displayID) > 20 {
				displayID = displayID[:20] + "..."
			}
			pct := 100 * float64(count) / float64(len(successfulResults))
			fmt.Printf("  %s: %d requests (%.1f%%)\n", displayID, count, pct)
		}
	}
}

func main() {
	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// Load audio file
	audio, err := loadAudio(audioFilePath)
	if err != nil {
		log.Fatalf("audio fixture error: %v", err)
	}
	fmt.Printf("Loaded audio file: %d bytes\n", len(audio))

	// Config factory that creates session config with collector callbacks
	configFactory := func(collector *AnimationCollector) []avatarsdkgo.SessionOption {
		return []avatarsdkgo.SessionOption{
			avatarsdkgo.WithAPIKey(cfg.apiKey),
			avatarsdkgo.WithAppID(cfg.appID),
			avatarsdkgo.WithUseQueryAuth(cfg.useQueryAuth),
			avatarsdkgo.WithConsoleEndpointURL(cfg.consoleURL),
			avatarsdkgo.WithIngressEndpointURL(cfg.ingressURL),
			avatarsdkgo.WithAvatarID(cfg.avatarID),
			avatarsdkgo.WithTransportFrames(collector.transportFrame),
			avatarsdkgo.WithOnError(collector.onError),
			avatarsdkgo.WithOnClose(collector.onClose),
		}
	}

	// Create connection pool
	pool := NewAvatarConnectionPool(poolSize, configFactory, sessionTTL)

	ctx := context.Background()

	// Initialize the pool
	if err := pool.Initialize(ctx); err != nil {
		log.Fatalf("pool initialization error: %v", err)
	}

	// Run multiple rounds of concurrent requests over time
	roundResults := runMultipleRounds(
		ctx,
		pool,
		audio,
		numRounds,
		concurrentRequests,
		roundInterval,
	)

	// Print multi-round summary
	printMultiRoundSummary(roundResults)

	// Print pool stats
	stats := pool.GetStats()
	fmt.Printf("\nFinal Pool Statistics:\n")
	fmt.Printf("  Total requests served: %v\n", stats["total_requests_served"])
	fmt.Printf("  Connections in pool: %v\n", stats["total_connections"])
	if conns, ok := stats["connections"].([]map[string]interface{}); ok {
		for _, conn := range conns {
			connID := conn["connection_id"].(string)
			if len(connID) > 20 {
				connID = connID[:20] + "..."
			}
			fmt.Printf("  Connection %s: %v requests, age: %.1fs (%.1f min)\n",
				connID, conn["request_count"],
				conn["age_seconds"].(float64),
				conn["age_seconds"].(float64)/60)
		}
	}

	// Close pool
	pool.Close()
}

func loadConfig() (*sdkConfig, error) {
	cfg := &sdkConfig{
		apiKey:       strings.TrimSpace(os.Getenv("AVATAR_API_KEY")),
		appID:        strings.TrimSpace(os.Getenv("AVATAR_APP_ID")),
		useQueryAuth: strings.ToLower(strings.TrimSpace(os.Getenv("AVATAR_USE_QUERY_AUTH"))) == "true" || os.Getenv("AVATAR_USE_QUERY_AUTH") == "1",
		consoleURL:   strings.TrimSpace(os.Getenv("AVATAR_CONSOLE_ENDPOINT")),
		ingressURL:   strings.TrimSpace(os.Getenv("AVATAR_INGRESS_ENDPOINT")),
		avatarID:     strings.TrimSpace(os.Getenv("AVATAR_SESSION_AVATAR_ID")),
	}

	var missing []string
	if cfg.apiKey == "" {
		missing = append(missing, "AVATAR_API_KEY")
	}
	if cfg.appID == "" {
		missing = append(missing, "AVATAR_APP_ID")
	}
	if cfg.consoleURL == "" {
		missing = append(missing, "AVATAR_CONSOLE_ENDPOINT")
	}
	if cfg.ingressURL == "" {
		missing = append(missing, "AVATAR_INGRESS_ENDPOINT")
	}
	if cfg.avatarID == "" {
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
