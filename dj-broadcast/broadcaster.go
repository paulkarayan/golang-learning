package main

import "sync"

// using mutex here to protect the station map.
type StationManager struct {
	mu       sync.Mutex
	stations map[string]*Broadcaster
}

// use the monitor goroutine pattern a la bank example
type Broadcaster struct {
	subscribeCh   chan subRequest
	unsubscribeCh chan int
	sendCh        chan []byte
	closeCh       chan struct{}
	history       [][]byte
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
func NewBroadcaster() *Broadcaster {
	// init channels
	// go b.run() — start the monitor goroutine, which runs until Close

}

func (b *Broadcaster) run() {
	subscribers := make(map[int]chan []byte)
	nextID := 0

	for {
		select {
		case req := <-b.subscribeCh:
			// assign id, make channel
			// send history
			// add to map
			// reply via req.resp

		case id := <-b.unsubscribeCh:
			// close channel, delete from map

		case data := <-b.sendCh:
			// append to history
			// send to all subscribers

		case <-b.closeCh:
			// close all subscriber channels
			// return (kills the goroutine)
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
