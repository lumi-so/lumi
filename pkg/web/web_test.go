package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/gin-gonic/gin"
)

func newTestEngine() *Engine {
	return New(Config{
		PoolSize: 4,
		GinMode:  "test",
	})
}

func TestNew(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	if e.Router == nil {
		t.Fatal("Router is nil")
	}
	if e.Pool() == nil {
		t.Fatal("Pool is nil")
	}
}

func TestLuaHandler_JSON(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	// Create a Lua handler that returns JSON
	L := e.pool.Get()
	err := L.DoString(`
		function handle_hello(c)
			c:json(200, {message = "hello world"})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_hello")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/hello", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/hello", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "hello world") {
		t.Fatalf("body = %q, want contains 'hello world'", w.Body.String())
	}
}

func TestLuaHandler_Param(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_user(c)
			local id = c:param("id")
			c:json(200, {user_id = id})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_user")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/users/:id", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/42", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "42") {
		t.Fatalf("body = %q, want contains '42'", w.Body.String())
	}
}

func TestLuaHandler_Query(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_search(c)
			local q = c:query("q")
			local page = c:query("page")
			c:json(200, {query = q, page = page})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_search")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/search", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/search?q=hello&page=2", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "hello") {
		t.Fatalf("body = %q, want contains 'hello'", body)
	}
	if !strings.Contains(body, "2") {
		t.Fatalf("body = %q, want contains '2'", body)
	}
}

func TestLuaHandler_JSONBody(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_create(c)
			local name = c:get("name")
			local age = c:get("info.age")
			c:json(201, {name = name, age = age})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_create")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.POST("/create", e.LuaHandler(ref))

	jsonBody := `{"name":"alice","info":{"age":30}}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/create", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	e.Router.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "alice") {
		t.Fatalf("body = %q, want contains 'alice'", body)
	}
	if !strings.Contains(body, "30") {
		t.Fatalf("body = %q, want contains '30'", body)
	}
}

func TestLuaHandler_Method(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_info(c)
			c:json(200, {method = c:method(), path = c:path()})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_info")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/info", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/info", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "GET") {
		t.Fatalf("body = %q, want contains 'GET'", body)
	}
	if !strings.Contains(body, "/info") {
		t.Fatalf("body = %q, want contains '/info'", body)
	}
}

func TestLuaHandler_Status(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_nocontent(c)
			c:status(204)
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_nocontent")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.DELETE("/item", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/item", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestLuaHandler_String(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_text(c)
			c:string(200, "plain text response")
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_text")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/text", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/text", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plain text response") {
		t.Fatalf("body = %q, want contains 'plain text response'", w.Body.String())
	}
}

func TestLuaHandler_Header(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_auth(c)
			local auth = c:header("Authorization")
			c:json(200, {auth = auth})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_auth")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/auth", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth", nil)
	req.Header.Set("Authorization", "Bearer token123")
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Bearer token123") {
		t.Fatalf("body = %q, want contains 'Bearer token123'", w.Body.String())
	}
}

func TestLuaHandler_SetHeader(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_custom_header(c)
			c:setHeader("X-Custom", "my-value")
			c:json(200, {ok = true})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_custom_header")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/custom", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/custom", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Custom") != "my-value" {
		t.Fatalf("X-Custom = %q, want 'my-value'", w.Header().Get("X-Custom"))
	}
}

func TestLuaMiddleware(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	// Use LuaMiddleware to inject state
	e.Router.Use(LuaMiddleware(e.pool))

	e.Router.GET("/mw-test", func(c *gin.Context) {
		L := GetState(c)
		if L == nil {
			t.Fatal("GetState returned nil")
			return
		}
		c.JSON(http.StatusOK, gin.H{"has_state": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mw-test", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "true") {
		t.Fatalf("body = %q, want contains 'true'", w.Body.String())
	}
}

func TestRouterGroup(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_api(c)
			c:json(200, {api = true})
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_api")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	// Register route on a group
	api := e.Router.Group("/api/v1")
	api.GET("/test", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "true") {
		t.Fatalf("body = %q, want contains 'true'", w.Body.String())
	}
}

func TestLuaHandler_ErrorRecovery(t *testing.T) {
	e := newTestEngine()
	defer e.Close()

	L := e.pool.Get()
	err := L.DoString(`
		function handle_error(c)
			error("something went wrong")
		end
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	L.GetGlobal("handle_error")
	ref := L.Ref(lua.RegistryIndex)
	e.pool.Put(L)

	e.Router.GET("/error", e.LuaHandler(ref))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/error", nil)
	e.Router.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "internal server error") {
		t.Fatalf("body = %q, want contains 'internal server error'", w.Body.String())
	}
}
