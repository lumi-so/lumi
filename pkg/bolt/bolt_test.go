package bolt

import (
	"path/filepath"
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// newLuaState creates a Lua state with the bolt module registered.
func newLuaState(t *testing.T) *lua.State {
	t.Helper()

	L := lua.NewState()
	t.Cleanup(func() {
		L.Close()
	})

	Register(L, Config{})
	return L
}

// tempDBPath returns a path for a test database in a temp directory.
func tempDBPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "test.db")
}

// ---------------------------------------------------------------------------
// TestOpenClose
// ---------------------------------------------------------------------------

func TestOpenClose(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(db ~= nil, "db should not be nil")
		assert(err == nil, "err should be nil: " .. tostring(err))
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenReadOnly
// ---------------------------------------------------------------------------

func TestOpenReadOnly(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	// First create the DB
	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(db ~= nil)
		db:close()
	`
	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	// Now open read-only
	L2 := newLuaState(t)
	code2 := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `", "r")
		assert(db ~= nil, "db should not be nil")
		assert(err == nil, "err should be nil: " .. tostring(err))
		db:close()
	`
	if err := L2.DoString(code2); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenInvalidMode
// ---------------------------------------------------------------------------

func TestOpenInvalidMode(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `", "x")
		assert(db == nil, "db should be nil for invalid mode")
		assert(err ~= nil, "err should not be nil")
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenWithOptions
// ---------------------------------------------------------------------------

func TestOpenWithOptions(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `", "c", {timeout = 1000, page_size = 4096})
		assert(db ~= nil, "db should not be nil")
		assert(err == nil, "err should be nil: " .. tostring(err))
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestPutGet
// ---------------------------------------------------------------------------

func TestPutGet(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil, "open failed: " .. tostring(err))

		-- Create a bucket first via a write tx
		local tx, err = db:begin_write()
		assert(err == nil, "begin_write failed: " .. tostring(err))
		local b, err = tx:create_bucket("data")
		assert(err == nil, "create_bucket failed: " .. tostring(err))
		assert(b ~= nil, "bucket should not be nil")
		local ok, err = tx:commit()
		assert(ok == true, "commit failed: " .. tostring(err))

		-- Now put and get
		local ok, err = db:put("data", "hello", "world")
		assert(ok == true, "put failed: " .. tostring(err))

		local val = db:get("data", "hello")
		assert(val == "world", "expected 'world', got: " .. tostring(val))

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestGetNonExistent
// ---------------------------------------------------------------------------

func TestGetNonExistent(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		tx:create_bucket("data")
		tx:commit()

		local val = db:get("data", "nonexistent")
		assert(val == nil, "expected nil for nonexistent key, got: " .. tostring(val))

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestDelete
// ---------------------------------------------------------------------------

func TestDelete(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		tx:create_bucket("data")
		tx:commit()

		db:put("data", "key1", "value1")
		assert(db:get("data", "key1") == "value1")

		local ok, err = db:delete("data", "key1")
		assert(ok == true, "delete failed: " .. tostring(err))
		assert(db:get("data", "key1") == nil)

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestList
// ---------------------------------------------------------------------------

func TestList(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		tx:create_bucket("data")
		tx:commit()

		db:put("data", "a", "1")
		db:put("data", "b", "2")
		db:put("data", "c", "3")

		local keys = db:list("data")
		assert(#keys == 3, "expected 3 keys, got " .. tostring(#keys))
		assert(keys[1] == "a")
		assert(keys[2] == "b")
		assert(keys[3] == "c")

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestListPrefix
// ---------------------------------------------------------------------------

func TestListPrefix(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		tx:create_bucket("data")
		tx:commit()

		db:put("data", "user:1", "alice")
		db:put("data", "user:2", "bob")
		db:put("data", "post:1", "hello")

		local keys = db:list_prefix("data", "user:")
		assert(#keys == 2, "expected 2 keys, got " .. tostring(#keys))
		assert(keys[1] == "user:1")
		assert(keys[2] == "user:2")

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestBucketExists
// ---------------------------------------------------------------------------

func TestBucketExists(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		assert(db:bucket_exists("data") == false)

		local tx, err = db:begin_write()
		assert(err == nil)
		tx:create_bucket("data")
		tx:commit()

		assert(db:bucket_exists("data") == true)
		assert(db:bucket_exists("other") == false)

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestTransactionRead
// ---------------------------------------------------------------------------

func TestTransactionRead(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		-- Setup: create bucket with data
		local tx, err = db:begin_write()
		assert(err == nil)
		local b, err = tx:create_bucket("data")
		b:put("k", "v")
		tx:commit()

		-- Read transaction
		local tx, err = db:begin_read()
		assert(err == nil)
		assert(tx:writable() == false)

		local b = tx:bucket("data")
		assert(b ~= nil, "bucket should not be nil")
		assert(b:get("k") == "v")

		tx:close()

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestTransactionWrite
// ---------------------------------------------------------------------------

func TestTransactionWrite(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		assert(tx:writable() == true)

		local b, err = tx:create_bucket("mydata")
		assert(err == nil)
		assert(b ~= nil)

		local ok, err = b:put("key", "value")
		assert(ok == true, "put failed: " .. tostring(err))

		local ok, err = tx:commit()
		assert(ok == true, "commit failed: " .. tostring(err))

		-- Verify persisted
		assert(db:get("mydata", "key") == "value")

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestTransactionRollback
// ---------------------------------------------------------------------------

func TestTransactionRollback(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		-- Setup bucket
		local tx, err = db:begin_write()
		tx:create_bucket("data")
		tx:commit()

		-- Write in a transaction then rollback
		local tx, err = db:begin_write()
		assert(err == nil)
		local b = tx:bucket("data")
		b:put("key", "ghost")
		tx:close()  -- rollback (not committed)

		-- Verify not persisted
		assert(db:get("data", "key") == nil)

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestCursor
// ---------------------------------------------------------------------------

func TestCursor(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		local b, err = tx:create_bucket("data")
		b:put("a", "1")
		b:put("b", "2")
		b:put("c", "3")
		tx:commit()

		-- Cursor iteration
		local tx, err = db:begin_read()
		local b = tx:bucket("data")
		local c = b:cursor()

		local k, v = c:first()
		assert(k == "a" and v == "1", "first failed: " .. tostring(k) .. "=" .. tostring(v))

		k, v = c:next()
		assert(k == "b" and v == "2", "next failed: " .. tostring(k) .. "=" .. tostring(v))

		k, v = c:next()
		assert(k == "c" and v == "3", "next2 failed: " .. tostring(k) .. "=" .. tostring(v))

		k, v = c:next()
		assert(k == nil and v == nil, "should be nil at end")

		-- Seek
		k, v = c:seek("b")
		assert(k == "b" and v == "2", "seek b failed: " .. tostring(k) .. "=" .. tostring(v))

		-- Seek past end
		k, v = c:seek("z")
		assert(k == nil and v == nil, "seek z should be nil")

		-- Last
		k, v = c:last()
		assert(k == "c" and v == "3", "last failed: " .. tostring(k) .. "=" .. tostring(v))

		-- Prev
		k, v = c:prev()
		assert(k == "b" and v == "2", "prev failed: " .. tostring(k) .. "=" .. tostring(v))

		tx:close()
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestBatchWrite
// ---------------------------------------------------------------------------

func TestBatchWrite(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		-- Batch write: auto-commits on close
		local tx, err = db:begin_batch()
		assert(err == nil)
		assert(tx:writable() == true)

		local b, err = tx:create_bucket("batchdata")
		assert(err == nil)
		b:put("k", "batch-value")

		-- close auto-commits for batch
		local ok, err = tx:close()
		assert(ok == true, "batch close failed: " .. tostring(err))

		-- Verify persisted
		assert(db:get("batchdata", "k") == "batch-value")

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestSequence
// ---------------------------------------------------------------------------

func TestSequence(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		local b, err = tx:create_bucket("seq")
		assert(err == nil)

		local s1, err = b:sequence()
		assert(err == nil, "sequence failed: " .. tostring(err))
		assert(s1 == 1, "expected 1, got " .. tostring(s1))

		local s2, err = b:sequence()
		assert(s2 == 2, "expected 2, got " .. tostring(s2))

		local s3, err = b:sequence()
		assert(s3 == 3, "expected 3, got " .. tostring(s3))

		tx:commit()
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestForEach
// ---------------------------------------------------------------------------

func TestForEach(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		local b, err = tx:create_bucket("data")
		b:put("x", "1")
		b:put("y", "2")
		b:put("z", "3")
		tx:commit()

		local tx, err = db:begin_read()
		local b = tx:bucket("data")

		local count = 0
		local seen = {}
		local ok, err = b:for_each(function(k, v)
			count = count + 1
			seen[k] = v
		end)
		assert(ok == true, "for_each failed: " .. tostring(err))
		assert(count == 3, "expected 3, got " .. tostring(count))
		assert(seen["x"] == "1")
		assert(seen["y"] == "2")
		assert(seen["z"] == "3")

		tx:close()
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestForEachStop
// ---------------------------------------------------------------------------

func TestForEachStop(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		local b, err = tx:create_bucket("data")
		b:put("a", "1")
		b:put("b", "2")
		b:put("c", "3")
		tx:commit()

		local tx, err = db:begin_read()
		local b = tx:bucket("data")

		local count = 0
		local ok, err = b:for_each(function(k, v)
			count = count + 1
			return true  -- stop after first
		end)
		assert(ok == true, "for_each failed: " .. tostring(err))
		assert(count == 1, "expected 1 (stopped), got " .. tostring(count))

		tx:close()
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestSync
// ---------------------------------------------------------------------------

func TestSync(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local ok, err = db:sync()
		assert(ok == true, "sync failed: " .. tostring(err))

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestPath
// ---------------------------------------------------------------------------

func TestPath(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local p = db:path()
		assert(p == "` + path + `", "path mismatch: " .. tostring(p))

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestCreateBucketIfNotExists
// ---------------------------------------------------------------------------

func TestCreateBucketIfNotExists(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)

		local b1, err = tx:create_bucket_if_not_exists("stuff")
		assert(err == nil)
		assert(b1 ~= nil)

		-- Second call should not error
		local b2, err = tx:create_bucket_if_not_exists("stuff")
		assert(err == nil)
		assert(b2 ~= nil)

		tx:commit()
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestDeleteBucket
// ---------------------------------------------------------------------------

func TestDeleteBucket(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		tx:create_bucket("todelete")
		tx:commit()

		assert(db:bucket_exists("todelete") == true)

		local tx, err = db:begin_write()
		assert(err == nil)
		local ok, err = tx:delete_bucket("todelete")
		assert(ok == true, "delete_bucket failed: " .. tostring(err))
		tx:commit()

		assert(db:bucket_exists("todelete") == false)

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestCursorDelete
// ---------------------------------------------------------------------------

func TestCursorDelete(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		local b, err = tx:create_bucket("data")
		b:put("a", "1")
		b:put("b", "2")
		b:put("c", "3")
		tx:commit()

		-- Delete via cursor
		local tx, err = db:begin_write()
		local b = tx:bucket("data")
		local c = b:cursor()

		c:seek("b")
		local ok, err = c:delete()
		assert(ok == true, "cursor delete failed: " .. tostring(err))

		tx:commit()

		-- Verify b is gone
		assert(db:get("data", "b") == nil)
		assert(db:get("data", "a") == "1")
		assert(db:get("data", "c") == "3")

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestPutToMissingBucket
// ---------------------------------------------------------------------------

func TestPutToMissingBucket(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local ok, err = db:put("nonexistent", "k", "v")
		assert(ok == nil, "expected nil on missing bucket")
		assert(err ~= nil, "expected error on missing bucket")

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestReadTxCommitFails
// ---------------------------------------------------------------------------

func TestReadTxCommitFails(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		tx:create_bucket("data")
		tx:commit()

		local tx, err = db:begin_read()
		assert(err == nil)

		local ok, err = tx:commit()
		assert(ok == nil, "commit on read tx should fail")
		assert(err ~= nil, "expected error")

		tx:close()
		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestBinaryData
// ---------------------------------------------------------------------------

func TestBinaryData(t *testing.T) {
	L := newLuaState(t)
	path := tempDBPath(t)

	code := `
		local bolt = require("bolt")
		local db, err = bolt.open("` + path + `")
		assert(err == nil)

		local tx, err = db:begin_write()
		assert(err == nil)
		local b, err = tx:create_bucket("data")
		assert(err == nil)

		-- Binary data with null bytes and high bytes
		local binary = string.char(0, 1, 2, 255, 254, 253)
		b:put("bin", binary)
		tx:commit()

		local val = db:get("data", "bin")
		assert(#val == 6, "expected 6 bytes, got " .. tostring(#val))
		assert(string.byte(val, 1) == 0)
		assert(string.byte(val, 2) == 1)
		assert(string.byte(val, 3) == 2)
		assert(string.byte(val, 4) == 255)
		assert(string.byte(val, 5) == 254)
		assert(string.byte(val, 6) == 253)

		db:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}
