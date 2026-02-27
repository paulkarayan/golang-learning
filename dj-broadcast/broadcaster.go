package main

import (
	"sync"
)

// using mutex here to protect the station map.
type StationManager struct {
	mu       sync.Mutex
	stations map[string]*Broadcaster
}

func NewStationManager() *StationManager {
	// init map
}

func (sm *StationManager) Create(id string) error {
	// lock, check if exists, create broadcaster
}

func (sm *StationManager) Get(id string) (*Broadcaster, bool) {
	// lock, lookup
}

func (sm *StationManager) Stop(id string) error {
	// lock, close broadcaster, delete from map
}

// use the monitor goroutine pattern a la bank example
type Broadcaster struct {
	subscribeCh   chan subRequest
	unsubscribeCh chan int
	sendCh        chan []byte
	closeCh       chan struct{}
	// claude points out that this should be in run() because other goroutines could
	// access it here. but they wont, and i have agency. lol
	history [][]byte
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
		closeCh:       make(chan struct{}),
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
			req.resp <- subResponse{id: id, ch: ch}

		case id := <-b.unsubscribeCh:
			// close channel, delete from map
			if ch, ok := subscribers[id]; ok {
				close(ch)
				delete(subscribers, id)
			}

		case data := <-b.sendCh:
			// append to history
			history = append(history, data)
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
				close(ch)
			}
			// return (kills the _monitor_ goroutine)
			return
		}
	}
}

func (b *Broadcaster) Subscribe() (int, chan []byte) {
	// send subRequest, wait for response
}

func (b *Broadcaster) Unsubscribe(id int) {
	// send id to unsubscribeCh
}

func (b *Broadcaster) Send(data []byte) {
	// send data to sendCh
}

func (b *Broadcaster) Close() {
	// send to closeCh
}
