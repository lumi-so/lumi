local test = require("lumi.test")

test.describe("Example", function()
    test.it("basic math", function()
        test.assert_eq(2 + 2, 4)
    end)

    test.it("string operations", function()
        local s = "hello" .. " " .. "world"
        test.assert_eq(s, "hello world")
        test.assert_contains(s, "world")
    end)

    test.it("table operations", function()
        local t = {1, 2, 3}
        test.assert_eq(#t, 3)
        test.assert_type(t, "table")
    end)
end)
