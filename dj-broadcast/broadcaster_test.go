package main

import "testing"

func TestBroadcasterSendReceive(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	id, ch := b.Subscribe()
	_ = id

	b.Send([]byte("ahoy hoy"))

	msg := <-ch
	if string(msg) != "ahoy hoy" {
		t.Fatalf("expected 'ahoy hoy', got %q", string(msg))
	}
}
