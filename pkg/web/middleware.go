package web

import (
	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/gin-gonic/gin"
)

// LuaMiddleware creates a Gin middleware that acquires a Lua state from the pool,
// injects the request context, and releases it after the handler completes.
func LuaMiddleware(pool *lua.StatePool) gin.HandlerFunc {
	return func(c *gin.Context) {
		L := pool.Get()
		defer func() {
			L.DeleteUserValue("gin_ctx")
			L.SetTop(0)
			pool.Put(L)
		}()

		// Propagate request context (timeout/cancellation)
		L.SetContext(c.Request.Context())

		// Inject gin.Context for binding layer
		L.SetUserValue("gin_ctx", c)

		// Store L in gin.Context for downstream handlers
		c.Set("lua_state", L)

		c.Next()
	}
}

// GetState retrieves the Lua state from gin.Context.
// Returns nil if LuaMiddleware was not used.
func GetState(c *gin.Context) *lua.State {
	if v, ok := c.Get("lua_state"); ok {
		if L, ok := v.(*lua.State); ok {
			return L
		}
	}
	return nil
}
