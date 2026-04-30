// Package web provides a Gin + go-lua web framework for Lumi.
// It manages a pool of Lua states and dispatches HTTP requests to Lua handlers.
package web

import (
	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/gin-gonic/gin"
)

// Config configures the web engine.
type Config struct {
	// PoolSize is the max number of Lua states. Default: 32.
	PoolSize int

	// InitFunc is called on each new Lua state to register modules.
	InitFunc func(L *lua.State)

	// Sandbox, if non-nil, enables sandboxed Lua states.
	Sandbox *lua.SandboxConfig

	// GinMode: "debug", "release", "test". Default: "release".
	GinMode string

	// OnError is called when a Lua handler returns an error.
	// If nil, a generic 500 JSON response is sent.
	OnError func(err error, c *gin.Context)
}

// Engine is the Lumi web engine combining Gin and Lua.
type Engine struct {
	Router *gin.Engine
	pool   *lua.StatePool
	config Config
}

// New creates a new Lumi web engine.
func New(cfg Config) *Engine {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 32
	}
	if cfg.GinMode == "" {
		cfg.GinMode = gin.ReleaseMode
	}

	gin.SetMode(cfg.GinMode)

	pool := lua.NewStatePool(lua.PoolConfig{
		MaxStates: cfg.PoolSize,
		InitFunc:  cfg.InitFunc,
		Sandbox:   cfg.Sandbox,
	})

	r := gin.New()
	r.Use(gin.Recovery())

	return &Engine{
		Router: r,
		pool:   pool,
		config: cfg,
	}
}

// Run starts the HTTP server on the given address.
func (e *Engine) Run(addr string) error {
	return e.Router.Run(addr)
}

// Close shuts down the engine and releases all Lua states.
func (e *Engine) Close() {
	e.pool.Close()
}

// Pool returns the underlying StatePool (for advanced use).
func (e *Engine) Pool() *lua.StatePool {
	return e.pool
}

// LuaHandler returns a gin.HandlerFunc that executes a Lua handler function
// stored in the registry at handlerRef.
func (e *Engine) LuaHandler(handlerRef int) gin.HandlerFunc {
	return func(c *gin.Context) {
		L := e.pool.Get()
		defer func() {
			L.DeleteUserValue("gin_ctx")
			L.SetTop(0)
			e.pool.Put(L)
		}()

		L.SetContext(c.Request.Context())
		L.SetUserValue("gin_ctx", c)

		// Push handler from registry, push context proxy, call
		pushContextProxy(L)

		if err := L.CallRef(handlerRef, 1, 0); err != nil {
			e.handleError(err, c)
		}
	}
}

// LuaHandlerFunc returns a gin.HandlerFunc that loads a Lua file and calls
// a named function from it. If funcName is empty, the file's return value
// is called directly.
func (e *Engine) LuaHandlerFunc(luaFile string, funcName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		L := e.pool.Get()
		defer func() {
			L.DeleteUserValue("gin_ctx")
			L.SetTop(0)
			e.pool.Put(L)
		}()

		L.SetContext(c.Request.Context())
		L.SetUserValue("gin_ctx", c)

		if err := L.DoFile(luaFile); err != nil {
			e.handleError(err, c)
			return
		}

		// Get the function from the returned module table
		if funcName != "" {
			L.GetField(-1, funcName)
			L.Remove(-2) // remove module table
		}

		// Push context proxy as argument
		pushContextProxy(L)

		if err := L.SafeCall(1, 0); err != nil {
			e.handleError(err, c)
		}
	}
}

func (e *Engine) handleError(err error, c *gin.Context) {
	if e.config.OnError != nil {
		e.config.OnError(err, c)
	} else {
		c.JSON(500, gin.H{"error": "internal server error"})
	}
}
