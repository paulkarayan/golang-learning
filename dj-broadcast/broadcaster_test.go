package main

import (
	"bytes"
	"context"
	"testing"
)

func TestBroadcasterSendReceive(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	b.Send([]byte("ahoy hoy"))

	cursor := 0

	msg, ok := b.Read(context.Background(), &cursor)

	if !ok {
		t.Fatal("expected data, got done")
	}
	if string(msg) != "ahoy hoy" {
		t.Fatalf("expected 'ahoy hoy', got %q", string(msg))
	}
}

func TestTwoSubscribers(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	b.Send([]byte("same message"))

	client1, client2 := 0, 0
	msg1, _ := b.Read(context.Background(), &client1)
	msg2, _ := b.Read(context.Background(), &client2)

	if string(msg1) != "same message" {
		t.Fatalf("ch1: expected 'same message', got %q", string(msg1))
	}
	if string(msg2) != "same message" {
		t.Fatalf("ch2: expected 'same message', got %q", string(msg2))
	}
}

// note - we arent handling a case w/ large history
func TestLateSubscriberGetsHistory(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	b.Send([]byte("first"))
	b.Send([]byte("second"))

	cursor := 0
	msg1, _ := b.Read(context.Background(), &cursor)
	msg2, _ := b.Read(context.Background(), &cursor)

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

	data := []byte{0x00, 0xFF, 0x89, 0x50, 0x4E, 0x47}
	b.Send(data)

	cursor := 0
	msg, _ := b.Read(context.Background(), &cursor)
	if !bytes.Equal(msg, data) {
		t.Fatalf("expected %x, got %x", data, msg)
	}
}

func TestCloseShutsDownWithoutDataLoss(t *testing.T) {
	b := NewBroadcaster()
	b.Send([]byte("made it before the bell"))
	// we explicitly call this. no Defer
	b.Close()

	cursor := 0
	msg, ok := b.Read(context.Background(), &cursor)

	// history has survived close
	if !ok || string(msg) != "made it before the bell" {
		t.Fatalf("expected 'made it before the bell', got %q (ok=%v)", msg, ok)
	}

	// there is nothing left after read
	_, ok = b.Read(context.Background(), &cursor)
	if ok {
		t.Fatal("expected done after draining")
	}
}

func TestStationManagerCreate(t *testing.T) {
	sm := NewStationManager()
	err := sm.Create("rock")
	if err != nil {
		t.Fatal(err)
	}
	defer sm.Stop("rock")
	b, ok := sm.Get("rock")
	if !ok {
		t.Fatal("expected station to exist")
	}
	if b == nil {
		t.Fatal("expected broadcaster, got nil")
	}
}

func TestStationManagerDuplicateCreate(t *testing.T) {
	sm := NewStationManager()
	sm.Create("rock")
	defer sm.Stop("rock")
	err := sm.Create("rock")
	if err == nil {
		t.Fatal("expected error for duplicate station")
	}
}

func TestStationManagerStop(t *testing.T) {
	sm := NewStationManager()
	sm.Create("rock")
	err := sm.Stop("rock")
	if err != nil {
		t.Fatal(err)
	}
	_, ok := sm.Get("rock")
	if ok {
		t.Fatal("expected station to be gone")
	}
}

func TestStationManagerGetNonexistent(t *testing.T) {
	sm := NewStationManager()
	_, ok := sm.Get("nope")
	if ok {
		t.Fatal("expected station to not exist")
	}
}

// TOCTOU on subscribe() - found by AI
// 1. Get() returns a pointer to broadcaster
// 2. Stop() closes it
// 3. active subscriber gets silently disconnected mid-stream.
// This test FAILS — proving the bug exists.
func TestTOCTOU_SubscriberSilentDisconnect(t *testing.T) {
	sm := NewStationManager()
	sm.Create("radio")

	// Simulate subscribe() handler: get the pointer, start reading
	b, ok := sm.Get("radio")
	if !ok {
		t.Fatal("station should exist")
	}

	b.Send([]byte("msg1"))
	b.Send([]byte("msg2"))

	// Subscriber reads first message
	cursor := 0
	_, ok = b.Read(context.Background(), &cursor)
	if !ok {
		t.Fatal("should get first message")
	}

	// Concurrent DELETE /station while subscriber is mid-stream
	sm.Stop("radio")

	// Subscriber tries to read msg2 — it's in history, so this works
	_, ok = b.Read(context.Background(), &cursor)
	if !ok {
		t.Fatal("msg2 was already buffered, should still be readable")
	}

	// But any future data is impossible. The subscriber is kicked off
	// with no indication that the station was deleted vs. naturally ended.
	_, ok = b.Read(context.Background(), &cursor)
	if !ok {
		t.Fatal("TOCTOU: subscriber silently disconnected — cannot distinguish deletion from normal shutdown")
	}
}
