package http

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

type fakeStream struct{ c, done chan struct{} }

func (f fakeStream) C() <-chan struct{}    { return f.c }
func (f fakeStream) Done() <-chan struct{} { return f.done }

// ccd's server has a 30 s ReadTimeout and a 60 s WriteTimeout
// (adapters/cli/serve.go). The write deadline, left alone, fails the first
// write after it and ends the stream. This runs a stream past both, scaled
// down, and checks it still delivers.
func TestAStreamOutlivesTheServersTimeouts(t *testing.T) {
	defer SetStreamTimings(50*time.Millisecond, time.Minute)()

	fs := fakeStream{c: make(chan struct{}, 1), done: make(chan struct{})}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveStream(w, r, fs) }),
		ReadHeaderTimeout: 200 * time.Millisecond,
		ReadTimeout:       200 * time.Millisecond,
		WriteTimeout:      200 * time.Millisecond,
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	time.Sleep(600 * time.Millisecond) // three times both timeouts
	fs.c <- struct{}{}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the stream ended at the server's timeouts")
			}
			if line == "event: changed" {
				close(fs.done)
				return
			}
		case <-deadline:
			t.Fatal("no change delivered after the server's timeouts passed")
		}
	}
}
