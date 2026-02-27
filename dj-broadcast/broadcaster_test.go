package main

import (
	"bytes"
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

func TestTwoSubscribers(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	_, ch1 := b.Subscribe()
	_, ch2 := b.Subscribe()

	b.Send([]byte("same message"))

	msg1 := <-ch1
	msg2 := <-ch2

	if string(msg1) != "same message" {
		t.Fatalf("ch1: expected 'same message', got %q", string(msg1))
	}
	if string(msg2) != "same message" {
		t.Fatalf("ch2: expected 'same message', got %q", string(msg2))
	}
}

func TestUnsubscribe(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	id, ch := b.Subscribe()
	b.Unsubscribe(id)

	// channel should be closed
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed")
	}
}

// note - we arent handling a case w/ large history
func TestLateSubscriberGetsHistory(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	b.Send([]byte("first"))
	b.Send([]byte("second"))

	_, ch := b.Subscribe()

	msg1 := <-ch
	msg2 := <-ch

	if string(msg1) != "first" {
		t.Fatalf("expected 'first', got %q", msg1)
	}
	if string(msg2) != "second" {
		t.Fatalf("expected 'second', got %q", msg2)
	}
}

// note: maybe use a fixture in future, for now thanks code gen for the suggestion
func TestBinaryData(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	_, ch := b.Subscribe()

	data := []byte{0x00, 0xFF, 0x89, 0x50, 0x4E, 0x47}
	b.Send(data)

	msg := <-ch
	if !bytes.Equal(msg, data) {
		t.Fatalf("expected %x, got %x", data, msg)
	}
}

func TestCloseShutsDownAll(t *testing.T) {
	b := NewBroadcaster()

	_, ch1 := b.Subscribe()
	_, ch2 := b.Subscribe()

	b.Close()
	// note: this wont work. because we're closing explicitly... it'd block
	// 4eva. ask me how i know.
	// defer b.Close()

	_, ok1 := <-ch1
	_, ok2 := <-ch2

	if ok1 {
		t.Fatal("expected ch1 to be closed")
	}
	if ok2 {
		t.Fatal("expected ch2 to be closed")
	}
}
