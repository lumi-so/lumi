package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/lumi-so/lumi/pkg/luadebug"
)

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	debug := fs.Bool("debug", false, "attach interactive debugger")
	breakpoints := fs.String("b", "", "breakpoints (file:line,file:line,...)")
	fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Usage: lumi run [options] <script.lua> [args...]")
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

	// Set script arguments as global 'arg' table
	luaArgs := fs.Args()
	setLuaArgs(L, file, luaArgs)

	// Attach debugger if requested
	if *debug || *breakpoints != "" {
		dbg := luadebug.NewDebugger()

		// Parse breakpoints
		if *breakpoints != "" {
			for _, bp := range strings.Split(*breakpoints, ",") {
				parts := strings.SplitN(strings.TrimSpace(bp), ":", 2)
				if len(parts) == 2 {
					line, err := strconv.Atoi(parts[1])
					if err == nil {
						dbg.SetBreakpoint(parts[0], line)
						fmt.Printf("Breakpoint: %s:%d\n", parts[0], line)
					}
				}
			}
		}

		dbg.Attach(L)
		defer dbg.Detach(L)

		if *debug {
			fmt.Println("Debugger attached. Type 'h' for help.")
		}
	}

	// Run script
	if err := L.DoFile(file); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	return 0
}

func setLuaArgs(L *lua.State, script string, args []string) {
	L.CreateTable(len(args), 1)

	// arg[0] = script name
	L.PushString(script)
	L.RawSetI(-2, 0)

	// arg[1], arg[2], ... = additional arguments
	for i := 1; i < len(args); i++ {
		L.PushString(args[i])
		L.RawSetI(-2, int64(i))
	}

	L.SetGlobal("arg")
}
