package main

import (
	"crypto/tls"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

func bearerAuthMiddleware(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "authorization requied", http.StatusUnauthorized)
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != token {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func main() {
	addr := flag.String("addr", ":4000", "HTTP network address")
	token := flag.String("token", "dev-hardcoded-secret", "bearer auth token")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	// just make it TLS 1.3
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519,
			tls.CurveP256},
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
	mux.HandleFunc("GET /snippet/view/{id}", bearerAuthMiddleware(*token, snippetView))
	mux.HandleFunc("GET /snippet/create", bearerAuthMiddleware(*token, snippetCreate))
	mux.HandleFunc("POST /snippet/create", bearerAuthMiddleware(*token, snippetCreatePost))

	logger.Info("starting server on", "addr", *addr)

	err := srv.ListenAndServeTLS("./cmd/tls/localhost.pem", "./cmd/tls/localhost-key.pem")
	logger.Error("handle error", "err", err)
}
