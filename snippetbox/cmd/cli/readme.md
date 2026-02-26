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
