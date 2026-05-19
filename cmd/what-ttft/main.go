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
  what-ttft bench --config benchmark.yaml [flags]

Commands:
  run      benchmark one OpenAI-compatible model from flags; Responses API is the OpenAI default
  bench    run a YAML benchmark across one or more targets
  version  print build version information
`

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

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
	case "bench":
		return benchCommand(args[1:], stdout, stderr)
	case "version":
		printVersion(stdout)
		return 0
	default:
		writeFormatted(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(output io.Writer) {
	writeText(output, usageText)
}

func printVersion(output io.Writer) {
	writeFormatted(output, "what-ttft %s\ncommit: %s\nbuilt: %s\n", version, commit, date)
}

func writeText(output io.Writer, text string) {
	if _, err := fmt.Fprint(output, text); err != nil {
		return
	}
}

func writeFormatted(output io.Writer, format string, args ...any) {
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
