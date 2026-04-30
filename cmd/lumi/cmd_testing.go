package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

func cmdTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	verbose := fs.Bool("v", false, "verbose output")
	runFilter := fs.String("run", "", "filter test names (substring match)")
	fs.Parse(args)

	// Determine test directory
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}

	// Find lib/ directory (for test.lua)
	libDir := findLibDir(dir)

	// Discover test files
	files := discoverTestFiles(dir)
	if len(files) == 0 {
		fmt.Println("No test files found.")
		fmt.Printf("Looking in: %s\n", dir)
		fmt.Println("Test files must match: test_*.lua or *_test.lua")
		return 0
	}

	fmt.Printf("Running %d test file(s)...\n\n", len(files))

	totalPassed := 0
	totalFailed := 0
	totalSkipped := 0
	start := time.Now()

	for _, file := range files {
		relPath, _ := filepath.Rel(dir, file)
		if relPath == "" {
			relPath = file
		}

		L := lua.NewState()

		// Setup package path for lib/
		if libDir != "" {
			setupPackagePath(L, libDir)
		}

		// Load test file
		if err := L.DoFile(file); err != nil {
			fmt.Printf("✗ %s — load error: %v\n", relPath, err)
			totalFailed++
			L.Close()
			continue
		}

		// Run tests
		err := L.DoString(`return require("lumi.test").run()`)
		if err != nil {
			fmt.Printf("✗ %s — run error: %v\n", relPath, err)
			totalFailed++
			L.Close()
			continue
		}

		// Parse results
		summary := parseTestResults(L, *runFilter)
		L.Close()

		// Print results
		filePassed := 0
		fileFailed := 0

		for _, r := range summary {
			if *runFilter != "" && !strings.Contains(r.Name, *runFilter) && !strings.Contains(r.Suite, *runFilter) {
				totalSkipped++
				continue
			}

			if r.Passed {
				filePassed++
				totalPassed++
				if *verbose {
					fmt.Printf("  ✓ %s/%s (%.1fms)\n", r.Suite, r.Name, float64(r.Duration)/float64(time.Millisecond))
				}
			} else {
				fileFailed++
				totalFailed++
				fmt.Printf("  ✗ %s/%s\n", r.Suite, r.Name)
				if r.Error != "" {
					lines := strings.Split(r.Error, "\n")
					for _, line := range lines {
						fmt.Printf("    %s\n", line)
					}
				}
			}
		}

		if fileFailed == 0 {
			fmt.Printf("✓ %s (%d passed)\n", relPath, filePassed)
		} else {
			fmt.Printf("✗ %s (%d passed, %d failed)\n", relPath, filePassed, fileFailed)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\n--- Results ---\n")
	fmt.Printf("Passed: %d, Failed: %d", totalPassed, totalFailed)
	if totalSkipped > 0 {
		fmt.Printf(", Skipped: %d", totalSkipped)
	}
	fmt.Printf(" (%.3fs)\n", elapsed.Seconds())

	if totalFailed > 0 {
		return 1
	}
	return 0
}

type testResult struct {
	Suite    string
	Name     string
	Passed   bool
	Error    string
	Duration time.Duration
}

func parseTestResults(L *lua.State, filter string) []testResult {
	if !L.IsTable(-1) {
		return nil
	}

	var results []testResult

	L.GetField(-1, "results")
	if L.IsTable(-1) {
		n := L.RawLen(-1)
		for i := int64(1); i <= n; i++ {
			L.RawGetI(-1, i)
			if L.IsTable(-1) {
				r := testResult{
					Suite:  getFieldString(L, -1, "suite"),
					Name:   getFieldString(L, -1, "name"),
					Passed: getFieldBool(L, -1, "passed"),
					Error:  getFieldString(L, -1, "error"),
				}
				dur := getFieldNumber(L, -1, "duration")
				r.Duration = time.Duration(dur * float64(time.Second))
				results = append(results, r)
			}
			L.Pop(1)
		}
	}
	L.Pop(1) // results table
	L.Pop(1) // main table

	return results
}

func setupPackagePath(L *lua.State, libDir string) {
	code := fmt.Sprintf(`package.path = %q .. "/?.lua;" .. %q .. "/?/init.lua;" .. package.path`, libDir, libDir)
	L.DoString(code)

	// Preload lumi.test
	testPath := filepath.Join(libDir, "test.lua")
	if _, err := os.Stat(testPath); err == nil {
		preload := fmt.Sprintf(`
			local f = loadfile(%q)
			if f then
				package.preload["lumi.test"] = f
				package.preload["test"] = f
			end
		`, testPath)
		L.DoString(preload)
	}
}

func discoverTestFiles(dir string) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".lua") {
			files = append(files, path)
		} else if strings.HasSuffix(name, "_test.lua") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func findLibDir(startDir string) string {
	dir, _ := filepath.Abs(startDir)
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "lib")
		if _, err := os.Stat(filepath.Join(candidate, "test.lua")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// Helper functions for reading Lua table fields
func getFieldString(L *lua.State, idx int, key string) string {
	L.GetField(idx, key)
	s, _ := L.ToString(-1)
	L.Pop(1)
	return s
}

func getFieldBool(L *lua.State, idx int, key string) bool {
	L.GetField(idx, key)
	b := L.ToBoolean(-1)
	L.Pop(1)
	return b
}

func getFieldNumber(L *lua.State, idx int, key string) float64 {
	L.GetField(idx, key)
	n, _ := L.ToNumber(-1)
	L.Pop(1)
	return n
}
