// Package main provides the what-ttft command-line interface.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

const usageText = `what-ttft benchmarks AI provider latency.

Usage:
  what-ttft --help

The benchmark implementation is under active development. See implementation-plan.md.
`

func main() {
	flag.Usage = printUsage

	if len(os.Args) == 1 {
		printUsage()
		return
	}

	if os.Args[1] == "--help" || os.Args[1] == "-h" {
		printUsage()
		return
	}

	flag.Parse()

	if flag.NArg() > 0 {
		log.Printf("unknown command %q", flag.Arg(0))
		os.Exit(2)
	}
}

func printUsage() {
	if _, err := fmt.Fprint(flag.CommandLine.Output(), usageText); err != nil {
		log.Printf("write usage: %v", err)
	}
}
