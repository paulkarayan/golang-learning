package main

import (
	"fmt"
	"sync"
)

// using mutex here to protect the station map.
type StationManager struct {
	mu       sync.Mutex
	stations map[string]*Broadcaster
}

func NewStationManager() *StationManager {
	return &StationManager{
		stations: make(map[string]*Broadcaster),
	}
}

func (sm *StationManager) Create(id string) error {
	// lock, check if exists,
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.stations[id]; ok {
		return fmt.Errorf("station %s already exists", id)
	}

	// create broadcaster
	sm.stations[id] = NewBroadcaster()
	// Go functions that return error return nil on success
	return nil
}

func (sm *StationManager) Get(id string) (*Broadcaster, bool) {
	// lock,
	sm.mu.Lock()
	defer sm.mu.Unlock()
	// lookup

	b, ok := sm.stations[id]
	return b, ok
}

func (sm *StationManager) Stop(id string) error {
	// lock

	sm.mu.Lock()
	defer sm.mu.Unlock()
	// close broadcaster after finding by id
	b, ok := sm.stations[id]
	if !ok {
		return fmt.Errorf("station %s not found", id)
	}
	b.Close()
	// delete from map
	delete(sm.stations, id)
	return nil

}

// use the monitor goroutine pattern a la bank example
type Broadcaster struct {
	subscribeCh   chan subRequest
	unsubscribeCh chan int
	sendCh        chan []byte
	closeCh       chan struct{}
}

type subRequest struct {
	resp chan subResponse
}

type subResponse struct {
	id int
	ch chan []byte
}

// the goroutine (monitor) here is the only one that reads from channels, touches
// the state in the station map and history
// NOTE: see the history comment above. i'm not correct.
func NewBroadcaster() *Broadcaster {
	b := &Broadcaster{
		subscribeCh:   make(chan subRequest),
		unsubscribeCh: make(chan int),
		sendCh:        make(chan []byte),
		closeCh:       make(chan struct{}, 1), //lets avoid the double close problem. make this a non-blocking send
	}
	// go b.run() — start the monitor goroutine, which runs until Close
	go b.run()
	return b
}

func (b *Broadcaster) run() {
	subscribers := make(map[int]chan []byte)
	// note: var is more idiomatic if you dont need initialize w values apparently
	history := [][]byte{}
	nextID := 0

	for {
		select {
		case req := <-b.subscribeCh:
			// assign id
			id := nextID
			nextID++
			// make channel
			// aribtary 10 message buffer, would tune for slow client behavior
			ch := make(chan []byte, 10)
			// send history
			for _, h := range history {
				ch <- h
			}

			// add to map and reply via req.resp
			subscribers[id] = ch
			req.resp <- subResponse{id: id, ch: ch}
			// fmt.Printf("subscribers: %v, history len: %d\n", subscribers, len(history))

		case id := <-b.unsubscribeCh:
			// close channel, delete from map
			if ch, ok := subscribers[id]; ok {
				// we can't double close because monitor pattern, so suppress
				close(ch) // nosemgrep: channel-close-without-once
				delete(subscribers, id)
			}

		case data := <-b.sendCh:
			// append to history
			// note that the ring buffer below handles the case but semgrep still spots it, so suppress
			history = append(history, data) // nosemgrep: unbounded-append-in-loop
			// add a ring buffer so this doesnt grow unbounded. thx semgrep!
			if len(history) > 100 {
				history = history[1:]
			}
			// send to all subscribers
			for _, ch := range subscribers {
				select {
				case ch <- data:
				default:
					// drop if channel is full. how do we wanna handle?
				}
			}

		case <-b.closeCh:
			// close all subscriber channels
			for _, ch := range subscribers {
				// we cant double close bc monitor pattern, so suppress semgrep
				close(ch) // nosemgrep: channel-close-without-once
			}
			// return (kills the _monitor_ goroutine)
			return
		}
	}
}

func (b *Broadcaster) Subscribe() (id int, ch chan []byte) {
	// send subRequest, wait for response
	req := subRequest{resp: make(chan subResponse)}
	b.subscribeCh <- req
	res := <-req.resp
	return res.id, res.ch
}

func (b *Broadcaster) Unsubscribe(id int) {
	// send id to unsubscribeCh
	b.unsubscribeCh <- id
}

func (b *Broadcaster) Send(data []byte) {
	// send data to sendCh
	b.sendCh <- data
}

func (b *Broadcaster) Close() {
	// send to closeCh
	// use the buffered channel of size 1, non-blocking send
	// "signal at most once" channels
	select {
	case b.closeCh <- struct{}{}: // apparently this is a convention for signal only channel as 0 byte
	default:
	}
}
