// Package main provides the what-ttft command-line interface.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

const usageText = `what-ttft benchmarks AI provider latency.

Usage:
  what-ttft --help
  what-ttft run [flags]

Commands:
  run    benchmark an OpenAI-compatible streaming Chat Completions endpoint
`

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout, stderr)
	default:
		writeFormatted(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(output io.Writer) {
	writeText(output, usageText)
}

func writeText(output io.Writer, text string) {
	if _, err := fmt.Fprint(output, text); err != nil {
		return
	}
}

func writeFormatted(output io.Writer, format string, args ...any) {
	//nolint:gosec // CLI call sites pass constant format strings; user values are provided only as arguments.
	if _, err := fmt.Fprintf(output, format, args...); err != nil {
		return
	}
}

func writeLine(output io.Writer, text string) {
	if _, err := fmt.Fprintln(output, text); err != nil {
		return
	}
}

func newFlagSet(name string, output io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(output)
	return set
}
