package main

import (
	"context"
	"crypto/tls"
	"net"
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
	ca := newTestCA(t)
	serverCert := ca.issueCert(t, "localhost", "localhost")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    ca.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterSnippetBoxServer(s, &grpcServer{})
	go func() { s.Serve(lis) }()
	t.Cleanup(func() { s.Stop() })

	clientCert := ca.issueCert(t, "admin", "localhost")
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      ca.pool,
		ServerName:   "localhost",
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

func TestGRPCInterceptorRejectsUserOnCreate(t *testing.T) {
	ca := newTestCA(t)
	serverCert := ca.issueCert(t, "localhost", "localhost")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    ca.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.UnaryInterceptor(roleInterceptor()),
	)
	pb.RegisterSnippetBoxServer(s, &grpcServer{})
	go func() { s.Serve(lis) }()
	t.Cleanup(func() { s.Stop() })

	// user cert, NOT admin
	clientCert := ca.issueCert(t, "user", "localhost")
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      ca.pool,
		ServerName:   "localhost",
	}

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewSnippetBoxClient(conn)

	// CreateSnippet should fail — user doesn't have admin role
	_, err = client.CreateSnippet(context.Background(), &pb.CreateSnippetRequest{
		Title: "test", Content: "test", Expires: 7,
	})
	if err == nil {
		t.Fatal("expected error, user should not be able to create")
	}

	// GetSnippet should succeed — user has user role
	_, err = client.GetSnippet(context.Background(), &pb.GetSnippetRequest{Id: 1})
	if err != nil {
		t.Fatalf("expected success for GetSnippet, got %v", err)
	}
}
