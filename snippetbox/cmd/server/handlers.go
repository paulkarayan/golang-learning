package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func home(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Server", "Go")
	w.Write([]byte("Hello from Snippetbox")) //nolint:errcheck
}

func snippetView(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}

	fmt.Fprintf(w, "Display a specific snippet with ID %d...", id) //nolint:errcheck
}

func snippetCreate(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Display a form for creating a new snippet...")) //nolint:errcheck
}

func snippetCreatePost(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Expires int    `json:"expires"`
	}

	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	// note: i had a bug here - the first write implicity sent at 200 status. i caught in tests!
	// fmt.Fprintf(w, ...) writes bytes to w, which is the http.ResponseWriter.   gdi
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Title: %s\nContent: %s\nExpires: %d\n", input.Title, input.Content, input.Expires) //nolint:errcheck
	// w.WriteHeader(http.StatusCreated)
}
