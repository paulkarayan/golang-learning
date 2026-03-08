package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	pb "snippetbox.paulkarayan.com/cmd/proto"
)

// i have to change to this becuase otherwise args is global, os.Exit kills the process, and we dont capture stdout
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, nil))
}

func run(args []string, stdout io.Writer, client *http.Client) int {

	// no args
	if len(args) < 1 {
		fmt.Fprintln(stdout, "expected 'foo' or 'bar' subcommands") //nolint:errcheck,gosec
		return 1
	}

	switch args[0] {

	case "foo":
		fooCmd := flag.NewFlagSet("foo", flag.ExitOnError)
		fooEnable := fooCmd.Bool("enable", false, "enable")
		fooName := fooCmd.String("name", "", "name")

		if err := fooCmd.Parse(args[1:]); err != nil {
			fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
			return 1
		}
		fmt.Fprintln(stdout, "subcommand 'foo'")       //nolint:errcheck,gosec
		fmt.Fprintln(stdout, "  enable:", *fooEnable)  //nolint:errcheck,gosec
		fmt.Fprintln(stdout, "  name:", *fooName)      //nolint:errcheck,gosec
		fmt.Fprintln(stdout, "  tail:", fooCmd.Args()) //nolint:errcheck,gosec

	case "bar":
		barCmd := flag.NewFlagSet("bar", flag.ExitOnError)
		barLevel := barCmd.Int("level", 0, "level")

		if err := barCmd.Parse(args[1:]); err != nil {
			fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
			return 1
		}
		fmt.Fprintln(stdout, "subcommand 'bar'")       //nolint:errcheck,gosec
		fmt.Fprintln(stdout, "  level:", *barLevel)    //nolint:errcheck,gosec
		fmt.Fprintln(stdout, "  tail:", barCmd.Args()) //nolint:errcheck,gosec

	//  sbox home -v --role user
	case "home":
		homeCmd := flag.NewFlagSet("home", flag.ExitOnError)
		host := homeCmd.String("host", "https://localhost:4000", "server host")
		grpcHost := homeCmd.String("grpc-host", "localhost:4001", "grpc server address")
		role := homeCmd.String("role", "user", "user or admin")
		verbose := homeCmd.Bool("v", false, "verbose output")
		useHTTP := homeCmd.Bool("http", false, "use HTTP instead of gRPC")
		if err := homeCmd.Parse(args[1:]); err != nil {
			fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
			return 1
		}

		if *useHTTP {
			if client == nil {
				var err error
				client, err = clientForRole(*role, "./cmd/tls/ca-cert.pem", "./cmd/tls")
				if err != nil {
					fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
					return 1
				}
			}

			resp, err := makeRequest(context.Background(), client, "GET", *host+"/", nil)
			if err != nil {
				fmt.Fprintln(stdout, "error", err) //nolint:errcheck,gosec
				return 1
			}
			printResponse(resp, *verbose, stdout)
		} else {
			// do your grpc thing
			conn, err := grpcConnForRole(*role, "./cmd/tls/ca-cert.pem", "./cmd/tls", *grpcHost)
			if err != nil {
				fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
				return 1
			}
			defer conn.Close() //nolint:errcheck,gosec

			c := pb.NewSnippetBoxClient(conn)
			resp, err := c.Home(context.Background(), &pb.HomeRequest{})
			if err != nil {
				fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
				return 1
			}

			fmt.Fprintln(stdout, resp.Message) //nolint:errcheck,gosec
		}

	//  sbox view --id 1 --role admin
	case "view":
		viewCmd := flag.NewFlagSet("view", flag.ExitOnError)
		host := viewCmd.String("host", "https://localhost:4000", "server host")
		id := viewCmd.Int("id", 0, "snippet id")
		role := viewCmd.String("role", "user", "user or admin")
		verbose := viewCmd.Bool("v", false, "verbose output")

		if err := viewCmd.Parse(args[1:]); err != nil {
			fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
			return 1
		}

		// The flag methods (String, Int, Bool) return pointers
		//   because the values don't exist yet at declaration time —
		//   they get filled in when Parse runs. The pointer gives
		//  you a reference to where the value will be once parsing
		//  happens.
		//  That's why you dereference with *host, *id, etc. — to
		//  get the actual value after parsing.

		if client == nil {
			var err error
			client, err = clientForRole(*role, "./cmd/tls/ca-cert.pem", "./cmd/tls")
			if err != nil {
				fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
				return 1
			}
		}

		// remember id = 0 is default and will 404
		resp, err := makeRequest(context.Background(), client, "GET",
			fmt.Sprintf("%s/snippet/view/%d", *host, *id), nil)
		if err != nil {
			fmt.Fprintln(stdout, "error", err) //nolint:errcheck,gosec
			return 1
		}
		printResponse(resp, *verbose, stdout)

	case "create":
		createCmd := flag.NewFlagSet("create", flag.ExitOnError)
		host := createCmd.String("host", "https://localhost:4000", "server host")
		title := createCmd.String("title", "", "snippet title")
		content := createCmd.String("content", "", "snippet content")
		expires := createCmd.Int("expires", 7, "days until expiry")
		role := createCmd.String("role", "admin", "user or admin")
		verbose := createCmd.Bool("v", false, "verbose output")

		if err := createCmd.Parse(args[1:]); err != nil {
			fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
			return 1
		}

		if client == nil {
			var err error
			client, err = clientForRole(*role, "./cmd/tls/ca-cert.pem", "./cmd/tls")
			if err != nil {
				fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
				return 1
			}
		}

		data := struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Expires int    `json:"expires"`
		}{*title, *content, *expires}

		payload, err := json.Marshal(data)
		if err != nil {
			fmt.Fprintln(stdout, "error:", err) //nolint:errcheck,gosec
			return 1
		}
		resp, err := makeRequest(context.Background(), client, "POST", *host+"/snippet/create", bytes.NewReader(payload))
		if err != nil {
			fmt.Fprintln(stdout, "error", err) //nolint:errcheck,gosec
			return 1
		}

		printResponse(resp, *verbose, stdout)

	// wrong args
	default:
		fmt.Fprintln(stdout, "expected 'foo' or 'bar' or 'home' or 'view' or 'create' subcommands") //nolint:errcheck,gosec
		return 1
	}
	return 0
}
