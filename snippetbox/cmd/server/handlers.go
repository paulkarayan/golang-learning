package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func home(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Server", "Go")
	if _, err := w.Write([]byte("Hello from Snippetbox")); err != nil {
		return
	}
}

func snippetView(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}

	fmt.Fprintf(w, "Display a specific snippet with ID %d...", id) //nolint:errcheck,gosec
}

func snippetCreate(w http.ResponseWriter, r *http.Request) {
	if _, err := w.Write([]byte("Display a form for creating a new snippet...")); err != nil {
		return
	}
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
	if _, err = fmt.Fprintf(w, "Title: %s\nContent: %s\nExpires: %d\n", input.Title, input.Content, input.Expires); err != nil {
		return
	}
	// w.WriteHeader(http.StatusCreated)
}
