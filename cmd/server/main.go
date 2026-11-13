package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"temporal-schedules-ui/internal/temporal"
	"temporal-schedules-ui/internal/web"
)

func main() {
	// 1. Start a reconnecting Temporal client in the background. The web UI
	//    can serve immediately and degrades to an "offline" banner while
	//    Temporal is unreachable.
	rc := temporal.NewReconnectingClient()

	// 2. Start the demo worker as soon as we have a client. The worker keeps
	//    running; if the connection drops the reconnect loop re-establishes it
	//    and we restart the worker on the new client.
	go runWorker(rc)

	// 3. Start the HTTP UI.
	addr := os.Getenv("SCHEDULES_UI_ADDR")
	if addr == "" {
		addr = ":7099"
	}
	srv, err := web.New(rc)
	if err != nil {
		log.Fatalf("init web: %v", err)
	}

	// Graceful shutdown on Ctrl-C.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down…")
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// runWorker waits for a connected Temporal client, then starts a worker that
// runs the demo SendEmailWorkflow so triggered schedule actions actually
// execute. If the connection drops, it waits for reconnection and restarts.
func runWorker(rc *temporal.ReconnectingClient) {
	var cur client.Client
	var w worker.Worker
	var mu sync.Mutex

	for {
		c, err := rc.Client()
		if err != nil {
			// Not connected yet — retry shortly.
			sleepBriefly()
			continue
		}

		mu.Lock()
		if cur == nil || c != cur {
			if w != nil {
				w.Stop()
			}
			w = worker.New(c, temporal.TaskQueue, worker.Options{})
			w.RegisterWorkflow(temporal.SendEmailWorkflow)
			w.RegisterActivity(temporal.SendEmailActivity)
			if err := w.Start(); err != nil {
				log.Printf("warn: worker start: %v", err)
			} else {
				log.Printf("worker started on task queue %q", temporal.TaskQueue)
			}
			cur = c
		}
		mu.Unlock()
		sleepBriefly()
	}
}

func sleepBriefly() {
	time.Sleep(3 * time.Second)
}
