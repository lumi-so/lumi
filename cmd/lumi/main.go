package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "test":
		exitCode := cmdTest(args)
		os.Exit(exitCode)
	case "profile":
		exitCode := cmdProfile(args)
		os.Exit(exitCode)
	case "run":
		exitCode := cmdRun(args)
		os.Exit(exitCode)
	case "version":
		fmt.Printf("lumi %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`lumi — Lua development toolkit

Usage:
  lumi <command> [options] [arguments]

Commands:
  test      Run Lua test files
  profile   Profile a Lua script
  run       Run a Lua script (with optional debugging)
  version   Print version

Run 'lumi <command> -h' for details on each command.
`)
}
