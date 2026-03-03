package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	srv := newServer()
	fmt.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", srv)) // nosemgrep: go.lang.security.audit.net.use-tls.use-tls
}

func newServer() http.Handler {
	sm := NewStationManager()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /station", createStation(sm))
	mux.HandleFunc("POST /station/broadcast", broadcast(sm))
	mux.HandleFunc("GET /station/listen", subscribe(sm))
	mux.HandleFunc("DELETE /station", deleteStation(sm))

	return mux
}

// like snippetbox...
func createStation(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		err := sm.Create(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		// remember Go returns a 200 unless we change it
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintln(w, "station created:", id) //nolint:errcheck
	}
}

func deleteStation(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		err := sm.Stop(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		fmt.Fprintln(w, "station deleted:", id) //nolint:errcheck
	}
}

// connect client to the broadcaster
func subscribe(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		b, ok := sm.Get(id)
		if !ok {
			http.Error(w, "station not found", http.StatusNotFound)
			return
		}
		subID, ch := b.Subscribe()
		defer b.Unsubscribe(subID)

		// we have to push the data in the handler buffer to the client
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return // station closed
				}
				w.Write(msg) // nosemgrep: go.net.xss.no-direct-write-to-responsewriter-taint.no-direct-write-to-responsewriter-taint
				flusher.Flush()
			case <-r.Context().Done():
				return // client disconnected
			}
		}
	}
}

func broadcast(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		b, ok := sm.Get(id)
		if !ok {
			http.Error(w, "station not found", http.StatusNotFound)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		b.Send(data)
	}
}
