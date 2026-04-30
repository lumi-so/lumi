package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

func setupHTTPTest(t *testing.T) (*lua.State, *httptest.Server) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		resp := map[string]any{
			"method":  r.Method,
			"body":    string(body),
			"headers": r.Header.Get("X-Custom"),
		}
		w.Header().Set("X-Response", "test")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`[{"id":1,"name":"Alice"}]`))
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("done"))
	})

	server := httptest.NewServer(mux)

	L := lua.NewState()
	Register(L, Config{})

	return L, server
}

func TestHTTPGet(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := fmt.Sprintf(`
		local http = require("http")
		local resp, err = http.get("%s/users")
		assert(err == nil, "err should be nil: " .. tostring(err))
		assert(resp.status == 200, "status should be 200")
		assert(resp.body ~= "", "body should not be empty")
	`, server.URL)

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPPost(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := fmt.Sprintf(`
		local http = require("http")
		local resp = http.post("%s/echo", {
			headers = {["X-Custom"] = "hello"},
			body = "test body",
		})
		assert(resp.status == 200, "status should be 200, got " .. tostring(resp.status))
		assert(resp.headers["x-response"] == "test", "missing x-response header")
	`, server.URL)

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPPut(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := fmt.Sprintf(`
		local http = require("http")
		local resp = http.put("%s/echo", {
			body = "put data",
		})
		assert(resp.status == 200)
	`, server.URL)

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPDelete(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := fmt.Sprintf(`
		local http = require("http")
		local resp = http.delete("%s/echo")
		assert(resp.status == 200)
	`, server.URL)

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPRequest(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := fmt.Sprintf(`
		local http = require("http")
		local resp = http.request({
			method = "PUT",
			url = "%s/echo",
			body = "put data",
		})
		assert(resp.status == 200)
	`, server.URL)

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPRequestMissingURL(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := `
		local http = require("http")
		local resp, err = http.request({method = "GET"})
		assert(resp == nil, "resp should be nil")
		assert(err ~= nil, "err should not be nil")
	`

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTimeout(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := fmt.Sprintf(`
		local http = require("http")
		local resp, err = http.request({
			url = "%s/slow",
			timeout = 0.1,
		})
		assert(resp == nil, "resp should be nil on timeout")
		assert(err ~= nil, "err should not be nil on timeout")
	`, server.URL)

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPAsyncGet(t *testing.T) {
	L, server := setupHTTPTest(t)
	defer server.Close()
	defer L.Close()

	code := fmt.Sprintf(`
		local http = require("http")
		local f = http.async_get("%s/users")
		assert(f ~= nil, "future should not be nil")
		local resp = f:await()
		assert(resp ~= nil, "resp should not be nil")
		assert(resp.status == 200, "status should be 200")
	`, server.URL)

	if err := L.DoString(code); err != nil {
		t.Fatal(err)
	}
}
