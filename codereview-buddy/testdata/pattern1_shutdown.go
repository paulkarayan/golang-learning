package testdata

import "sync"

// Pattern 1: Public methods called after Close/Shutdown
// BUG: Send() does not check if the server is closed.

type Server struct {
	mu      sync.Mutex
	done    bool
	clients []chan []byte
}

func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	for _, ch := range s.clients {
		close(ch)
	}
	s.clients = nil
}

// BUG: No check for s.done — will append to nil slice and send on closed channels.
func (s *Server) Send(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.clients {
		ch <- data
	}
}

func (s *Server) Subscribe() chan []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan []byte, 10)
	s.clients = append(s.clients, ch)
	return ch
}
