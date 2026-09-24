package web

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
)

// A pod receiving SIGTERM must finish the requests it is already serving:
// before Close drained the server, those ended as 502 at the ingress.
func TestCloseDrainsInFlightRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started := make(chan struct{})
	var finished atomic.Bool
	r := gin.New()
	r.GET("/slow", func(c *gin.Context) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		c.String(http.StatusOK, "done")
		finished.Store(true)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	s := &Web{host: "127.0.0.1", port: port, gs: cs.NewGracefulServer(5 * time.Second), r: r}
	served := make(chan error, 1)
	go func() { served <- s.Serve() }()

	url := fmt.Sprintf("http://127.0.0.1:%d/slow", port)
	type result struct {
		body string
		err  error
	}
	res := make(chan result, 1)
	go func() {
		var resp *http.Response
		var err error
		for i := 0; i < 50; i++ {
			if resp, err = http.Get(url); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			res <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		res <- result{body: string(b), err: err}
	}()

	<-started
	s.Close()
	// serve() closes Redis, NATS and the rest right after Close returns, so
	// the handler must be done by then, not merely left running.
	if !finished.Load() {
		t.Fatal("Close returned while a request was still in flight")
	}

	got := <-res
	if got.err != nil || got.body != "done" {
		t.Fatalf("in-flight request was cut: body=%q err=%v", got.body, got.err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve returned %v after a graceful close", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Fatal("server still accepts connections after Close")
	}
}
