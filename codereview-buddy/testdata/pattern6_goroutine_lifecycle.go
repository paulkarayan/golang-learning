package testdata

import (
	"fmt"
	"net"
)

// Pattern 6: Fire-and-forget goroutine with no termination path
// BUG: The goroutine has no way to be stopped. If Accept() blocks
// indefinitely (or the listener is closed), this goroutine leaks.

type Listener struct {
	ln net.Listener
}

func (l *Listener) Start() {
	go func() {
		for {
			conn, err := l.ln.Accept()
			if err != nil {
				fmt.Println("accept error:", err)
				continue // BUG: never breaks out of loop
			}
			go func() {
				// handle connection with no coordination
				buf := make([]byte, 1024)
				conn.Read(buf)
				conn.Close()
			}()
		}
	}()
}
