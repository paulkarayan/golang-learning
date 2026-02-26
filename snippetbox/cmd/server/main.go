package main

import (
	"crypto/tls"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", ":4000", "HTTP network address")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	// TLS 1.2 vs. default of 1.0
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		TLSConfig:    tlsConfig,
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	mux.HandleFunc("GET /{$}", home)
	mux.HandleFunc("GET /snippet/view/{id}", snippetView)
	mux.HandleFunc("GET /snippet/create", snippetCreate)
	mux.HandleFunc("POST /snippet/create", snippetCreatePost)

	logger.Info("starting server on", "addr", *addr)

	err := srv.ListenAndServeTLS("./tls/cert.pem", "./tls/key.pem")
	logger.Error("handle error", "err", err)
}
