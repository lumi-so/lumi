package web

import (
	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/gin-gonic/gin"
)

// RegisterRouter registers the "router" module in the Lua state.
// This allows Lua code to define routes:
//
//	local router = require("router")
//	local api = router.group("/api/v1")
//	api:get("/users/:id", handler)
//	api:post("/users", handler)
func RegisterRouter(L *lua.State, engine *Engine) {
	lua.RegisterModule(L, "router", map[string]lua.Function{
		"group": lua.WrapSafe(func(L *lua.State) int {
			prefix := L.CheckString(1)
			pushRouterGroup(L, engine, engine.Router.Group(prefix))
			return 1
		}),
		"get":    lua.WrapSafe(makeRouteRegistrar(engine, "GET")),
		"post":   lua.WrapSafe(makeRouteRegistrar(engine, "POST")),
		"put":    lua.WrapSafe(makeRouteRegistrar(engine, "PUT")),
		"delete": lua.WrapSafe(makeRouteRegistrar(engine, "DELETE")),
		"patch":  lua.WrapSafe(makeRouteRegistrar(engine, "PATCH")),
	})
}

func pushRouterGroup(L *lua.State, engine *Engine, group *gin.RouterGroup) {
	// Store the group as userdata
	L.PushUserdata(group)

	// Create metatable with route methods
	if L.NewMetatable("lumi.router_group") {
		// __index table
		methods := map[string]lua.Function{
			"get":    lua.WrapSafe(groupRoute(engine, "GET")),
			"post":   lua.WrapSafe(groupRoute(engine, "POST")),
			"put":    lua.WrapSafe(groupRoute(engine, "PUT")),
			"delete": lua.WrapSafe(groupRoute(engine, "DELETE")),
			"patch":  lua.WrapSafe(groupRoute(engine, "PATCH")),
			"use":    lua.WrapSafe(groupUse(engine)),
			"group":  lua.WrapSafe(groupGroup(engine)),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func makeRouteRegistrar(engine *Engine, method string) lua.Function {
	return func(L *lua.State) int {
		path := L.CheckString(1)
		// Handler function is at stack index 2 — copy to top and ref
		L.PushValue(2)
		ref := L.Ref(lua.RegistryIndex)

		handler := engine.LuaHandler(ref)
		addRoute(engine.Router, method, path, handler)
		return 0
	}
}

func groupRoute(engine *Engine, method string) lua.Function {
	return func(L *lua.State) int {
		// self (group userdata) is at index 1
		group := L.UserdataValue(1).(*gin.RouterGroup)
		path := L.CheckString(2)

		// Handler function at index 3
		L.PushValue(3)
		ref := L.Ref(lua.RegistryIndex)

		handler := engine.LuaHandler(ref)
		addGroupRoute(group, method, path, handler)
		return 0
	}
}

func groupUse(engine *Engine) lua.Function {
	return func(L *lua.State) int {
		group := L.UserdataValue(1).(*gin.RouterGroup)

		// Middleware function at index 2
		L.PushValue(2)
		ref := L.Ref(lua.RegistryIndex)

		group.Use(engine.LuaHandler(ref))
		return 0
	}
}

func groupGroup(engine *Engine) lua.Function {
	return func(L *lua.State) int {
		group := L.UserdataValue(1).(*gin.RouterGroup)
		prefix := L.CheckString(2)
		pushRouterGroup(L, engine, group.Group(prefix))
		return 1
	}
}

// addRoute adds a route to the gin engine by HTTP method.
func addRoute(r *gin.Engine, method, path string, handler gin.HandlerFunc) {
	switch method {
	case "GET":
		r.GET(path, handler)
	case "POST":
		r.POST(path, handler)
	case "PUT":
		r.PUT(path, handler)
	case "DELETE":
		r.DELETE(path, handler)
	case "PATCH":
		r.PATCH(path, handler)
	}
}

// addGroupRoute adds a route to a gin router group by HTTP method.
func addGroupRoute(g *gin.RouterGroup, method, path string, handler gin.HandlerFunc) {
	switch method {
	case "GET":
		g.GET(path, handler)
	case "POST":
		g.POST(path, handler)
	case "PUT":
		g.PUT(path, handler)
	case "DELETE":
		g.DELETE(path, handler)
	case "PATCH":
		g.PATCH(path, handler)
	}
}
