package http

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// Config configures the HTTP client binding.
type Config struct {
	// DefaultTimeout for requests (default: 30s)
	DefaultTimeout time.Duration

	// MaxResponseBody limits response body size (default: 10MB)
	MaxResponseBody int64

	// Client is the http.Client to use. If nil, uses default with timeout.
	Client *http.Client
}

// Register registers the "http" module in the Lua state.
func Register(L *lua.State, cfg Config) {
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.MaxResponseBody == 0 {
		cfg.MaxResponseBody = 10 * 1024 * 1024
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: cfg.DefaultTimeout}
	}

	lua.RegisterModule(L, "http", map[string]lua.Function{
		"get":           lua.WrapSafe(httpGet(cfg)),
		"post":          lua.WrapSafe(httpPost(cfg)),
		"put":           lua.WrapSafe(httpPut(cfg)),
		"delete":        lua.WrapSafe(httpDelete(cfg)),
		"request":       lua.WrapSafe(httpRequest(cfg)),
		"async_get":     lua.WrapSafe(httpAsyncGet(cfg)),
		"async_post":    lua.WrapSafe(httpAsyncPost(cfg)),
		"async_request": lua.WrapSafe(httpAsyncRequest(cfg)),
	})
}

// http.get(url) → {status, body, headers} or nil, err
func httpGet(cfg Config) lua.Function {
	return func(L *lua.State) int {
		url := L.CheckString(1)
		return doRequest(L, cfg, "GET", url, nil)
	}
}

// http.post(url, opts) → {status, body, headers} or nil, err
func httpPost(cfg Config) lua.Function {
	return func(L *lua.State) int {
		url := L.CheckString(1)
		opts := getRequestOpts(L, 2)
		return doRequest(L, cfg, "POST", url, opts)
	}
}

// http.put(url, opts) → {status, body, headers} or nil, err
func httpPut(cfg Config) lua.Function {
	return func(L *lua.State) int {
		url := L.CheckString(1)
		opts := getRequestOpts(L, 2)
		return doRequest(L, cfg, "PUT", url, opts)
	}
}

// http.delete(url, opts) → {status, body, headers} or nil, err
func httpDelete(cfg Config) lua.Function {
	return func(L *lua.State) int {
		url := L.CheckString(1)
		opts := getRequestOpts(L, 2)
		return doRequest(L, cfg, "DELETE", url, opts)
	}
}

// http.request(opts) → {status, body, headers} or nil, err
// opts = {method, url, headers, body, timeout}
func httpRequest(cfg Config) lua.Function {
	return func(L *lua.State) int {
		opts := getFullRequestOpts(L, 1)
		if opts == nil || opts.URL == "" {
			L.PushNil()
			L.PushString("http.request: url is required")
			return 2
		}
		return doRequest(L, cfg, opts.Method, opts.URL, opts)
	}
}

type requestOpts struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
	Timeout float64 // seconds
}

func getRequestOpts(L *lua.State, idx int) *requestOpts {
	if L.GetTop() < idx || L.IsNil(idx) {
		return nil
	}
	opts := &requestOpts{}
	if L.IsTable(idx) {
		// Read headers
		L.GetField(idx, "headers")
		if L.IsTable(-1) {
			opts.Headers = make(map[string]string)
			L.ForEach(-1, func(inner *lua.State) bool {
				k, _ := inner.ToString(-2)
				v, _ := inner.ToString(-1)
				opts.Headers[k] = v
				return true
			})
		}
		L.Pop(1)

		// Read body
		L.GetField(idx, "body")
		if s, ok := L.ToString(-1); ok {
			opts.Body = s
		}
		L.Pop(1)

		// Read timeout
		L.GetField(idx, "timeout")
		if n, ok := L.ToNumber(-1); ok && n > 0 {
			opts.Timeout = n
		}
		L.Pop(1)
	}
	return opts
}

func getFullRequestOpts(L *lua.State, idx int) *requestOpts {
	opts := getRequestOpts(L, idx)
	if opts == nil {
		opts = &requestOpts{}
	}
	if L.IsTable(idx) {
		L.GetField(idx, "method")
		if s, ok := L.ToString(-1); ok {
			opts.Method = strings.ToUpper(s)
		}
		L.Pop(1)

		L.GetField(idx, "url")
		if s, ok := L.ToString(-1); ok {
			opts.URL = s
		}
		L.Pop(1)
	}
	if opts.Method == "" {
		opts.Method = "GET"
	}
	return opts
}

func doRequest(L *lua.State, cfg Config, method, url string, opts *requestOpts) int {
	var bodyReader io.Reader
	if opts != nil && opts.Body != "" {
		bodyReader = strings.NewReader(opts.Body)
	}

	ctx := L.Context()

	// Apply timeout
	if opts != nil && opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout*float64(time.Second)))
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	// Set headers
	if opts != nil && opts.Headers != nil {
		for k, v := range opts.Headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := cfg.Client.Do(req)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	defer resp.Body.Close()

	// Read body (limited)
	body, err := io.ReadAll(io.LimitReader(resp.Body, cfg.MaxResponseBody))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	// Build response table
	respHeaders := make(map[string]any)
	for k := range resp.Header {
		respHeaders[strings.ToLower(k)] = resp.Header.Get(k)
	}

	L.PushAny(map[string]any{
		"status":  resp.StatusCode,
		"body":    string(body),
		"headers": respHeaders,
	})
	return 1
}

// http.async_get(url) → Future
func httpAsyncGet(cfg Config) lua.Function {
	return func(L *lua.State) int {
		url := L.CheckString(1)
		return doAsyncRequest(L, cfg, "GET", url, nil)
	}
}

// http.async_post(url, opts) → Future
func httpAsyncPost(cfg Config) lua.Function {
	return func(L *lua.State) int {
		url := L.CheckString(1)
		opts := getRequestOpts(L, 2)
		return doAsyncRequest(L, cfg, "POST", url, opts)
	}
}

// http.async_request(opts) → Future
func httpAsyncRequest(cfg Config) lua.Function {
	return func(L *lua.State) int {
		opts := getFullRequestOpts(L, 1)
		if opts == nil || opts.URL == "" {
			L.PushNil()
			L.PushString("http.async_request: url is required")
			return 2
		}
		return doAsyncRequest(L, cfg, opts.Method, opts.URL, opts)
	}
}

func doAsyncRequest(L *lua.State, cfg Config, method, url string, opts *requestOpts) int {
	ctx := L.Context()

	future := lua.NewFuture()

	go func() {
		var bodyReader io.Reader
		if opts != nil && opts.Body != "" {
			bodyReader = strings.NewReader(opts.Body)
		}

		reqCtx := ctx
		if opts != nil && opts.Timeout > 0 {
			var cancel context.CancelFunc
			reqCtx, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout*float64(time.Second)))
			defer cancel()
		}

		req, err := http.NewRequestWithContext(reqCtx, method, url, bodyReader)
		if err != nil {
			future.Reject(err)
			return
		}

		if opts != nil && opts.Headers != nil {
			for k, v := range opts.Headers {
				req.Header.Set(k, v)
			}
		}

		resp, err := cfg.Client.Do(req)
		if err != nil {
			future.Reject(err)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, cfg.MaxResponseBody))
		if err != nil {
			future.Reject(err)
			return
		}

		respHeaders := make(map[string]string)
		for k := range resp.Header {
			respHeaders[strings.ToLower(k)] = resp.Header.Get(k)
		}

		future.Resolve(map[string]any{
			"status":  resp.StatusCode,
			"body":    string(body),
			"headers": respHeaders,
		})
	}()

	L.PushUserdata(future)
	// Set metatable with :await() method
	if L.NewMetatable("lumi.future") {
		L.NewTable()
		L.PushFunction(lua.WrapSafe(futureAwait))
		L.SetField(-2, "await")
		L.PushFunction(lua.WrapSafe(futureCancel))
		L.SetField(-2, "cancel")
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
	return 1
}

func futureAwait(L *lua.State) int {
	f := L.UserdataValue(1).(*lua.Future)

	// Block until resolved (respects context cancellation)
	select {
	case <-f.Wait():
		val, err := f.Result()
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}
		L.PushAny(val)
		return 1
	case <-L.Context().Done():
		L.PushNil()
		L.PushString("request cancelled")
		return 2
	}
}

func futureCancel(L *lua.State) int {
	f := L.UserdataValue(1).(*lua.Future)
	f.Cancel()
	return 0
}
