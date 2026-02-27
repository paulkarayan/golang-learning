package main

import (
	"context"

	pb "snippetbox.paulkarayan.com/cmd/proto"
)

type grpcServer struct {
	pb.UnimplementedSnippetBoxServer
}

func (s *grpcServer) Home(ctx context.Context, req *pb.HomeRequest) (*pb.HomeResponse, error) {
	return &pb.HomeResponse{Message: "Hello from Snippetbox"}, nil
}

func (s *grpcServer) GetSnippet(ctx context.Context, req *pb.GetSnippetRequest) (*pb.GetSnippetResponse, error) {
	return &pb.GetSnippetResponse{
		Snippet: &pb.Snippet{
			Id:      req.Id,
			Title:   "placeholder",
			Content: "placeholder",
		},
	}, nil
}

func (s *grpcServer) CreateSnippet(ctx context.Context, req *pb.CreateSnippetRequest) (*pb.CreateSnippetResponse, error) {
	return &pb.CreateSnippetResponse{
		Snippet: &pb.Snippet{
			Title:   req.Title,
			Content: req.Content,
			Expires: req.Expires,
		},
	}, nil
}
