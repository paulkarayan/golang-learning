package main

import (
	"fmt"
	"net/http"
)

func main() {
	srv := newServer()
	fmt.Println("listening on :8080")
	http.ListenAndServe(":8080", srv)
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

// like snippetbox,
func createStation(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// sm.Create(id)
	}
}

func deleteStation(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// sm.Stop(id)
	}
}

// connect client to the broadcaster
func subscribe(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// sm.Subscribe(id)
	}
}

func broadcast(sm *StationManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// sm.Send(id)
	}
}
