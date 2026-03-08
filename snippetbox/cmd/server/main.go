package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	pb "snippetbox.paulkarayan.com/cmd/proto"
)

// func bearerAuthMiddleware(token string, next http.HandlerFunc) http.HandlerFunc {
// 	return func(w http.ResponseWriter, r *http.Request) {
// 		authHeader := r.Header.Get("Authorization")
// 		if authHeader == "" {
// 			http.Error(w, "authorization requied", http.StatusUnauthorized)
// 			return
// 		}
// 		parts := strings.SplitN(authHeader, " ", 2)
// 		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != token {
// 			http.Error(w, "invalid token", http.StatusUnauthorized)
// 			return
// 		}
// 		next(w, r)
// 	}
// }

// for extracting role + ensuring client cert in place
func requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "no client cert", http.StatusUnauthorized)
			return
		}
		cn := r.TLS.PeerCertificates[0].Subject.CommonName
		// admin also includes role of user... makes it easier to handle routes
		if cn != role && cn != "admin" {
			http.Error(w, "forbidden: requires "+role, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// roleForMethod maps gRPC method → required role, same as HTTP route → role.
func roleForMethod(fullMethod string) string {
	switch fullMethod {
	case "/snippetbox.SnippetBox/CreateSnippet":
		return "admin"
	default:
		return "user"
	}
}

// checkCertRole extracts the CN from the client cert in the context
// and verifies it matches the required role. Same logic as requireRole
// for HTTP, but we pull the cert from the context instead of r.TLS.
func checkCertRole(ctx context.Context, role string) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no peer info")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return status.Error(codes.Unauthenticated, "no client cert")
	}
	cn := tlsInfo.State.PeerCertificates[0].Subject.CommonName
	if cn != role && cn != "admin" {
		return status.Error(codes.PermissionDenied, "forbidden: requires "+role)
	}
	return nil
}

// unary interceptor — for request/response RPCs (Home, GetSnippet, CreateSnippet)
func roleInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {
		if err := checkCertRole(ctx, roleForMethod(info.FullMethod)); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// stream interceptor — for streaming RPCs (e.g. ListSnippets)
func streamRoleInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo,
		handler grpc.StreamHandler) error {
		if err := checkCertRole(ss.Context(), roleForMethod(info.FullMethod)); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func main() {
	addr := flag.String("addr", ":4000", "HTTP network address")
	// token := flag.String("token", "hardcode", "bearer auth token")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mux := http.NewServeMux()

	// load CA
	caCert, _ := os.ReadFile("./cmd/tls/ca-cert.pem")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// separeate certifcates for grpc
	serverCert, err := tls.LoadX509KeyPair(
		"./cmd/tls/server-cert.pem",
		"./cmd/tls/server-key.pem",
	)
	if err != nil {
		logger.Error("load server cert", "err", err)
		return
	}

	// just TLS 1.3
	tlsConfig := &tls.Config{
		Certificates:     []tls.Certificate{serverCert},
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
		ClientCAs:        caCertPool,
		ClientAuth:       tls.RequireAndVerifyClientCert,
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
	mux.HandleFunc("GET /snippet/view/{id}", requireRole("user", snippetView))
	mux.HandleFunc("GET /snippet/create", requireRole("user", snippetCreate))
	mux.HandleFunc("POST /snippet/create", requireRole("admin", snippetCreatePost))

	logger.Info("starting server on", "addr", *addr)

	// now grpc

	creds := credentials.NewTLS(tlsConfig)
	grpcSrv := grpc.NewServer(
		grpc.Creds(creds),
		grpc.UnaryInterceptor(roleInterceptor()),
		grpc.StreamInterceptor(streamRoleInterceptor()),
	)
	pb.RegisterSnippetBoxServer(grpcSrv, &grpcServer{})

	// we have to do this in goroutine so we can run second server wo blocking
	go func() {
		// "err" caught by linter even though its scoped to the goroutine but ill fix nonetheless
		lc := net.ListenConfig{}
		lis, lisErr := lc.Listen(context.Background(), "tcp", ":4001")
		if lisErr != nil {
			logger.Error("grpc listen", "err", lisErr)
			return
		}
		logger.Info("starting grpc server on", "addr", ":4001")
		if lisErr := grpcSrv.Serve(lis); lisErr != nil {
			logger.Error("grpc serve", "err", lisErr)
		}
	}()

	err = srv.ListenAndServeTLS("./cmd/tls/server-cert.pem", "./cmd/tls/server-key.pem")
	logger.Error("handle error", "err", err)
}
