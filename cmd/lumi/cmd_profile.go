package main

import (
	"flag"
	"fmt"
	"os"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/lumi-so/lumi/pkg/luadebug"
)

func cmdProfile(args []string) int {
	fs := flag.NewFlagSet("profile", flag.ExitOnError)
	top := fs.Int("top", 20, "show top N functions")
	fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Usage: lumi profile [options] <script.lua>")
		return 1
	}

	file := fs.Arg(0)

	L := lua.NewState()
	defer L.Close()

	// Setup lib path
	libDir := findLibDir(".")
	if libDir != "" {
		setupPackagePath(L, libDir)
	}

	// Attach profiler
	p := luadebug.NewProfiler()
	p.Attach(L)

	// Run script
	if err := L.DoFile(file); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		p.Detach(L)
		return 1
	}

	p.Detach(L)

	// Print report
	report := p.Report()

	// Limit functions shown
	if len(report.Functions) > *top {
		report.Functions = report.Functions[:*top]
	}

	fmt.Print(report.String())
	return 0
}
