Commands & Flags:

  ┌─────────────────┬───────────────────────────────────────────┬─────────────────────────────────────────────┐
  │     Command     │                Description                │                    Flags                    │
  ├─────────────────┼───────────────────────────────────────────┼─────────────────────────────────────────────┤
  │ sbox home       │ Hit the home route (GET /)                │ --host (default localhost:4000)             │
  ├─────────────────┼───────────────────────────────────────────┼─────────────────────────────────────────────┤
  │ sbox view       │ View a snippet (GET /snippet/view/{id})   │ --id (required, int)                        │
  ├─────────────────┼───────────────────────────────────────────┼─────────────────────────────────────────────┤
  │ sbox create     │ Create a snippet (POST /snippet/create)   │ --title, --content, --expires (string       │
  │                 │                                           │ params)                                     │
  ├─────────────────┼───────────────────────────────────────────┼─────────────────────────────────────────────┤
  │ sbox            │ Show the create form (GET                 │ (none beyond --host)                        │
  │ list-create     │ /snippet/create)                          │                                             │
  └─────────────────┴───────────────────────────────────────────┴─────────────────────────────────────────────┘

Global flags (apply to all commands):
  - --host — server base URL, default http://localhost:4000
  - --verbose / -v — print full response headers + body

Structure:

  cmd/
    cli/
      main.go        // flag parsing, subcommand dispatch
      client.go      // shared HTTP client + helpers

Approach: Use flag.NewFlagSet per subcommand — Go's flag package doesn't have built-in subcommand support, but you create a FlagSet for each (home, view, create) and switch on os.Args[1] to pick which set to parse.

Usage would look like:
  sbox view --id 3
  sbox create --title "My Snippet" --content "fmt.Println()" --expires 7
  sbox home -v

# resources

https://gobyexample.com/command-line-subcommands
https://gobyexample.com/command-line-flags


# commands

cd snippetbox

# foo subcommand
go run ./cmd/cli/ foo
go run ./cmd/cli/ foo --enable --name "test"
go run ./cmd/cli/ foo --name "hello"

# bar subcommand
go run ./cmd/cli/ bar
go run ./cmd/cli/ bar --level 5
go run ./cmd/cli/ bar --level 5 things

# no args (error)
go run ./cmd/cli/

# bad subcommand (error)
go run ./cmd/cli/ blah



# testing

- args []string — instead of reading os.Args, the caller passes args in. In tests you pass []string{"view",
"--id", "3"}. In real usage, main() passes os.Args[1:].
- stdout io.Writer — instead of fmt.Println, you use fmt.Fprintln(stdout, ...). In tests you pass a bytes.Buffer
and check its contents. In real usage, main() passes os.Stdout.
- returns int — instead of os.Exit(1), you return 1. In tests you check the return value. In real usage, main()
calls os.Exit(run(...)).

go test ./cmd/cli/ -v

leaks!
go get go.uber.org/goleak

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}



# build

go build -o sbox ./cmd/cli/


--- TLS

file:///Users/pk/Desktop/golang/lets-go-professional-package.html/09.03-generating-a-self-signed-tls-certificate.html

for ease:
brew install mkcert
mkcert -install

mkcert defaults to a reasonable key size (EC P-256)

mkcert localhost

NOTE: for test certs, i'm fine not gitignoring them. but i would in production


go test ./cmd/server/ -v

gotta chmod? no its fine, i got the path wrong.


why did the tests pass???
httptest.NewTLSServer generates its own temporary certs, and ts.Client() trusts them automatically.

-- cipher suite

using  mozilla modern options

With TLS 1.3, Go uses all three and you can't change
  them:

  1. TLS_AES_128_GCM_SHA256
  2. TLS_AES_256_GCM_SHA384
  3. TLS_CHACHA20_POLY1305_SHA256

  Go picks which one at runtime based on hardware support.
   AES-GCM if the CPU has AES-NI (your Mac does), CHACHA20
   otherwise.

--- add Bearer auth

https://www.alexedwards.net/blog/basic-authentication-in-go
^^ follow a pattern like this

and then did a refactor...

sbox view --id 1 --token hardcode

Display a specific snippet with ID 1...

--- let's do mtls

gdi mkcert doesnt do client certs

CA is here:
/Users/pk/Library/Application Support/mkcert

eh let's just use openssl

cd cmd/tls

# CA (signs both server and client certs)
openssl req -newkey rsa:2048 -new -nodes -x509 -days 3650 \
  -out ca-cert.pem -keyout ca-key.pem \
  -subj "/CN=snippetbox-ca"

# Server cert (signed by CA)
openssl req -newkey rsa:2048 -new -nodes \
  -keyout server-key.pem -out server.csr \
  -subj "/CN=localhost"

openssl x509 -req -in server.csr -CA ca-cert.pem -CAkey ca-key.pem \
  -CAcreateserial -out server-cert.pem -days 3650

# Client cert (signed by same CA)
openssl req -newkey rsa:2048 -new -nodes \
  -keyout client-key.pem -out client.csr \
  -subj "/CN=sbox-cli"

openssl x509 -req -in client.csr -CA ca-cert.pem -CAkey ca-key.pem \
  -CAcreateserial -out client-cert.pem -days 3650


i keep getting emails despite these being test certs so add to gitignore...

-- let's add ability to differentiate two user types via cert



openssl req -newkey rsa:2048 -new -nodes -keyout client-admin-key.pem -out client-admin.csr -subj "/CN=admin"

openssl x509 -req -in client-admin.csr -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial -out client-admin-cert.pem -days 3650

openssl req -newkey rsa:2048 -new -nodes -keyout client-user-key.pem -out client-user.csr -subj "/CN=user"

openssl x509 -req -in client-user.csr -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial -out client-user-cert.pem -days 3650


--- grpc
we should be able to do both
  1. HTTP stays on :4000 as-is
  2. gRPC runs on :4001 (or whatever)

brew install protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go get google.golang.org/grpc

echo 'export PATH="$PATH:/Users/pk/go-wks/bin"' >> ~/.zshrc
source ~/.zshrc

protoc --version && protoc-gen-go --version && protoc-gen-go-grpc --version


# writing the .proto

message HomeRequest {}  <--- this is empty because its a bare request

protoc --go_out=. --go-grpc_out=. snippetbox.proto

uhh looks fine?

go get google.golang.org/grpc/test/bufconn

no relative imports?!?
pb "snippetbox/cmd/proto"


i dont have a SAN entry and i dont want to change it.
InsecureSkipVerify: true,

but that's lame

openssl req -new -x509 -key ./cmd/tls/server-key.pem -out ./cmd/tls/server-cert.pem -days 365 -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"

this was self signed gdi

openssl req -new -key cmd/tls/server-key.pem -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" -out cmd/tls/server.csr

openssl x509 -req -in cmd/tls/server.csr -CA cmd/tls/ca-cert.pem -CAkey cmd/tls/ca-key.pem -CAcreateserial -out cmd/tls/server-cert.pem -days 365 -extfile <(echo                        "subjectAltName=DNS:localhost,IP:127.0.0.1")
