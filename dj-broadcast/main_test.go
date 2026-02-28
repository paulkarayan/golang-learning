package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

	resp, err := http.Get(srv.URL + "/station/listen?id=punk")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// we observed this test hanging. so we need to deal with
	//  http.Get blocking until the server sends headers + starts the body

	done := make(chan string)
	go func() {
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		done <- string(buf[:n])
	}()

	body := <-done

	if !strings.Contains(body, "Time Bomb") {
		t.Fatalf("expected Time Bomb in history, got %q", body)
	}
	if !strings.Contains(body, "Ruby Soho") {
		t.Fatalf("expected Ruby Soho in history, got %q", body)
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

// tests the slow reader behavior happens
func TestSlowReader(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	_, ch := b.Subscribe()

	// send more than the buffer size (10)
	for i := 0; i < 15; i++ {
		b.Send([]byte(fmt.Sprintf("msg %d", i)))
	}

	// should only get 10 (buffer size), rest (5) are dropped
	count := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			count++
		default:
			// nothing left in buffer
			goto done
		}
	}
done:
	if count != 10 {
		t.Fatalf("expected 10 messages, got %d", count)
	}
}

// we expect that slow readers will not slow everyone else down

func TestSlowReaderDoesntBlockOthers(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	_, slow := b.Subscribe()
	_, fast := b.Subscribe()

	// fill slow reader's buffer
	for i := 0; i < 15; i++ {
		b.Send([]byte(fmt.Sprintf("msg %d", i)))
	}

	// fast reader drains immediately — should get 10
	count := 0
	// weird... but ok.
loop:
	for {
		select {
		case <-fast:
			count++
		default:
			break loop
		}
	}

	if count != 10 {
		t.Fatalf("fast reader expected 10, got %d", count)
	}

	// we wouldnt get to this point if slow reader blocked Send
	// but we're just suppressing the unused var error
	_ = slow
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

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
	)
}

func TestManyListeners(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	numListeners := 1000
	channels := make([]chan []byte, numListeners)

	for i := 0; i < numListeners; i++ {
		_, ch := b.Subscribe()
		channels[i] = ch
	}

	b.Send([]byte("broadcast to all"))

	for i, ch := range channels {
		select {
		case msg := <-ch:
			if string(msg) != "broadcast to all" {
				t.Fatalf("listener %d: expected 'broadcast to all', got %q", i, msg)
			}
		default:
			t.Fatalf("listener %d: no message received", i)
		}
	}
}

func TestServerDropsMidStream(t *testing.T) {
	b := NewBroadcaster()

	_, ch := b.Subscribe()

	b.Send([]byte("first"))

	// server kills the station
	b.Close()

	// listener should see the first message
	msg := <-ch
	if string(msg) != "first" {
		t.Fatalf("expected 'first', got %q", msg)
	}

	// next read should get closed channel
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed")
	}
}
