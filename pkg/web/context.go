package web

import (
	"encoding/json"
	"io"
	"strings"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/gin-gonic/gin"
)

// pushContextProxy pushes a Lua table representing the request context.
// The table has methods: get, param, query, postForm, header, body,
// json, html, redirect, setHeader, setCookie, set, abort, next, status,
// method, path.
func pushContextProxy(L *lua.State) {
	L.NewTableFrom(map[string]any{
		"get":       lua.Function(lua.WrapSafe(ctxGet)),
		"param":     lua.Function(lua.WrapSafe(ctxParam)),
		"query":     lua.Function(lua.WrapSafe(ctxQuery)),
		"postForm":  lua.Function(lua.WrapSafe(ctxPostForm)),
		"header":    lua.Function(lua.WrapSafe(ctxHeader)),
		"body":      lua.Function(lua.WrapSafe(ctxBody)),
		"json":      lua.Function(lua.WrapSafe(ctxJSON)),
		"html":      lua.Function(lua.WrapSafe(ctxHTML)),
		"redirect":  lua.Function(lua.WrapSafe(ctxRedirect)),
		"setHeader": lua.Function(lua.WrapSafe(ctxSetHeader)),
		"setCookie": lua.Function(lua.WrapSafe(ctxSetCookie)),
		"set":       lua.Function(lua.WrapSafe(ctxSet)),
		"abort":     lua.Function(lua.WrapSafe(ctxAbort)),
		"next":      lua.Function(lua.WrapSafe(ctxNext)),
		"status":    lua.Function(lua.WrapSafe(ctxStatus)),
		"method":    lua.Function(lua.WrapSafe(ctxMethod)),
		"path":      lua.Function(lua.WrapSafe(ctxPath)),
		"string":    lua.Function(lua.WrapSafe(ctxString)),
	})
}

// getGinContext retrieves *gin.Context from the Lua state's UserValue.
func getGinContext(L *lua.State) *gin.Context {
	v := L.UserValue("gin_ctx")
	if v == nil {
		return nil
	}
	c, _ := v.(*gin.Context)
	return c
}

// --- Request data access ---

// c:get("user.info.age") — dot-path safe access into JSON body
func ctxGet(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}

	// Index 1 is self (the context table, because of `:` syntax)
	// Index 2 is the first explicit argument
	path := L.CheckString(2)

	body := getOrParseBody(c)
	if body == nil {
		L.PushNil()
		return 1
	}

	val := getNestedSafe(body, strings.Split(path, "."))
	L.PushAny(val)
	return 1
}

// c:param("id") — URL path parameter
func ctxParam(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}
	name := L.CheckString(2)
	val := c.Param(name)
	if val == "" {
		L.PushNil()
	} else {
		L.PushString(val)
	}
	return 1
}

// c:query("page") — query string parameter
func ctxQuery(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}
	name := L.CheckString(2)
	val, exists := c.GetQuery(name)
	if !exists {
		L.PushNil()
	} else {
		L.PushString(val)
	}
	return 1
}

// c:postForm("field") — form field
func ctxPostForm(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}
	name := L.CheckString(2)
	val, exists := c.GetPostForm(name)
	if !exists {
		L.PushNil()
	} else {
		L.PushString(val)
	}
	return 1
}

// c:header("Authorization") — request header
func ctxHeader(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}
	name := L.CheckString(2)
	val := c.GetHeader(name)
	if val == "" {
		L.PushNil()
	} else {
		L.PushString(val)
	}
	return 1
}

// c:body() — full parsed JSON body as table
func ctxBody(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}
	body := getOrParseBody(c)
	L.PushAny(body)
	return 1
}

// --- Response methods ---

// c:json(status, table) — JSON response
func ctxJSON(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	status := int(L.CheckInteger(2))
	data := L.ToAny(3)
	c.JSON(status, data)
	return 0
}

// c:string(status, text) — plain text response
func ctxString(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	status := int(L.CheckInteger(2))
	text := L.CheckString(3)
	c.String(status, "%s", text)
	return 0
}

// c:html(status, template, data) — HTML response
func ctxHTML(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	status := int(L.CheckInteger(2))
	tmpl := L.CheckString(3)
	data := L.ToAny(4)
	c.HTML(status, tmpl, data)
	return 0
}

// c:redirect(status, url)
func ctxRedirect(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	status := int(L.CheckInteger(2))
	url := L.CheckString(3)
	c.Redirect(status, url)
	return 0
}

// c:setHeader(key, value)
func ctxSetHeader(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	key := L.CheckString(2)
	value := L.CheckString(3)
	c.Header(key, value)
	return 0
}

// c:setCookie(name, value, maxAge, path, domain, secure, httpOnly)
func ctxSetCookie(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	name := L.CheckString(2)
	value := L.CheckString(3)
	maxAge := int(L.CheckInteger(4))
	// Optional params with defaults
	path := L.OptString(5, "/")
	domain := L.OptString(6, "")
	secure := false
	httpOnly := true

	if L.GetTop() >= 7 {
		secure = L.ToBoolean(7)
	}
	if L.GetTop() >= 8 {
		httpOnly = L.ToBoolean(8)
	}

	c.SetCookie(name, value, maxAge, path, domain, secure, httpOnly)
	return 0
}

// c:set(key, value) — set value in gin.Context (for passing to next middleware)
func ctxSet(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	key := L.CheckString(2)
	value := L.ToAny(3)
	c.Set(key, value)
	return 0
}

// c:abort() — abort the middleware chain
func ctxAbort(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	c.Abort()
	return 0
}

// c:next() — call next middleware
func ctxNext(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	c.Next()
	return 0
}

// c:status(code) — set response status without body
func ctxStatus(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		return 0
	}
	code := int(L.CheckInteger(2))
	c.Status(code)
	return 0
}

// c:method() — get HTTP method
func ctxMethod(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}
	L.PushString(c.Request.Method)
	return 1
}

// c:path() — get request path
func ctxPath(L *lua.State) int {
	c := getGinContext(L)
	if c == nil {
		L.PushNil()
		return 1
	}
	L.PushString(c.Request.URL.Path)
	return 1
}

// --- JSON body parsing (lazy, cached) ---

const bodyKey = "__lua_parsed_body"

func getOrParseBody(c *gin.Context) map[string]any {
	// Check cache
	if v, exists := c.Get(bodyKey); exists {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return nil
	}

	// Check content type
	ct := c.ContentType()
	if !strings.Contains(ct, "json") {
		c.Set(bodyKey, nil)
		return nil
	}

	// Parse body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) == 0 {
		c.Set(bodyKey, nil)
		return nil
	}

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		c.Set(bodyKey, nil)
		return nil
	}

	c.Set(bodyKey, m)
	return m
}

// getNestedSafe safely traverses a nested map by dot-separated keys.
// Returns nil if any intermediate key doesn't exist or isn't a map.
func getNestedSafe(m map[string]any, keys []string) any {
	if m == nil || len(keys) == 0 {
		return nil
	}

	var current any = m
	for _, key := range keys {
		switch v := current.(type) {
		case map[string]any:
			current = v[key]
		default:
			return nil
		}
	}
	return current
}
