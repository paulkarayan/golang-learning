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
	log.Fatal(http.ListenAndServe(":8080", srv)) //nolint:gosec // nosemgrep: go.lang.security.audit.net.use-tls.use-tls
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
		fmt.Fprintln(w, "station created:", id) //nolint:errcheck,gosec
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

		fmt.Fprintln(w, "station deleted:", id) //nolint:errcheck,gosec
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

		// we have to push the data in the handler buffer to the client
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}

		// this is the context bit to avoid a leak!
		// WE - the caller - must wake Read when client disconnects
		go func() {
			<-r.Context().Done()
			b.cond.Broadcast()
		}()

		cursor := 0
		for {
			data, ok := b.Read(r.Context(), &cursor)
			if !ok {
				return
			}
			if _, err := w.Write(data); err != nil { //nolint:gosec // raw audio stream, not HTML
				return
			}
			flusher.Flush()
		}
	}
}

// move the id check into SM to avoid TOCTOU error - TestTOCTOU_SendAfterStop

func (sm *StationManager) Send(id string, data []byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	b, ok := sm.stations[id]
	if !ok || b.done {
		return fmt.Errorf("station %s not found", id)
	}
	b.Send(data)
	return nil
}

func broadcast(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id",
				http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body",
				http.StatusBadRequest)
			return
		}
		if err := sm.Send(id, data); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}
}
