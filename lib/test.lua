-- lumi test framework
-- Usage:
--   local test = require("lumi.test")
--   test.describe("MyModule", function()
--     test.it("does something", function()
--       test.assert(true)
--     end)
--   end)

local M = {}

-- Internal state
local suites = {}        -- collected test suites
local current_suite = nil
local results = {}       -- { suite, name, passed, error, duration }

-- Test collection API
function M.describe(name, fn)
    local suite = {
        name = name,
        tests = {},
        before_each = nil,
        after_each = nil,
        before_all = nil,
        after_all = nil,
    }
    local prev = current_suite
    current_suite = suite
    fn()
    current_suite = prev
    suites[#suites + 1] = suite
end

function M.it(name, fn)
    if not current_suite then
        error("it() must be called inside describe()")
    end
    current_suite.tests[#current_suite.tests + 1] = {
        name = name,
        fn = fn,
    }
end

function M.before_each(fn)
    if not current_suite then error("before_each() must be inside describe()") end
    current_suite.before_each = fn
end

function M.after_each(fn)
    if not current_suite then error("after_each() must be inside describe()") end
    current_suite.after_each = fn
end

function M.before_all(fn)
    if not current_suite then error("before_all() must be inside describe()") end
    current_suite.before_all = fn
end

function M.after_all(fn)
    if not current_suite then error("after_all() must be inside describe()") end
    current_suite.after_all = fn
end

-- Assertions
function M.assert(cond, msg)
    if not cond then
        error(msg or "assertion failed", 2)
    end
end

function M.assert_eq(actual, expected, msg)
    if actual ~= expected then
        local m = msg and (msg .. ": ") or ""
        error(string.format("%sexpected %s, got %s", m, tostring(expected), tostring(actual)), 2)
    end
end

function M.assert_ne(actual, expected, msg)
    if actual == expected then
        local m = msg and (msg .. ": ") or ""
        error(string.format("%sexpected not %s", m, tostring(expected)), 2)
    end
end

function M.assert_nil(val, msg)
    if val ~= nil then
        error((msg or "expected nil") .. ", got " .. tostring(val), 2)
    end
end

function M.assert_not_nil(val, msg)
    if val == nil then
        error(msg or "expected non-nil value", 2)
    end
end

function M.assert_error(fn, expected_msg)
    local ok, err = pcall(fn)
    if ok then
        error("expected error but function succeeded", 2)
    end
    if expected_msg and not string.find(tostring(err), expected_msg, 1, true) then
        error(string.format("expected error containing %q, got %q", expected_msg, tostring(err)), 2)
    end
end

function M.assert_type(val, expected_type, msg)
    local actual = type(val)
    if actual ~= expected_type then
        local m = msg and (msg .. ": ") or ""
        error(string.format("%sexpected type %s, got %s", m, expected_type, actual), 2)
    end
end

function M.assert_gt(a, b, msg)
    if not (a > b) then
        error(string.format("%s: expected %s > %s", msg or "assert_gt", tostring(a), tostring(b)), 2)
    end
end

function M.assert_lt(a, b, msg)
    if not (a < b) then
        error(string.format("%s: expected %s < %s", msg or "assert_lt", tostring(a), tostring(b)), 2)
    end
end

function M.assert_contains(str, substr, msg)
    if type(str) ~= "string" then
        error("assert_contains: first argument must be a string", 2)
    end
    if not string.find(str, substr, 1, true) then
        error(string.format("%s: %q does not contain %q", msg or "assert_contains", str, substr), 2)
    end
end

-- Test execution (called by Go runner)
function M.run()
    results = {}
    local passed, failed, errored = 0, 0, 0

    for _, suite in ipairs(suites) do
        if suite.before_all then
            local ok, err = pcall(suite.before_all)
            if not ok then
                -- All tests in suite fail
                for _, t in ipairs(suite.tests) do
                    results[#results + 1] = {
                        suite = suite.name,
                        name = t.name,
                        passed = false,
                        error = "before_all failed: " .. tostring(err),
                    }
                    errored = errored + 1
                end
                goto continue_suite
            end
        end

        for _, t in ipairs(suite.tests) do
            -- before_each
            if suite.before_each then
                local ok, err = pcall(suite.before_each)
                if not ok then
                    results[#results + 1] = {
                        suite = suite.name,
                        name = t.name,
                        passed = false,
                        error = "before_each failed: " .. tostring(err),
                    }
                    errored = errored + 1
                    goto continue_test
                end
            end

            -- run test
            local start = os.clock()
            local ok, err = xpcall(t.fn, debug.traceback)
            local duration = os.clock() - start

            if ok then
                results[#results + 1] = {
                    suite = suite.name,
                    name = t.name,
                    passed = true,
                    duration = duration,
                }
                passed = passed + 1
            else
                results[#results + 1] = {
                    suite = suite.name,
                    name = t.name,
                    passed = false,
                    error = tostring(err),
                    duration = duration,
                }
                failed = failed + 1
            end

            -- after_each
            if suite.after_each then
                pcall(suite.after_each)
            end

            ::continue_test::
        end

        if suite.after_all then
            pcall(suite.after_all)
        end

        ::continue_suite::
    end

    return {
        results = results,
        passed = passed,
        failed = failed,
        errored = errored,
        total = passed + failed + errored,
    }
end

-- Reset state (for running multiple test files)
function M.reset()
    suites = {}
    current_suite = nil
    results = {}
end

return M
