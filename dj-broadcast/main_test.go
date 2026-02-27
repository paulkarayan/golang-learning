package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBroadcastAndListen(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	http.Post(srv.URL+"/station?id=punk", "", nil)

	// broadcast first
	http.Post(
		srv.URL+"/station/broadcast?id=punk",
		"application/octet-stream",
		strings.NewReader("Now playing: Time Bomb"),
	)

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

	http.Post(srv.URL+"/station?id=punk", "", nil)

	http.Post(srv.URL+"/station/broadcast?id=punk",
		"application/octet-stream",
		strings.NewReader("Now playing: Time Bomb"))
	http.Post(srv.URL+"/station/broadcast?id=punk",
		"application/octet-stream",
		strings.NewReader("Now playing: Ruby Soho"))

	resp, err := http.Get(srv.URL + "/station/listen?id=punk")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

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
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateDuplicateStation(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	http.Post(srv.URL+"/station?id=punk", "", nil)
	resp, err := http.Post(srv.URL+"/station?id=punk", "", nil)
	if err != nil {
		t.Fatal(err)
	}
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
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
