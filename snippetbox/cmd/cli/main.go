package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

// i have to change to this becuase otherwise args is global, os.Exit kills the process, and we dont capture stdout
func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, stdout io.Writer) int {

	//no args
	if len(args) < 1 {
		fmt.Fprintln(stdout, "expected 'foo' or 'bar' subcommands")
		return 1
	}

	switch args[0] {

	case "foo":
		fooCmd := flag.NewFlagSet("foo", flag.ExitOnError)
		fooEnable := fooCmd.Bool("enable", false, "enable")
		fooName := fooCmd.String("name", "", "name")

		fooCmd.Parse(args[1:])
		fmt.Fprintln(stdout, "subcommand 'foo'")
		fmt.Fprintln(stdout, "  enable:", *fooEnable)
		fmt.Fprintln(stdout, "  name:", *fooName)
		fmt.Fprintln(stdout, "  tail:", fooCmd.Args())

	case "bar":
		barCmd := flag.NewFlagSet("bar", flag.ExitOnError)
		barLevel := barCmd.Int("level", 0, "level")

		barCmd.Parse(args[1:])
		fmt.Fprintln(stdout, "subcommand 'bar'")
		fmt.Fprintln(stdout, "  level:", *barLevel)
		fmt.Fprintln(stdout, "  tail:", barCmd.Args())

	//  sbox home -v
	case "home":
		homeCmd := flag.NewFlagSet("home", flag.ExitOnError)
		host := homeCmd.String("host", "https://localhost:4000", "server host")
		verbose := homeCmd.Bool("v", false, "verbose output")
		homeCmd.Parse(args[1:])

		resp, err := http.Get(*host + "/")
		if err != nil {
			fmt.Fprintln(stdout, "error", err)
			return 1
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if *verbose {
			fmt.Fprintln(stdout, "Status:", resp.Status)
			for k, v := range resp.Header {
				fmt.Fprintf(stdout, "%s: %s\n", k, v)
			}
			fmt.Fprintln(stdout, "---")
		}
		fmt.Fprintln(stdout, string(body))

	//  sbox view --id 1
	case "view":
		viewCmd := flag.NewFlagSet("view", flag.ExitOnError)
		host := viewCmd.String("host", "https://localhost:4000", "server host")
		id := viewCmd.Int("id", 0, "snippet id")
		verbose := viewCmd.Bool("v", false, "verbose output")

		viewCmd.Parse(args[1:])

		//The flag methods (String, Int, Bool) return pointers
		//   because the values don't exist yet at declaration time —
		//   they get filled in when Parse runs. The pointer gives
		//  you a reference to where the value will be once parsing
		//  happens.
		//  That's why you dereference with *host, *id, etc. — to
		//  get the actual value after parsing.

		// remember id = 0 is default and will 404
		resp, err := http.Get(fmt.Sprintf("%s/snippet/view/%d", *host, *id))
		if err != nil {
			fmt.Fprintln(stdout, "error", err)
			return 1
		}

		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if *verbose {
			fmt.Fprintln(stdout, "Status:", resp.Status)
			for k, v := range resp.Header {
				fmt.Fprintf(stdout, "%s: %s\n", k, v)
			}
			fmt.Fprintln(stdout, "---")
		}
		fmt.Fprintln(stdout, string(body))

	case "create":
		createCmd := flag.NewFlagSet("create", flag.ExitOnError)
		host := createCmd.String("host", "https://localhost:4000", "server host")
		title := createCmd.String("title", "", "snippet title")
		content := createCmd.String("content", "", "snippet content")
		expires := createCmd.Int("expires", 7, "days until expiry")
		verbose := createCmd.Bool("v", false, "verbose output")

		createCmd.Parse(args[1:])

		data := struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Expires int    `json:"expires"`
		}{*title, *content, *expires}

		payload, err := json.Marshal(data)
		if err != nil {
			fmt.Fprintln(stdout, "error:", err)
			return 1
		}
		resp, err := http.Post(*host+"/snippet/create", "application/json", bytes.NewReader(payload))
		if err != nil {
			fmt.Fprintln(stdout, "error", err)
			return 1
		}

		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if *verbose {
			fmt.Fprintln(stdout, "Status:", resp.Status)
			for k, v := range resp.Header {
				fmt.Fprintf(stdout, "%s: %s\n", k, v)
			}
			fmt.Fprintln(stdout, "---")
		}
		fmt.Fprintln(stdout, string(body))

	// wrong args
	default:
		fmt.Fprintln(stdout, "expected 'foo' or 'bar' or 'home' or 'view' subcommands")
		return 1
	}
	return 0
}
