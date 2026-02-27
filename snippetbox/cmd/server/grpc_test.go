package main

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
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
