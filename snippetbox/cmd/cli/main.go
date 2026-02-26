package main

import (
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
	// returns something like
	case "home":
		homeCmd := flag.NewFlagSet("home", flag.ExitOnError)
		host := homeCmd.String("host", "http://localhost:4000", "server host")
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

	// wrong args
	default:
		fmt.Fprintln(stdout, "expected 'foo' or 'bar' subcommands")
		return 1
	}
	return 0
}
