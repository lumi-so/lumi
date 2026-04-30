-- Todo CRUD Controller
-- Each function receives a context proxy (c) as its argument.

local db = require("db")
local log = require("log")

-- GET /api/v1/todos — List all todos
function todo_list(c)
    local todos, err = db.query("SELECT * FROM todos ORDER BY created_at DESC")
    if err then
        log.error("failed to list todos", {err = err})
        c:json(500, {code = -1, msg = "database error"})
        return
    end

    c:json(200, {code = 0, data = todos or {}})
end

-- POST /api/v1/todos — Create a new todo
function todo_create(c)
    local title = c:get("title")
    if not title or #title == 0 then
        c:json(400, {code = 1, msg = "title is required"})
        return
    end

    local result, err = db.exec(
        "INSERT INTO todos (title) VALUES (?)", title
    )
    if err then
        log.error("failed to create todo", {err = err, title = title})
        c:json(500, {code = -1, msg = "database error"})
        return
    end

    log.info("todo created", {id = result.last_id, title = title})
    c:json(201, {code = 0, msg = "created", data = {id = result.last_id}})
end

-- GET /api/v1/todos/:id — Get a single todo
function todo_get(c)
    local id = c:param("id")
    if not id then
        c:json(400, {code = 1, msg = "id is required"})
        return
    end

    local todo, err = db.get("SELECT * FROM todos WHERE id = ?", id)
    if err then
        log.error("failed to get todo", {err = err, id = id})
        c:json(500, {code = -1, msg = "database error"})
        return
    end

    if not todo then
        c:json(404, {code = 1, msg = "todo not found"})
        return
    end

    c:json(200, {code = 0, data = todo})
end

-- PUT /api/v1/todos/:id — Update a todo
function todo_update(c)
    local id = c:param("id")
    if not id then
        c:json(400, {code = 1, msg = "id is required"})
        return
    end

    local title = c:get("title")
    local completed = c:get("completed")

    if title then
        local _, err = db.exec("UPDATE todos SET title = ? WHERE id = ?", title, id)
        if err then
            log.error("failed to update todo title", {err = err, id = id})
            c:json(500, {code = -1, msg = "database error"})
            return
        end
    end

    if completed ~= nil then
        local val = completed and 1 or 0
        local _, err = db.exec("UPDATE todos SET completed = ? WHERE id = ?", val, id)
        if err then
            log.error("failed to update todo completed", {err = err, id = id})
            c:json(500, {code = -1, msg = "database error"})
            return
        end
    end

    log.info("todo updated", {id = id})
    c:json(200, {code = 0, msg = "updated"})
end

-- DELETE /api/v1/todos/:id — Delete a todo
function todo_delete(c)
    local id = c:param("id")
    if not id then
        c:json(400, {code = 1, msg = "id is required"})
        return
    end

    local result, err = db.exec("DELETE FROM todos WHERE id = ?", id)
    if err then
        log.error("failed to delete todo", {err = err, id = id})
        c:json(500, {code = -1, msg = "database error"})
        return
    end

    if result.affected == 0 then
        c:json(404, {code = 1, msg = "todo not found"})
        return
    end

    log.info("todo deleted", {id = id})
    c:json(200, {code = 0, msg = "deleted"})
end
