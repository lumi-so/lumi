// Package main demonstrates a complete Lumi web application.
// It implements a Todo REST API using pkg/web, pkg/db, pkg/log, and pkg/http.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gin-gonic/gin"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/lumi-so/lumi/pkg/db"
	luahttp "github.com/lumi-so/lumi/pkg/http"
	lualog "github.com/lumi-so/lumi/pkg/log"
	"github.com/lumi-so/lumi/pkg/web"
)

func main() {
	// 1. Setup SQLite database
	database, err := sql.Open("sqlite3", "./todos.db")
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	// Create table if not exists
	_, err = database.Exec(`
		CREATE TABLE IF NOT EXISTS todos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			completed INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// 2. Create Lumi web engine
	engine := web.New(web.Config{
		PoolSize: 8,
		GinMode:  "debug",
		InitFunc: func(L *lua.State) {
			// Register all Lua modules
			db.RegisterDB(L, db.DBConfig{DB: database})
			luahttp.Register(L, luahttp.Config{})
			lualog.Register(L, lualog.Config{
				Output: os.Stdout,
				Fields: map[string]any{"service": "todo-app"},
			})

			// Load controllers — defines global handler functions
			if err := L.DoFile("app/controllers/todo.lua"); err != nil {
				log.Printf("[WARN] Failed to load todo.lua: %v", err)
			}
			if err := L.DoFile("app/middlewares/logger.lua"); err != nil {
				log.Printf("[WARN] Failed to load logger.lua: %v", err)
			}
		},
		OnError: func(err error, c *gin.Context) {
			log.Printf("[ERROR] Lua: %v", err)
			c.JSON(500, gin.H{"code": -1, "msg": "internal server error"})
		},
	})
	defer engine.Close()

	// 3. Register routes
	api := engine.Router.Group("/api/v1")
	{
		api.GET("/todos", luaHandler(engine, "todo_list"))
		api.POST("/todos", luaHandler(engine, "todo_create"))
		api.GET("/todos/:id", luaHandler(engine, "todo_get"))
		api.PUT("/todos/:id", luaHandler(engine, "todo_update"))
		api.DELETE("/todos/:id", luaHandler(engine, "todo_delete"))
	}

	// 4. Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("🚀 Todo API running on http://localhost:%s\n", port)
	fmt.Println("  GET    /api/v1/todos")
	fmt.Println("  POST   /api/v1/todos")
	fmt.Println("  GET    /api/v1/todos/:id")
	fmt.Println("  PUT    /api/v1/todos/:id")
	fmt.Println("  DELETE /api/v1/todos/:id")
	if err := engine.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

// luaHandler creates a gin.HandlerFunc that calls a global Lua function by name.
// The function receives a context proxy table as its argument.
func luaHandler(e *web.Engine, funcName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		L := e.Pool().Get()
		defer func() {
			L.DeleteUserValue("gin_ctx")
			L.SetTop(0)
			e.Pool().Put(L)
		}()

		// Propagate request context (timeout/cancellation)
		L.SetContext(c.Request.Context())
		// Inject gin.Context for the web.PushContextProxy binding
		L.SetUserValue("gin_ctx", c)

		// Push the global Lua function
		if typ := L.GetGlobal(funcName); typ != lua.TypeFunction {
			log.Printf("[ERROR] Lua global %q is not a function (type=%v)", funcName, typ)
			c.JSON(500, gin.H{"code": -1, "msg": "handler not found"})
			return
		}

		// Push context proxy as argument
		web.PushContextProxy(L)

		// Call handler(ctx)
		if err := L.SafeCall(1, 0); err != nil {
			log.Printf("[ERROR] %s: %v", funcName, err)
			c.JSON(500, gin.H{"code": -1, "msg": "internal server error"})
		}
	}
}
