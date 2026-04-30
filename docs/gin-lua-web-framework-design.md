# Lumi Web Framework 设计方案（Gin + go-lua）

> **技术栈**：本文档面向 **lumi** 项目 `pkg/web/` 模块，基于 **Gin + go-lua v0.9.9** 构建。所有代码示例使用 go-lua 已验证的安全 API（`StatePool`、`SafeCall`、`WrapSafe`、`PushResource` 等），而非底层原始操作。

---

## 一、方案概述

### 1.1 设计目标

**「Go 负责底层支撑、Lua 负责业务快速迭代」**——兼顾性能、安全与开发效率。

| 痛点 | 解决方式 |
|------|----------|
| Lua VM 与 HTTP 请求生命周期错位 | `StatePool` 池化 + 请求级隔离 + 自动归还 |
| 请求超时/取消无法传播到 Lua | `L.SetContext(ctx)` 绑定 `context.Context`，自动中断 |
| Go 绑定 panic 导致进程崩溃 | `WrapSafe` 包装所有 Go 函数，panic → Lua error |
| Handler 错误缺少调用栈 | `SafeCall` 自动附加 `debug.traceback` |
| DB/文件等资源泄漏 | `PushResource` / `PushCloseableResource` + GC 自动清理 |
| JSON 嵌套取值繁琐、易 NPE | 惰性解析 + 路径安全取值 `c:get("user.info.age")` |
| 热更新与生产一致性 | `PrepareReload` 两阶段提交，兼容性检查后再 `Commit` |
| 部署依赖外部文件 | `SetFileSystem(embed.FS)` 单二进制部署 |

### 1.2 核心优势

- **安全**：`StatePool` 线程安全池化；`WrapSafe` 防 panic 扩散；`SandboxConfig` 限制内存/CPU/IO；请求级 `Context` 隔离。
- **性能**：VM 池化复用零分配；JSON 每请求最多解析一次；`PushAny` 自动类型桥接减少手动栈操作。
- **开发效率**：Lua 业务层语法简洁；`c:json()`/`c:param()` 对齐 Web 习惯；热更新支持快速迭代。
- **可观测**：`RefTracker` 开发期泄漏检测；`luadebug` 性能分析；`luatest` 单元测试。
- **部署**：`embed.FS` + `SetFileSystem` 实现单二进制分发，零外部文件依赖。

### 1.3 go-lua 关键能力清单

| 能力 | API | 用途 |
|------|-----|------|
| 池化 | `NewStatePool` / `pool.Get()` / `pool.Put()` | VM 生命周期管理 |
| 上下文 | `SetContext(ctx)` / `Context()` | 请求超时/取消传播 |
| 用户值 | `SetUserValue` / `UserValue` / `DeleteUserValue` | 请求级数据注入 |
| 安全调用 | `SafeCall(nArgs, nResults)` | PCall + 自动 traceback |
| 安全包装 | `WrapSafe(fn)` | Go panic → Lua error |
| 资源管理 | `PushResource` / `PushCloseableResource` | 自动 GC 清理 |
| 类型桥接 | `PushAny` / `ToAny` / `ToStruct` / `PushGoFunc` | Go↔Lua 自动转换 |
| 热更新 | `PrepareReload` / `ReloadModule` | 两阶段热更新 |
| 异步 | `NewFuture` / `NewScheduler` / `Spawn` | 并发 IO |
| 沙箱 | `NewSandboxState(SandboxConfig{...})` | 内存/CPU/IO 限制 |
| 虚拟 FS | `SetFileSystem(fs.FS)` | embed 支持 |
| 泄漏检测 | `NewRefTracker` / `Leaks()` | 开发期诊断 |

### 1.4 适用场景

- 中小规模 API、运营活动规则、可频繁调整的配置型逻辑。
- 后台管理类接口、需要可控热更新的业务（配合 `PrepareReload` 审计）。
- **不适用**：极高 QPS 核心链路（应下沉 Go）；强多租户隔离且未做进程/配额隔离的裸脚本执行。

---

## 二、架构设计

### 2.1 分层架构

```
┌─────────────────────────────────────────────────────┐
│  Lua 业务层    routes.lua / controllers/*.lua        │
├─────────────────────────────────────────────────────┤
│  绑定适配层    WrapSafe 注册 · PushAny 类型桥接      │
│               PushResource 资源管理 · 惰性 JSON 代理  │
├─────────────────────────────────────────────────────┤
│  生命周期层    StatePool 池化 · SetContext 超时传播   │
│               SafeCall handler 调用 · 请求级清理     │
├─────────────────────────────────────────────────────┤
│  Gin 引擎层    gin.Engine · RouterGroup · 中间件链   │
├─────────────────────────────────────────────────────┤
│  Go 基础层     DB · Redis · 日志 · JWT · HTTP Client │
└─────────────────────────────────────────────────────┘
```

### 2.2 核心设计原则

1. **职责分离**：Go 管 IO、连接池、密钥、超时；Lua 管业务流程编排。
2. **安全优先**：所有 Go 绑定经 `WrapSafe` 包装；所有资源经 `PushResource` 托管；VM 不跨 goroutine 共享。
3. **惰性加载**：未访问的 JSON 路径不触发额外工作；访问时从已缓存结构取值。
4. **API 收敛**：Lua 只见白名单 Go 函数，不暴露裸 `*gin.Context` 全能力。
5. **池化复用**：`StatePool` 管理 VM 生命周期，避免每请求 `NewState` 开销。

### 2.3 请求生命周期（完整流程）

```
HTTP Request
    │
    ▼
┌─ Gin 中间件链 ─────────────────────────────────────┐
│  Recovery → Logger → Auth(Go) → LuaVM 中间件       │
│                                      │              │
│                          pool.Get() → L             │
│                          L.SetContext(c.Request.     │
│                              Context())             │
│                          L.SetUserValue("gin_ctx",c)│
│                                      │              │
│                              ┌───────▼────────┐     │
│                              │  加载 handler   │     │
│                              │  L.SafeCall()   │     │
│                              └───────┬────────┘     │
│                                      │              │
│                          L.DeleteUserValue("gin_ctx")│
│                          L.SetTop(0)                │
│                          pool.Put(L)                │
└─────────────────────────────────────────────────────┘
    │
    ▼
HTTP Response
```

---

## 三、核心模块设计

### 3.1 State 池化（StatePool）

使用 go-lua 内置的 `StatePool`，替代手动 `sync.Pool`：

```go
import "github.com/anthropic/go-lua/lua"

pool := lua.NewStatePool(lua.PoolConfig{
    MaxStates: 32,
    InitFunc: func(L *lua.State) {
        // 注册进程级不变的绑定（无请求状态）
        binding.RegisterModules(L)
    },
    Sandbox: &lua.SandboxConfig{
        MemoryLimit: 10 * 1024 * 1024, // 10MB per state
        CPULimit:    1_000_000,
        AllowIO:     false,
    },
})
defer pool.Close()
```

**关键优势**（相比手动 `sync.Pool`）：

| 特性 | `sync.Pool` | `StatePool` |
|------|-------------|-------------|
| 线程安全 | 需自行保证 | 内置 |
| 最大数量限制 | 无（GC 回收） | `MaxStates` 上限 |
| 沙箱集成 | 手动配置 | `PoolConfig.Sandbox` |
| 初始化函数 | `New` 字段 | `InitFunc`（类型安全） |
| 关闭清理 | 无保证 | `pool.Close()` 关闭所有 |

### 3.2 请求中间件（Context 注入 + 超时传播）

```go
func LuaMiddleware(pool *lua.StatePool) gin.HandlerFunc {
    return func(c *gin.Context) {
        L := pool.Get()
        defer func() {
            L.DeleteUserValue("gin_ctx")
            L.SetTop(0)
            pool.Put(L)
        }()

        // 🔑 绑定请求 Context → Lua 自动感知超时/取消
        L.SetContext(c.Request.Context())

        // 注入 gin.Context 供绑定层使用
        L.SetUserValue("gin_ctx", c)

        c.Set("lua_state", L)
        c.Next()
    }
}
```

**`SetContext` 的作用**：当客户端断开或请求超时时，`context.Context` 被取消，go-lua VM 会自动中断当前执行。无需 Lua 侧显式检查——这是与 Go 生态的原生集成。

### 3.3 Handler 调用（SafeCall + traceback）

使用 `SafeCall` 替代原始 `pcall`，自动附加 `debug.traceback`：

```go
func callLuaHandler(L *lua.State, handlerName string) error {
    // 加载 handler 函数到栈顶
    L.Global(handlerName)

    // 推入参数（ctx proxy）
    pushContextProxy(L)

    // SafeCall = PCall + 自动 debug.traceback
    if err := L.SafeCall(1, 0); err != nil {
        // err 已包含完整 Lua 调用栈
        return fmt.Errorf("handler %s failed: %w", handlerName, err)
    }
    return nil
}
```

**对比**：

```go
// ❌ 旧方式：手动 pcall，无调用栈
L.PCall(1, 0, 0)  // 错误信息只有一行，难以定位

// ✅ 新方式：SafeCall 自动 traceback
L.SafeCall(1, 0)   // 错误信息包含完整调用栈
```

### 3.4 请求数据访问

#### 3.4.1 路径安全取值（v1 推荐）

提供 `c:get(path)` 方法，支持点分路径安全访问嵌套 JSON：

```lua
-- Lua 业务代码
local age = c:get("user.info.age")        -- 安全取值，任意层 nil 返回 nil
local name = c:get("user.name") or "匿名"  -- 配合 or 默认值
local id = c:param("id")                   -- 路径参数
local page = c:query("page") or "1"        -- 查询参数
```

Go 侧实现：

```go
// 注册 c:get 方法
func ctxGet(L *lua.State) int {
    c := L.UserValue("gin_ctx").(*gin.Context)
    path := lua.CheckString(L, 1)

    // 惰性解析 JSON body（每请求最多一次）
    body := getOrParseBody(c)

    // 按点分路径安全取值
    val := getNestedSafe(body, strings.Split(path, "."))
    L.PushAny(val) // 自动类型转换
    return 1
}
```

**取值优先级**：

1. JSON Body（点分路径深度遍历）
2. Form / Multipart
3. Query
4. Path Params
5. 未命中 → `nil`

#### 3.4.2 标准访问方法

```lua
-- 路径参数
local id = c:param("id")

-- 查询参数
local page = c:query("page")

-- 表单
local username = c:postForm("username")

-- Header
local token = c:header("Authorization")

-- 完整 body（原始 map）
local body = c:body()
```

### 3.5 响应 API

```lua
-- JSON 响应
c:json(200, {code = 0, msg = "ok", data = result})

-- HTML 响应
c:html(200, "index.html", {title = "首页"})

-- 重定向
c:redirect(302, "/login")

-- 设置 Header
c:setHeader("X-Request-Id", trace_id)

-- 设置 Cookie
c:setCookie("session", token, 3600)
```

### 3.6 Go 绑定注册（WrapSafe + 模块化）

**所有 Go 函数必须经 `WrapSafe` 包装**，确保 panic 不扩散到 Gin：

```go
func RegisterModules(L *lua.State) {
    // 数据库模块
    L.PushAny(map[string]lua.Function{
        "get":   lua.WrapSafe(dbGet),
        "query": lua.WrapSafe(dbQuery),
        "exec":  lua.WrapSafe(dbExec),
    })
    L.SetGlobal("db")

    // Redis 模块
    L.PushAny(map[string]lua.Function{
        "get":    lua.WrapSafe(redisGet),
        "set":    lua.WrapSafe(redisSet),
        "del":    lua.WrapSafe(redisDel),
        "expire": lua.WrapSafe(redisExpire),
    })
    L.SetGlobal("redis")

    // 日志模块（自动注入 trace_id）
    L.PushAny(map[string]lua.Function{
        "info":  lua.WrapSafe(logInfo),
        "warn":  lua.WrapSafe(logWarn),
        "error": lua.WrapSafe(logError),
    })
    L.SetGlobal("log")

    // HTTP Client
    L.PushAny(map[string]lua.Function{
        "get":  lua.WrapSafe(httpGet),
        "post": lua.WrapSafe(httpPost),
    })
    L.SetGlobal("http")
}
```

**`WrapSafe` 的保证**：

```go
// WrapSafe 将任意 Go 函数包装为安全的 Lua 函数
// - Go panic → 转换为 Lua error（不会崩溃进程）
// - 自动 recover + 错误信息保留
wrapped := lua.WrapSafe(func(L *lua.State) int {
    // 即使这里 panic，也只会变成 Lua error
    result := someRiskyOperation()
    L.PushAny(result)
    return 1
})
```

### 3.7 资源管理（PushResource + 自动清理）

对于 DB 连接、文件句柄等需要显式关闭的资源，使用 `PushResource` 系列 API：

```go
// 数据库游标 — GC 时自动关闭
func dbQuery(L *lua.State) int {
    c := L.UserValue("gin_ctx").(*gin.Context)
    sql := lua.CheckString(L, 1)

    rows, err := db.QueryContext(L.Context(), sql)
    if err != nil {
        L.PushNil()
        L.PushString(err.Error())
        return 2
    }

    // rows 实现了 io.Closer
    // PushResource: 即使 Lua 忘记关闭，GC 也会调用 rows.Close()
    L.PushResource(rows)
    return 1
}

// 文件句柄 — 支持 Lua 5.5 <close> 变量
func openFile(L *lua.State) int {
    path := lua.CheckString(L, 1)
    f, err := os.Open(path)
    if err != nil {
        L.PushNil()
        L.PushString(err.Error())
        return 2
    }

    // PushCloseableResource: 同时支持 __gc 和 __close
    // Lua 侧可用: local f <close> = open_file("data.txt")
    L.PushCloseableResource(f)
    return 1
}
```

**资源管理对比**：

| 方式 | 泄漏风险 | Lua 5.5 `<close>` | GC 兜底 |
|------|----------|--------------------|---------| 
| 手动 `:close()` | 高（忘记调用） | ❌ | ❌ |
| `PushResource` | 低 | ❌ | ✅ |
| `PushCloseableResource` | 极低 | ✅ 作用域结束即关闭 | ✅ |

---

## 四、高级功能

### 4.1 热更新（PrepareReload / ReloadModule）

go-lua 提供两阶段热更新，确保生产安全：

```go
// 方式一：两阶段提交（推荐生产环境）
func hotReloadHandler(L *lua.State, moduleName string) error {
    // 阶段一：准备（检查兼容性，不影响运行中请求）
    plan, err := L.PrepareReload(moduleName)
    if err != nil {
        return fmt.Errorf("reload prepare failed: %w", err)
    }

    // 检查是否有不兼容变更
    if plan.HasIncompatible() {
        plan.Abort()
        return fmt.Errorf("incompatible changes detected, reload aborted")
    }

    // 阶段二：提交（原子切换）
    plan.Commit()
    log.Printf("module %s reloaded successfully", moduleName)
    return nil
}

// 方式二：一键重载（适合开发环境）
func devReload(L *lua.State, moduleName string) error {
    result, err := L.ReloadModule(moduleName)
    if err != nil {
        return err
    }
    log.Printf("reload: %+v", result)
    return nil
}
```

**热更新策略**：

| 环境 | 策略 | API |
|------|------|-----|
| 开发 | 文件 `mtime` 监听 → 自动 `ReloadModule` | `ReloadModule` |
| 预发布 | 手动触发 → `PrepareReload` + 兼容性检查 | `PrepareReload` → `Commit` |
| 生产 | 版本路径 + 原子替换 + `PrepareReload` + 审计日志 | `PrepareReload` → 审计 → `Commit` |

**注意**：热更新作用于单个 `State`。在 `StatePool` 场景下，需要对池中所有 State 执行重载，或在 `InitFunc` 中更新加载逻辑，让新 `Get()` 的 State 自动加载新版本。

### 4.2 异步 IO（Future + Scheduler）

对于需要并发 IO 的场景（如同时查询多个服务），使用 Future + Scheduler：

```go
// Go 侧：注册异步 HTTP 请求
func asyncHTTPGet(L *lua.State) int {
    url := lua.CheckString(L, 1)

    future, ctx := lua.NewFutureWithContext(L.Context())

    go func() {
        req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            future.Reject(err)
            return
        }
        defer resp.Body.Close()
        body, _ := io.ReadAll(resp.Body)
        future.Resolve(string(body))
    }()

    // 将 future 推入 Lua 栈
    L.PushAny(future)
    return 1
}
```

```lua
-- Lua 侧：并发请求
local f1 = http.async_get("https://api.example.com/users")
local f2 = http.async_get("https://api.example.com/orders")

-- 等待所有完成
local users = f1:await()
local orders = f2:await()

c:json(200, {users = users, orders = orders})
```

**Scheduler 模式**（适合复杂并发流程）：

```go
sched := lua.NewScheduler(L)
defer sched.Destroy()

handle1, _ := sched.Spawn(L) // 启动协程 1
handle2, _ := sched.Spawn(L) // 启动协程 2

for sched.Tick() {
    // 驱动所有协程前进
}
```

### 4.3 沙箱安全（SandboxConfig）

通过 `StatePool` 的 `Sandbox` 配置或独立创建沙箱 State：

```go
// 方式一：池级沙箱（推荐）
pool := lua.NewStatePool(lua.PoolConfig{
    MaxStates: 32,
    InitFunc:  initFunc,
    Sandbox: &lua.SandboxConfig{
        MemoryLimit: 10 * 1024 * 1024, // 10MB
        CPULimit:    1_000_000,         // 指令数限制
        AllowIO:     false,             // 禁止文件 IO
    },
})

// 方式二：独立沙箱 State（多租户场景）
L := lua.NewSandboxState(lua.SandboxConfig{
    MemoryLimit: 5 * 1024 * 1024,
    CPULimit:    500_000,
    AllowIO:     false,
})
defer L.Close()
```

**沙箱限制清单**：

| 限制 | 说明 |
|------|------|
| `MemoryLimit` | Lua 堆内存上限，超限触发 error |
| `CPULimit` | 指令计数上限，防止死循环 |
| `AllowIO` | 是否允许文件读写 |
| 全局裁剪 | 默认移除 `os.execute`、`io.popen`、`loadfile`（不可信来源）等 |

### 4.4 虚拟文件系统（embed.FS）

使用 `SetFileSystem` 将 Lua 脚本嵌入 Go 二进制：

```go
import "embed"

//go:embed app/**/*.lua
var luaFS embed.FS

func main() {
    pool := lua.NewStatePool(lua.PoolConfig{
        MaxStates: 32,
        InitFunc: func(L *lua.State) {
            // Lua require/dofile 从 embed.FS 读取
            L.SetFileSystem(luaFS)
            binding.RegisterModules(L)
        },
    })
    defer pool.Close()

    r := gin.Default()
    r.Use(LuaMiddleware(pool))
    // ...
    r.Run(":8080")
}
```

**优势**：
- 单二进制部署，无需分发 Lua 文件
- 版本化：Lua 代码与 Go 二进制同版本
- 安全：嵌入的脚本不可被运行时篡改

---

## 五、业务层规范

### 5.1 目录结构

```text
lumi/
├── cmd/web/
│   └── main.go              # 入口：Engine + Pool + 路由挂载
├── pkg/web/
│   ├── pool.go              # StatePool 配置与管理
│   ├── middleware.go         # LuaMiddleware（SetContext + UserValue）
│   ├── handler.go           # SafeCall handler 调度
│   ├── binding/
│   │   ├── context.go       # gin_ctx 代理 + 路径取值
│   │   ├── db.go            # db 模块（WrapSafe）
│   │   ├── redis.go         # redis 模块（WrapSafe）
│   │   ├── http.go          # http 模块（WrapSafe + Future）
│   │   ├── log.go           # log 模块（自动 trace_id）
│   │   └── register.go      # RegisterModules 入口
│   └── reload/
│       └── watcher.go       # 文件监听 + PrepareReload
├── app/                     # Lua 业务代码
│   ├── routes.lua           # 路由声明
│   ├── controllers/
│   │   ├── user.lua
│   │   └── order.lua
│   └── middlewares/
│       └── auth.lua
└── app_test/                # Lua 测试（使用 luatest）
    ├── user_test.lua
    └── order_test.lua
```

### 5.2 Lua 业务示例

**`app/routes.lua`**：

```lua
local router = require("router")

-- 路由分组
local api = router.group("/api/v1")

-- 中间件
api:use(require("middlewares.auth"))

-- 路由注册
api:get("/users/:id", require("controllers.user").get)
api:post("/users", require("controllers.user").create)
api:get("/orders", require("controllers.order").list)
```

**`app/controllers/user.lua`**：

```lua
local M = {}

function M.get(c)
    local id = c:param("id")
    if not id then
        c:json(400, {code = 1, msg = "missing id"})
        return
    end

    local user, err = db.get("SELECT * FROM users WHERE id = ?", id)
    if err then
        log.error("db query failed: " .. err)
        c:json(500, {code = -1, msg = "internal error"})
        return
    end

    if not user then
        c:json(404, {code = 1, msg = "user not found"})
        return
    end

    c:json(200, {code = 0, msg = "ok", data = user})
end

function M.create(c)
    local name = c:get("name")
    local email = c:get("email")

    -- 校验
    if not name or #name == 0 then
        c:json(400, {code = 1, msg = "name is required"})
        return
    end

    local result, err = db.exec(
        "INSERT INTO users (name, email) VALUES (?, ?)",
        name, email
    )
    if err then
        log.error("create user failed: " .. err)
        c:json(500, {code = -1, msg = "internal error"})
        return
    end

    c:json(201, {code = 0, msg = "created", data = {id = result.last_id}})
end

return M
```

**`app/middlewares/auth.lua`**：

```lua
return function(c)
    local token = c:header("Authorization")
    if not token then
        c:json(401, {code = -1, msg = "unauthorized"})
        c:abort()
        return
    end

    local claims, err = jwt.verify(token)
    if err then
        c:json(401, {code = -1, msg = "invalid token"})
        c:abort()
        return
    end

    c:set("user_id", claims.user_id)
    c:next()
end
```

### 5.3 开发注意事项

1. **禁止全局可变状态**：不要在 `_G` 上存储请求数据。跨文件共享用 `require` 返回只读模块表。
2. **资源必须托管**：所有 DB 游标、文件句柄必须通过 `PushResource` / `PushCloseableResource` 推入 Lua，确保 GC 兜底。
3. **超时感知**：长时间操作应检查 `ctx` 是否已取消（Go 侧使用 `L.Context()`）。
4. **错误处理**：校验失败统一 `c:json`；Go 绑定 panic 由 `WrapSafe` 捕获转为 Lua error；`SafeCall` 自动附加 traceback。

---

## 六、测试策略

### 6.1 使用 luatest 测试 Handler

lumi 自带的 `luatest` 框架支持 Lua 单元测试：

```lua
-- app_test/user_test.lua
local T = require("luatest")

T.test("user.get returns 404 for missing user", function(t)
    local mock_c = t:mock_context({
        params = {id = "999"},
    })

    -- 模拟 db.get 返回 nil
    t:mock("db", "get", function() return nil, nil end)

    local user = require("controllers.user")
    user.get(mock_c)

    t:assert_status(mock_c, 404)
    t:assert_json(mock_c, {code = 1, msg = "user not found"})
end)
```

### 6.2 使用 luadebug 分析性能

```lua
local debug = require("luadebug")

-- 性能分析
debug.profile(function()
    -- 被测代码
    handle_request(mock_c)
end)
-- 输出: 函数调用次数、耗时排名
```

### 6.3 使用 RefTracker 检测泄漏

在开发/测试环境启用引用追踪，检测资源泄漏：

```go
func setupDevMode(pool *lua.StatePool) {
    tracker := lua.NewRefTracker()

    // 在测试结束时检查泄漏
    defer func() {
        leaks := tracker.Leaks()
        if len(leaks) > 0 {
            log.Printf("⚠️  Detected %d ref leaks:", len(leaks))
            for _, leak := range leaks {
                log.Printf("  - %v", leak)
            }
        }
    }()
}
```

**建议**：在 CI 中启用 `RefTracker`，泄漏数 > 0 则构建失败。

---

## 七、部署与运维

### 7.1 单二进制 + embed 部署

```go
//go:embed app/**/*.lua
var luaFS embed.FS

func main() {
    pool := lua.NewStatePool(lua.PoolConfig{
        MaxStates: runtime.NumCPU() * 2,
        InitFunc: func(L *lua.State) {
            L.SetFileSystem(luaFS)  // Lua 脚本从 embed 读取
            binding.RegisterModules(L)
        },
        Sandbox: &lua.SandboxConfig{
            MemoryLimit: 10 * 1024 * 1024,
            CPULimit:    1_000_000,
            AllowIO:     false,
        },
    })
    defer pool.Close()

    r := gin.Default()
    r.Use(gin.Recovery(), LuaMiddleware(pool))
    setupRoutes(r, pool)
    r.Run(":" + os.Getenv("PORT"))
}
```

**部署产物**：仅一个二进制文件，包含所有 Lua 脚本。

### 7.2 配置管理

```yaml
# config.yaml
server:
  port: 8080
  mode: release        # gin mode

lua:
  pool_size: 32        # StatePool MaxStates
  memory_limit: 10MB   # 沙箱内存限制
  cpu_limit: 1000000   # 沙箱指令限制
  allow_io: false

db:
  dsn: "${DB_DSN}"
redis:
  addr: "${REDIS_ADDR}"
```

敏感配置（DSN、密钥）通过环境变量注入 Go，Lua 侧不可见。

### 7.3 可观测性

| 维度 | 方案 |
|------|------|
| 链路追踪 | OpenTelemetry middleware → `trace_id` 注入 `log` 模块 |
| 指标 | Prometheus：池使用率、handler 延迟、SafeCall 错误率 |
| 日志 | 结构化 JSON 日志，`trace_id` + `handler_name` 字段 |
| 健康检查 | `/health` 端点检查池可用性 + DB/Redis 连通性 |

```go
// Prometheus 指标示例
var (
    poolUsage = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "lua_pool_active_states",
        Help: "Number of active Lua states from pool",
    })
    handlerDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "lua_handler_duration_seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"handler"},
    )
    safecallErrors = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "lua_safecall_errors_total",
        },
        []string{"handler"},
    )
)
```

---

## 八、落地检查清单

### 安全

- [ ] 所有 Go 绑定函数经 `WrapSafe` 包装
- [ ] `StatePool` 配置 `SandboxConfig`（内存 + CPU 限制）
- [ ] Lua 全局环境已裁剪（移除 `os.execute`、`io.popen`、`loadstring` 等）
- [ ] SQL 查询全部参数化，禁止 Lua 拼接 SQL
- [ ] Redis key 前缀在 Go 层强制
- [ ] JWT 密钥仅存在于 Go 侧

### 生命周期

- [ ] 中间件使用 `L.SetContext(c.Request.Context())` 传播超时
- [ ] 归还前 `DeleteUserValue("gin_ctx")` + `SetTop(0)`
- [ ] Handler 使用 `SafeCall` 调用（自动 traceback）
- [ ] DB/文件资源使用 `PushResource` / `PushCloseableResource`

### 性能

- [ ] `StatePool.MaxStates` 根据 CPU 核数调优
- [ ] JSON body 每请求最多解析一次（惰性缓存）
- [ ] 重 CPU / 大批量 SQL 下沉到 Go 粗粒度 API

### 热更新

- [ ] 生产环境使用 `PrepareReload` + 兼容性检查
- [ ] 热更新操作记录审计日志
- [ ] 多实例部署时热更新配合分发策略

### 测试

- [ ] Handler 有 `luatest` 单元测试
- [ ] CI 启用 `RefTracker` 检测泄漏
- [ ] 压测：池大小、GC、典型接口 P99 达标

### 部署

- [ ] 使用 `embed.FS` + `SetFileSystem` 单二进制部署
- [ ] 敏感配置通过环境变量注入，Lua 不可见
- [ ] OpenTelemetry 链路追踪已集成

---

*文档版本：lumi `pkg/web/` 模块设计方案，对齐 go-lua v0.9.9 安全 API。*
