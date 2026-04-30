-- Request logger middleware
-- Demonstrates the middleware pattern: call c:next() to continue the chain.

local log = require("log")

function request_logger(c)
    local start = os.clock()
    c:next()
    local duration = os.clock() - start
    log.info("request", {
        method = c:method(),
        path = c:path(),
        duration_ms = math.floor(duration * 1000),
    })
end
