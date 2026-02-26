https://github.com/grpc/grpc-go/blob/master/examples/route_guide/routeguide/route_guide.proto

protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    routeguide/route_guide.proto


https://grpc.io/docs/guides/auth/
https://github.com/grpc/grpc-go/tree/master/examples/features/encryption # how i actually do it
