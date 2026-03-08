package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func deleteStationHelper(srv *httptest.Server, id string) {
	req, _ := http.NewRequest("DELETE", srv.URL+"/station?id="+id, nil)
	r, _ := http.DefaultClient.Do(req)
	if r != nil {
		r.Body.Close()
	}
}

func TestBroadcastAndListen(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	r1, _ := http.Post(srv.URL+"/station?id=punk", "", nil)
	r1.Body.Close()
	defer deleteStationHelper(srv, "punk")

	// broadcast first
	r2, _ := http.Post(
		srv.URL+"/station/broadcast?id=punk",
		"application/octet-stream",
		strings.NewReader("Now playing: Time Bomb"),
	)
	r2.Body.Close()

	resp, err := http.Get(srv.URL + "/station/listen?id=punk")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)

	if string(buf[:n]) != "Now playing: Time Bomb" {
		t.Fatalf("expected 'Now playing: Time Bomb', got %q",
			string(buf[:n]))
	}
}
func TestListenGetsHistory(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	r1, _ := http.Post(srv.URL+"/station?id=punk", "", nil)
	r1.Body.Close()
	defer deleteStationHelper(srv, "punk")

	r2, _ := http.Post(srv.URL+"/station/broadcast?id=punk",
		"application/octet-stream",
		strings.NewReader("Now playing: Time Bomb"))
	r2.Body.Close()
	r3, _ := http.Post(srv.URL+"/station/broadcast?id=punk",
		"application/octet-stream",
		strings.NewReader("Now playing: Ruby Soho"))
	r3.Body.Close()

	resp, err := http.Get(srv.URL + "/station/listen?id=punk") //nolint:bodyclose
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// read in a loop — a single Read() may not return both messages
	// due to TCP buffering / timing
	done := make(chan string)
	go func() {
		var acc []byte
		buf := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(buf)
			acc = append(acc, buf[:n]...)
			if bytes.Contains(acc, []byte("Time Bomb")) && bytes.Contains(acc, []byte("Ruby Soho")) {
				done <- string(acc)
				return
			}
			if err != nil {
				done <- string(acc)
				return
			}
		}
	}()

	select {
	case body := <-done:
		if !strings.Contains(body, "Time Bomb") {
			t.Fatalf("expected Time Bomb in history, got %q", body)
		}
		if !strings.Contains(body, "Ruby Soho") {
			t.Fatalf("expected Ruby Soho in history, got %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for history")
	}
}

func TestCreateStation(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/station?id=punk", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	defer deleteStationHelper(srv, "punk")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateDuplicateStation(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	r1, _ := http.Post(srv.URL+"/station?id=punk", "", nil)
	r1.Body.Close()
	defer deleteStationHelper(srv, "punk")
	resp, err := http.Post(srv.URL+"/station?id=punk", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestListenNonexistent(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/station/listen?id=nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// tests the slow reader behavior happens - it gets all messages.
// we are no longer dropping
func TestSlowReaderGetsAllHistory(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	for i := 0; i < 1500; i++ {
		b.Send([]byte(fmt.Sprintf("msg %d", i)))
	}

	cursor := 0
	count := 0
	for count < 1500 {
		_, ok := b.Read(context.Background(), &cursor)
		if !ok {
			break
		}
		count++
	}
	if count != 1500 {
		t.Fatalf("expected 1500 messages, got %d", count)
	}
}

// we expect that slow readers will not slow everyone else down
// which i'm testing with: independent readers appear to not blocked. both
// cursors can advance indendently

func TestSlowReaderDoesntBlockOthers(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	// fill buffer
	for i := 0; i < 15; i++ {
		b.Send([]byte(fmt.Sprintf("msg %d", i)))
	}

	// fast reader drains immediately — should get 15
	c1, c2 := 0, 0
	for i := 0; i < 15; i++ {
		b.Read(context.Background(), &c1)
	}
	for i := 0; i < 15; i++ {
		b.Read(context.Background(), &c2)
	}

	if c1 != 15 || c2 != 15 {
		t.Fatalf("expected both cursors at 15, got c1=%d c2=%d", c1, c2)
	}

}

// later may replace with a stress test

func TestManyListeners(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	b.Send([]byte("broadcast to all"))

	// allow all 1000 goroutines to finish before ending the test...
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cursor := 0
			msg, ok := b.Read(context.Background(), &cursor)
			if !ok || string(msg) != "broadcast to all" {
				t.Errorf("listener %d: got %q ok=%v", n, msg, ok)
			}
		}(i)
	}
	wg.Wait()
}

func TestCleanupOnDisconnect(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	r1, _ := http.Post(srv.URL+"/station?id=punk", "", nil)
	r1.Body.Close()

	// use a context we can cancel to simulate disconnect
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(
		ctx, "GET",
		srv.URL+"/station/listen?id=punk", nil)

	go http.DefaultClient.Do(req)

	time.Sleep(100 * time.Millisecond)

	// cancel simulates client disconnect
	cancel()

	// broadcast should still work (no panic, no hang)
	r, err := http.Post(
		srv.URL+"/station/broadcast?id=punk",
		"application/octet-stream",
		strings.NewReader("still alive"),
	)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	defer deleteStationHelper(srv, "punk")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}
}

// Note: tried to not make this overlap with TestCloseShutsDownWithoutDataLoss
// this has a reader blocked waiting for more data.
// I don't love it. we might want to save this for stress testing for the timing
// element
func TestServerDropsMidStream(t *testing.T) {
	b := NewBroadcaster()

	b.Send([]byte("first"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		cursor := 0

		// gets "first" fine
		msg, ok := b.Read(context.Background(), &cursor)
		if !ok || string(msg) != "first" {
			t.Errorf("expected 'first', got %q", msg)
			return
		}

		// blocks here waiting for more data. then
		// Close() wakes it up
		_, ok = b.Read(context.Background(), &cursor)
		if ok {
			t.Errorf("expected done after close")
		}
	}()

	// give reader time to block on second Read
	time.Sleep(50 * time.Millisecond)
	b.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader goroutine didn't exit after Close")
	}
}

func TestReadAfterClose(t *testing.T) {
	b := NewBroadcaster()

	b.Send([]byte("a"))
	b.Send([]byte("b"))
	b.Send([]byte("zed"))
	b.Close()

	// client shows up after the job is done
	cursor := 0
	var msgs []string
	for {
		msg, ok := b.Read(context.Background(), &cursor)
		if !ok {
			break
		}
		msgs = append(msgs, string(msg))
	}

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0] != "a" || msgs[1] != "b" || msgs[2] != "zed" {
		t.Fatalf("unexpected messages: %v", msgs)
	}
}

func TestDisconnectMidStream(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	b.Send([]byte("first"))

	ctx, cancel := context.WithCancel(context.Background())

	// read first message fine
	cursor := 0
	msg, ok := b.Read(ctx, &cursor)
	if !ok || string(msg) != "first" {
		t.Fatalf("expected 'first', got %q", msg)
	}

	// cancel before next message arrives
	cancel()

	// Read should return false, not block forever
	_, ok = b.Read(ctx, &cursor)
	if ok {
		t.Fatal("expected Read to return false after context cancel")
	}
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
	)
}
