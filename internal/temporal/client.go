package temporal

import (
	"context"
	"errors"
	"log"
	"os"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/client"
)

// HostPort returns the Temporal host:port from the TEMPORAL_HOST_PORT env
// var, defaulting to localhost:7233.
func HostPort() string {
	h := os.Getenv("TEMPORAL_HOST_PORT")
	if h == "" {
		h = "localhost:7233"
	}
	return h
}

// TaskQueue is the task queue the demo worker listens on and that new
// schedules are created with by default.
const TaskQueue = "schedules-demo"

// ReconnectingClient holds a Temporal client that is (re)established in the
// background. The zero value is safe: Client() returns (nil, ErrNotConnected)
// until the first successful dial. This lets the web UI start immediately and
// degrade to an "offline" banner while Temporal is unreachable.
type ReconnectingClient struct {
	c        atomic.Pointer[client.Client]
	hostPort string
}

// ErrNotConnected is returned by Client() before the first successful dial.
var ErrNotConnected = errors.New("not connected to temporal yet")

// NewReconnectingClient starts a background goroutine that dials Temporal and
// retries forever (with backoff) until it succeeds. Returns immediately.
func NewReconnectingClient() *ReconnectingClient {
	rc := &ReconnectingClient{hostPort: HostPort()}
	go rc.connectLoop()
	return rc
}

func (rc *ReconnectingClient) connectLoop() {
	backoff := time.Second
	for {
		c, err := client.Dial(client.Options{HostPort: rc.hostPort})
		if err != nil {
			log.Printf("temporal: dial %s failed: %v (retrying in %s)", rc.hostPort, err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			continue
		}

		// Swap out the previous client (if any) and close it.
		if old := rc.c.Swap(&c); old != nil {
			(*old).Close()
		}
		log.Printf("connected to temporal at %s", rc.hostPort)
		backoff = time.Second

		// Block until the connection drops, then reconnect.
		rc.watch(c)
		time.Sleep(time.Second)
	}
}

// watch blocks until the client's connection is reported unhealthy.
func (rc *ReconnectingClient) watch(c client.Client) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := c.CheckHealth(ctx, nil)
		cancel()
		if err != nil {
			log.Printf("temporal: health check failed: %v (reconnecting)", err)
			return
		}
	}
}

// Client returns the current Temporal client, or ErrNotConnected if not yet
// connected.
func (rc *ReconnectingClient) Client() (client.Client, error) {
	p := rc.c.Load()
	if p == nil {
		return nil, ErrNotConnected
	}
	return *p, nil
}
