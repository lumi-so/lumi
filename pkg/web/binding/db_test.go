package binding

import (
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	lua "github.com/akzj/go-lua/pkg/lua"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	// Create test table
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT,
			age INTEGER
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert test data
	_, err = db.Exec(`INSERT INTO users (name, email, age) VALUES ('Alice', 'alice@test.com', 30)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO users (name, email, age) VALUES ('Bob', 'bob@test.com', 25)`)
	if err != nil {
		t.Fatal(err)
	}

	return db
}

func setupTestState(t *testing.T, db *sql.DB) *lua.State {
	t.Helper()
	L := lua.NewState()
	RegisterDB(L, DBConfig{DB: db, MaxQueryRows: 100})
	return L
}

func TestDBGet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local user = db.get("SELECT * FROM users WHERE id = ?", 1)
		assert(user ~= nil, "user should not be nil")
		assert(user.name == "Alice", "name should be Alice, got: " .. tostring(user.name))
		assert(user.age == 30, "age should be 30, got: " .. tostring(user.age))
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBGet_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local user = db.get("SELECT * FROM users WHERE id = ?", 999)
		assert(user == nil, "user should be nil for non-existent id")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local users = db.query("SELECT * FROM users ORDER BY id")
		assert(#users == 2, "should have 2 users, got: " .. #users)
		assert(users[1].name == "Alice")
		assert(users[2].name == "Bob")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBExec(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local result = db.exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", "Charlie", "c@test.com", 35)
		assert(result.affected == 1, "should affect 1 row")
		assert(result.last_id == 3, "last_id should be 3")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBTx_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local result = db.tx(function(tx)
			tx:exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", "Charlie", "c@test.com", 35)
			tx:exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", "Dave", "d@test.com", 40)
			return {inserted = 2}
		end)
		assert(result ~= nil, "result should not be nil")
		assert(result.inserted == 2, "should have inserted 2")

		-- Verify data is committed
		local users = db.query("SELECT * FROM users")
		assert(#users == 4, "should have 4 users after tx, got: " .. #users)
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBTx_Rollback(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local result, err = db.tx(function(tx)
			tx:exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", "Charlie", "c@test.com", 35)
			error("something went wrong")
		end)
		assert(result == nil, "result should be nil on error")
		assert(err ~= nil, "err should not be nil")

		-- Verify rollback: still only 2 users
		local users = db.query("SELECT * FROM users")
		assert(#users == 2, "should still have 2 users after rollback, got: " .. #users)
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBTx_ReadInTx(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local result = db.tx(function(tx)
			tx:exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", "Charlie", "c@test.com", 35)
			local user = tx:get("SELECT * FROM users WHERE name = ?", "Charlie")
			assert(user ~= nil, "should see inserted row within tx")
			assert(user.name == "Charlie")
			return user
		end)
		assert(result.name == "Charlie")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBBegin_Manual(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local tx = db.begin()
		tx:exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", "Eve", "e@test.com", 28)
		tx:commit()

		local users = db.query("SELECT * FROM users")
		assert(#users == 3, "should have 3 users after manual commit")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBBegin_ManualRollback(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	L := setupTestState(t, db)
	defer L.Close()

	err := L.DoString(`
		local db = require("db")
		local tx = db.begin()
		tx:exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", "Eve", "e@test.com", 28)
		tx:rollback()

		local users = db.query("SELECT * FROM users")
		assert(#users == 2, "should still have 2 users after rollback")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDBQuery_MaxRows(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert many rows
	for i := 0; i < 50; i++ {
		_, err := db.Exec("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", fmt.Sprintf("user%d", i), "x@x.com", i)
		if err != nil {
			t.Fatal(err)
		}
	}

	L := lua.NewState()
	defer L.Close()
	RegisterDB(L, DBConfig{DB: db, MaxQueryRows: 10})

	err := L.DoString(`
		local db = require("db")
		local users = db.query("SELECT * FROM users")
		assert(#users == 10, "should be limited to 10 rows, got: " .. #users)
	`)
	if err != nil {
		t.Fatal(err)
	}
}
