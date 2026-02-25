package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// i have to change to this becuase otherwise args is global, os.Exit kills the process, and we dont capture stdout
func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, stdout io.Writer) int {

	fooCmd := flag.NewFlagSet("foo", flag.ExitOnError)
	fooEnable := fooCmd.Bool("enable", false, "enable")
	fooName := fooCmd.String("name", "", "name")

	barCmd := flag.NewFlagSet("bar", flag.ExitOnError)
	barLevel := barCmd.Int("level", 0, "level")

	if len(args) < 1 {
		fmt.Fprintln(stdout, "expected 'foo' or 'bar' subcommands")
		return 1
	}

	switch args[0] {

	case "foo":
		fooCmd.Parse(args[1:])
		fmt.Fprintln(stdout, "subcommand 'foo'")
		fmt.Fprintln(stdout, "  enable:", *fooEnable)
		fmt.Fprintln(stdout, "  name:", *fooName)
		fmt.Fprintln(stdout, "  tail:", fooCmd.Args())
	case "bar":
		barCmd.Parse(args[1:])
		fmt.Fprintln(stdout, "subcommand 'bar'")
		fmt.Fprintln(stdout, "  level:", *barLevel)
		fmt.Fprintln(stdout, "  tail:", barCmd.Args())
	default:
		fmt.Fprintln(stdout, "expected 'foo' or 'bar' subcommands")
		return 1
	}
	return 0
}
