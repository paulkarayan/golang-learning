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

Approach: Use flag.NewFlagSet per subcommand — Go's flag package doesn't have built-in subcommand support, but
  you create a FlagSet for each (home, view, create) and switch on os.Args[1] to pick which set to parse.

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
