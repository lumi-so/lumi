package log

import (
	"encoding/json"
	"io"
	"os"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// Level represents a log level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Config configures the log module.
type Config struct {
	// Output writer (default: os.Stderr)
	Output io.Writer

	// MinLevel filters logs below this level (default: LevelInfo)
	MinLevel Level

	// Fields are added to every log entry (e.g., {"service": "myapp"})
	Fields map[string]any
}

// Register registers the "log" module in the Lua state.
func Register(L *lua.State, cfg Config) {
	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}

	lua.RegisterModule(L, "log", map[string]lua.Function{
		"debug": lua.WrapSafe(logFunc(cfg, LevelDebug)),
		"info":  lua.WrapSafe(logFunc(cfg, LevelInfo)),
		"warn":  lua.WrapSafe(logFunc(cfg, LevelWarn)),
		"error": lua.WrapSafe(logFunc(cfg, LevelError)),
	})
}

func logFunc(cfg Config, level Level) lua.Function {
	return func(L *lua.State) int {
		if level < cfg.MinLevel {
			return 0
		}

		msg := L.CheckString(1)

		// Build log entry
		entry := make(map[string]any)

		// Add static fields
		for k, v := range cfg.Fields {
			entry[k] = v
		}

		// Add fields from second argument (optional table)
		if L.GetTop() >= 2 && L.IsTable(2) {
			L.ForEach(2, func(inner *lua.State) bool {
				k, _ := inner.ToString(-2)
				v := inner.ToAny(-1)
				entry[k] = v
				return true
			})
		}

		// Standard fields (override any user-supplied "level", "msg", "time")
		entry["level"] = levelString(level)
		entry["msg"] = msg
		entry["time"] = time.Now().UTC().Format(time.RFC3339)

		// Encode and write
		data, _ := json.Marshal(entry)
		data = append(data, '\n')
		cfg.Output.Write(data)

		return 0
	}
}

func levelString(l Level) string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}
