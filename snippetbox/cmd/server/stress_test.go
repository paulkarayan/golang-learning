//go:build stress

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "snippetbox.paulkarayan.com/cmd/proto"
)

// Hammer HTTP handlers concurrently — handlers must be safe for concurrent use
func TestStress_ConcurrentHTTPHandlers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", home)
	mux.HandleFunc("GET /snippet/view/{id}", snippetView)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(ts.URL + "/")
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(ts.URL + "/snippet/view/1")
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
}

// Hammer gRPC server concurrently — all methods at once
func TestStress_ConcurrentGRPC(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	pb.RegisterSnippetBoxServer(s, &grpcServer{})
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Error(err)
		}
	}()
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

	client := pb.NewSnippetBoxClient(conn)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(3)

		go func() {
			defer wg.Done()
			_, err := client.Home(context.Background(), &pb.HomeRequest{})
			if err != nil {
				t.Error(err)
			}
		}()

		go func() {
			defer wg.Done()
			_, err := client.GetSnippet(context.Background(), &pb.GetSnippetRequest{Id: 1})
			if err != nil {
				t.Error(err)
			}
		}()

		go func() {
			defer wg.Done()
			_, err := client.CreateSnippet(context.Background(), &pb.CreateSnippetRequest{
				Title: "stress", Content: "test", Expires: 7,
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// Rapid connect/disconnect cycles — catches connection leak or goroutine leak
func TestStress_RapidGRPCConnections(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	pb.RegisterSnippetBoxServer(s, &grpcServer{})
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() { s.Stop() })

	for i := 0; i < 50; i++ {
		conn, err := grpc.NewClient("passthrough:///bufconn",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatal(err)
		}

		client := pb.NewSnippetBoxClient(conn)
		_, err = client.Home(context.Background(), &pb.HomeRequest{})
		if err != nil {
			t.Error(err)
		}
		conn.Close()
	}
}
