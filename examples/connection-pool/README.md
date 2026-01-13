# Connection Pool Example

This example demonstrates how to maintain a connection pool of avatar sessions and handle multiple concurrent audio inputs efficiently over an extended period (~5 minutes).

## Features

- **Connection Pooling**: Pre-initializes a configurable number of WebSocket connections for faster request handling
- **Concurrent Processing**: Handles multiple audio requests simultaneously using goroutines
- **Multi-Round Testing**: Runs multiple rounds of concurrent requests over time (simulating ~5 minutes of sustained usage)
- **Safe Resource Management**: Uses sync primitives for automatic connection borrowing/returning
- **Connection Reuse**: Connections are reused across rounds, reducing overhead
- **Long-Lived Connections**: Tests connection stability over extended periods
- **Statistics Tracking**: Tracks request counts, timing, and per-round performance

## Configuration

Set the following environment variables:

```bash
export AVATAR_API_KEY="your-api-key"
export AVATAR_APP_ID="your-app-id"
export AVATAR_CONSOLE_ENDPOINT="https://console.example.com"
export AVATAR_INGRESS_ENDPOINT="https://ingress.example.com"
export AVATAR_SESSION_AVATAR_ID="your-avatar-id"

# Optional
export AVATAR_USE_QUERY_AUTH="false"  # Set to "true" for web-style auth
```

## Running the Example

```bash
# From the repository root
cd examples/connection-pool
go run main.go
```

## How It Works

### Pool Initialization

The `AvatarConnectionPool` manages a set of pre-initialized connections:

```go
pool := NewAvatarConnectionPool(poolSize, configFactory, sessionTTL)
if err := pool.Initialize(ctx); err != nil {
    log.Fatal(err)
}
defer pool.Close()
```

### Borrowing Connections

Use `Borrow` and `Return` to safely manage connections:

```go
conn, err := pool.Borrow(ctx, 30*time.Second)
if err != nil {
    return err
}
defer pool.Return(conn)

// Send audio using the borrowed connection
requestID, err := conn.Session.SendAudio(audio, true)
if err != nil {
    return err
}

// Wait for animation frames
if err := conn.Collector.wait(ctx); err != nil {
    return err
}

// Access results
frames := conn.Collector.getFrames()
```

### Concurrent Requests

When you have more requests than connections, they queue up:

```go
// With poolSize=3 and 5 concurrent requests:
// - 3 requests run immediately
// - 2 requests wait for connections to become available
var wg sync.WaitGroup
for i := 0; i < 5; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        processAudioRequest(ctx, pool, audio)
    }()
}
wg.Wait()
```

## Pool Configuration Options

| Parameter | Default | Description |
|-----------|---------|-------------|
| `poolSize` | 100 | Number of WebSocket connections to maintain |
| `concurrentRequests` | 5 | Number of concurrent requests per round |
| `numRounds` | 10 | Number of rounds to run |
| `roundInterval` | 30s | Time between rounds (~5 min total with 10 rounds) |
| `sessionTTL` | 10min | Session time-to-live |
| `requestTimeout` | 45s | Timeout for each audio request |

Modify these constants at the top of `main.go` to adjust behavior.

### Multi-Round Testing

The example runs multiple rounds of concurrent requests:

```go
// Run 10 rounds of 5 concurrent requests, with 30s between rounds
// Total: 50 requests over ~5 minutes
roundResults := runMultipleRounds(ctx, pool, audio, 10, 5, 30*time.Second)
```

This tests:
- Connection stability over extended periods
- Connection reuse across many requests
- Pool behavior under sustained load

## Expected Output

```
Loaded audio file: 12345 bytes
Initializing connection pool with 3 connections...
  Connection 0: OK (connection_id=abc123...)
  Connection 1: OK (connection_id=def456...)
  Connection 2: OK (connection_id=ghi789...)
Pool initialized with 3/3 connections

============================================================
STARTING MULTI-ROUND TEST
============================================================
Rounds: 10
Requests per round: 5
Interval between rounds: 30s
Expected total duration: ~4.5 minutes
Pool size: 3 connections
============================================================

[Round 1/10] (elapsed: 0.0s, pool: 3/3 available)
  Completed: 5 OK, 0 FAILED in 2500.0ms
  Waiting 30s until next round...

[Round 2/10] (elapsed: 32.5s, pool: 3/3 available)
  Completed: 5 OK, 0 FAILED in 2400.0ms
  Waiting 30s until next round...

... (rounds 3-9) ...

[Round 10/10] (elapsed: 272.5s, pool: 3/3 available)
  Completed: 5 OK, 0 FAILED in 2300.0ms

============================================================
MULTI-ROUND TEST COMPLETE
Total duration: 275.0s (4.6 minutes)
============================================================

============================================================
MULTI-ROUND SUMMARY
============================================================

Overall Statistics:
  Total rounds: 10
  Total requests: 50
  Successful: 50 (100.0%)
  Failed: 0 (0.0%)

Request Performance:
  Avg duration: 2450.00ms
  Min duration: 2200.00ms
  Max duration: 2700.00ms
  Avg frames: 10.0

Per-Round Breakdown:
  Round  Time(s)    Duration(ms)   OK     FAIL  
  ------ ---------- -------------- ------ ------
  1      0.0        2500.0         5      0     
  2      32.5       2400.0         5      0     
  ...
  10     272.5      2300.0         5      0     

Connection Usage Distribution:
  abc123...: 17 requests (34.0%)
  def456...: 17 requests (34.0%)
  ghi789...: 16 requests (32.0%)

Final Pool Statistics:
  Total requests served: 50
  Connections in pool: 3
  Connection abc123...: 17 requests, age: 275.0s (4.6 min)
  Connection def456...: 17 requests, age: 275.0s (4.6 min)
  Connection ghi789...: 16 requests, age: 275.0s (4.6 min)

Closing connection pool...
Connection pool closed
```

## Integration with Your Application

To use this pattern in your own application:

1. Create the pool during application startup
2. Share the pool instance across request handlers
3. Use `pool.Borrow()` and `pool.Return()` in each request handler
4. Close the pool during application shutdown

Example with net/http:

```go
var pool *AvatarConnectionPool

func main() {
    pool = NewAvatarConnectionPool(...)
    if err := pool.Initialize(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer pool.Close()
    
    http.HandleFunc("/generate", handleGenerate)
    http.ListenAndServe(":8080", nil)
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    conn, err := pool.Borrow(ctx, 30*time.Second)
    if err != nil {
        http.Error(w, err.Error(), http.StatusServiceUnavailable)
        return
    }
    defer pool.Return(conn)
    
    // Send audio and wait for frames...
}
```
