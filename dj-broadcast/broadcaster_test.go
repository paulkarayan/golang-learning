package main

import (
	"fmt"
	"testing"
)

func TestBroadcasterSendReceive(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	id, ch := b.Subscribe()
	_ = id
	fmt.Println("subscribed with id:", id, "ch:", ch)

	b.Send([]byte("ahoy hoy"))

	msg := <-ch
	if string(msg) != "ahoy hoy" {
		t.Fatalf("expected 'ahoy hoy', got %q", string(msg))
	}
}
