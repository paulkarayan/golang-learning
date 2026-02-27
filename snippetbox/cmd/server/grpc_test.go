package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "snippetbox.paulkarayan.com/cmd/proto"
)

func startGRPCServer(t *testing.T) pb.SnippetBoxClient {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	pb.RegisterSnippetBoxServer(s, &grpcServer{})

	go func() {
		if err := s.Serve(lis); err != nil {
			t.Error(err)
		}
	}()
	// this is the defer equivalent for test cleanup
	t.Cleanup(func() { s.Stop() })

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	return pb.NewSnippetBoxClient(conn)
}

func TestGRPCHome(t *testing.T) {
	client := startGRPCServer(t)
	resp, err := client.Home(context.Background(), &pb.HomeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message != "Hello from Snippetbox" {
		t.Fatalf("expected 'Hello from Snippetbox', got %q", resp.Message)
	}
}

func TestGRPCGetSnippet(t *testing.T) {
	client := startGRPCServer(t)
	resp, err := client.GetSnippet(context.Background(), &pb.GetSnippetRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Snippet.Id != 1 {
		t.Fatalf("expected id 1, got %d", resp.Snippet.Id)
	}
}

func TestGRPCCreateSnippet(t *testing.T) {
	client := startGRPCServer(t)
	resp, err := client.CreateSnippet(context.Background(), &pb.CreateSnippetRequest{
		Title:   "Wasabi",
		Content: "wasabi with you?",
		Expires: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Snippet.Title != "Wasabi" {
		t.Fatalf("expected 'Wasabi', got %q", resp.Snippet.Title)
	}
}

func TestGRPCWithMTLS(t *testing.T) {
	// load CA
	caCert, _ := os.ReadFile("../tls/ca-cert.pem")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// server TLS config — require client certs
	serverCert, _ := tls.LoadX509KeyPair("../tls/server-cert.pem", "../tls/server-key.pem")
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	// start gRPC server. localhost:0 picks a random free port
	lis, _ := net.Listen("tcp", "localhost:0")
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterSnippetBoxServer(s, &grpcServer{})
	go func() { s.Serve(lis) }()
	t.Cleanup(func() { s.Stop() })

	// client with valid cert
	clientCert, _ := tls.LoadX509KeyPair("../tls/client-admin-cert.pem",
		"../tls/client-admin-key.pem")
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		// we had to add this because
		// cert's SAN has the hostname localhost but the TLS client
		// is verifying against the IP 127.0.0.1
		ServerName: "localhost",
	}

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewSnippetBoxClient(conn)
	resp, err := client.Home(context.Background(), &pb.HomeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message != "Hello from Snippetbox" {
		t.Fatalf("expected 'Hello from Snippetbox', got %q", resp.Message)
	}
}
