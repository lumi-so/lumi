# Lumi Web App Example — Todo API

A complete REST API built with Lumi, demonstrating:
- **pkg/web** — Gin integration with Lua handler dispatch
- **pkg/db** — SQLite database with parameterized queries
- **pkg/log** — Structured JSON logging
- **pkg/http** — HTTP client (available in Lua handlers)

## Quick Start

```bash
cd examples/web-app
go run main.go
```

The server starts on `http://localhost:8080` (override with `PORT` env var).

## API Endpoints

| Method | Path               | Description    |
|--------|--------------------|----------------|
| GET    | /api/v1/todos      | List all todos |
| POST   | /api/v1/todos      | Create a todo  |
| GET    | /api/v1/todos/:id  | Get a todo     |
| PUT    | /api/v1/todos/:id  | Update a todo  |
| DELETE | /api/v1/todos/:id  | Delete a todo  |

## Usage Examples

```bash
# Create a todo
curl -X POST http://localhost:8080/api/v1/todos \
  -H "Content-Type: application/json" \
  -d '{"title": "Learn Lumi"}'

# List todos
curl http://localhost:8080/api/v1/todos

# Get a single todo
curl http://localhost:8080/api/v1/todos/1

# Update a todo (mark completed)
curl -X PUT http://localhost:8080/api/v1/todos/1 \
  -H "Content-Type: application/json" \
  -d '{"completed": true}'

# Delete a todo
curl -X DELETE http://localhost:8080/api/v1/todos/1
```

## Architecture

```
main.go (Go)
├── Creates SQLite DB + todos table
├── Creates Lumi web.Engine with StatePool (8 states)
├── InitFunc registers modules: db, http, log
├── InitFunc loads: todo.lua → defines global handler functions
├── Registers Gin routes → luaHandler(engine, "func_name")
└── Starts HTTP server

app/controllers/todo.lua (Lua)
├── todo_list(c)   — db.query + c:json
├── todo_create(c) — c:get + db.exec + log.info
├── todo_get(c)    — c:param + db.get
├── todo_update(c) — c:get + db.exec
└── todo_delete(c) — c:param + db.exec

app/middlewares/logger.lua (Lua)
└── request_logger(c) — os.clock + c:next() + log.info
```

## Key Concepts Demonstrated

1. **StatePool** — Each HTTP request gets a Lua state from the pool; returned after use
2. **Context propagation** — `L.SetContext(c.Request.Context())` carries timeout/cancellation
3. **SafeCall** — Lua handler errors produce full tracebacks
4. **Parameterized SQL** — `db.exec("... WHERE id = ?", id)` prevents injection
5. **Structured logging** — `log.info("msg", {fields...})` outputs JSON
6. **Global Lua functions as handlers** — `InitFunc` loads `.lua` files that define global functions; Go routes dispatch to them by name
