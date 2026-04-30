package log

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
)

func TestLogInfo(t *testing.T) {
	var buf bytes.Buffer
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{Output: &buf})

	err := L.DoString(`
		local log = require("log")
		log.info("hello world", {user_id = 42})
	`)
	if err != nil {
		t.Fatal(err)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if entry["level"] != "info" {
		t.Errorf("level = %v, want info", entry["level"])
	}
	if entry["msg"] != "hello world" {
		t.Errorf("msg = %v, want 'hello world'", entry["msg"])
	}
	if entry["user_id"] != float64(42) {
		t.Errorf("user_id = %v, want 42", entry["user_id"])
	}
}

func TestLogLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{Output: &buf, MinLevel: LevelWarn})

	err := L.DoString(`
		local log = require("log")
		log.debug("should not appear")
		log.info("should not appear")
		log.warn("this should appear")
	`)
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "this should appear") {
		t.Error("warn message should appear")
	}
	if strings.Contains(output, "should not appear") {
		t.Error("debug/info should be filtered")
	}
}

func TestLogStaticFields(t *testing.T) {
	var buf bytes.Buffer
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Output: &buf,
		Fields: map[string]any{"service": "test-app", "version": "1.0"},
	})

	err := L.DoString(`
		local log = require("log")
		log.info("test")
	`)
	if err != nil {
		t.Fatal(err)
	}

	var entry map[string]any
	json.Unmarshal(buf.Bytes(), &entry)

	if entry["service"] != "test-app" {
		t.Errorf("service = %v, want test-app", entry["service"])
	}
	if entry["version"] != "1.0" {
		t.Errorf("version = %v, want 1.0", entry["version"])
	}
}

func TestLogNoFields(t *testing.T) {
	var buf bytes.Buffer
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{Output: &buf})

	err := L.DoString(`
		local log = require("log")
		log.error("something broke")
	`)
	if err != nil {
		t.Fatal(err)
	}

	var entry map[string]any
	json.Unmarshal(buf.Bytes(), &entry)
	if entry["level"] != "error" {
		t.Errorf("level = %v, want error", entry["level"])
	}
	if entry["msg"] != "something broke" {
		t.Errorf("msg = %v", entry["msg"])
	}
}

func TestLogDebugWithMinLevelDebug(t *testing.T) {
	var buf bytes.Buffer
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{Output: &buf, MinLevel: LevelDebug})

	err := L.DoString(`
		local log = require("log")
		log.debug("debug message", {detail = "verbose"})
	`)
	if err != nil {
		t.Fatal(err)
	}

	var entry map[string]any
	json.Unmarshal(buf.Bytes(), &entry)
	if entry["level"] != "debug" {
		t.Errorf("level = %v, want debug", entry["level"])
	}
	if entry["msg"] != "debug message" {
		t.Errorf("msg = %v", entry["msg"])
	}
}
