package binding

import (
	"database/sql"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// DBConfig configures the database binding.
type DBConfig struct {
	// DB is the database connection pool. Required.
	DB *sql.DB

	// MaxQueryRows limits rows returned by a single query. Default: 10000.
	MaxQueryRows int
}

// RegisterDB registers the "db" module in the Lua state.
// Provides: db.get, db.query, db.exec, db.tx, db.begin
func RegisterDB(L *lua.State, cfg DBConfig) {
	if cfg.MaxQueryRows <= 0 {
		cfg.MaxQueryRows = 10000
	}

	lua.RegisterModule(L, "db", map[string]lua.Function{
		"get":   lua.WrapSafe(dbGet(cfg)),
		"query": lua.WrapSafe(dbQuery(cfg)),
		"exec":  lua.WrapSafe(dbExec(cfg)),
		"tx":    lua.WrapSafe(dbTx(cfg)),
		"begin": lua.WrapSafe(dbBegin(cfg)),
	})
}

// dbGet implements db.get(sql, ...args) → row or nil, err
func dbGet(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		query := L.CheckString(1)
		args := collectArgs(L, 2)

		rows, err := cfg.DB.QueryContext(L.Context(), query, args...)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		if !rows.Next() {
			L.PushNil() // no row found
			return 1
		}

		row, err := scanRow(rows, cols)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		L.PushAny(row)
		return 1
	}
}

// dbQuery implements db.query(sql, ...args) → [{...}, ...] or nil, err
func dbQuery(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		query := L.CheckString(1)
		args := collectArgs(L, 2)

		rows, err := cfg.DB.QueryContext(L.Context(), query, args...)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		var results []map[string]any
		count := 0
		for rows.Next() {
			if count >= cfg.MaxQueryRows {
				break
			}
			row, err := scanRow(rows, cols)
			if err != nil {
				L.PushNil()
				L.PushString(err.Error())
				return 2
			}
			results = append(results, row)
			count++
		}

		if err := rows.Err(); err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		L.PushAny(results)
		return 1
	}
}

// dbExec implements db.exec(sql, ...args) → {affected=n, last_id=n} or nil, err
func dbExec(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		query := L.CheckString(1)
		args := collectArgs(L, 2)

		result, err := cfg.DB.ExecContext(L.Context(), query, args...)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		affected, _ := result.RowsAffected()
		lastID, _ := result.LastInsertId()

		L.PushAny(map[string]any{
			"affected": affected,
			"last_id":  lastID,
		})
		return 1
	}
}

// dbTx implements db.tx(callback) → result or nil, err
// Auto-commits on success, auto-rollbacks on error.
func dbTx(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		// Arg 1 must be a function
		if L.Type(1) != lua.TypeFunction {
			L.PushNil()
			L.PushString("db.tx: argument must be a function")
			return 2
		}

		tx, err := cfg.DB.BeginTx(L.Context(), nil)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		// Push the tx proxy as argument to callback
		pushTxProxy(L, tx, cfg)
		txIdx := L.GetTop()

		// Call: callback(tx)
		L.PushValue(1)     // push callback function
		L.PushValue(txIdx) // push tx proxy
		status := L.PCall(1, 1, 0)

		// Remove tx proxy from below
		L.Remove(txIdx)

		if status != lua.OK {
			// Error → rollback
			tx.Rollback()
			errMsg, _ := L.ToString(-1)
			L.Pop(1)
			L.PushNil()
			L.PushString(errMsg)
			return 2
		}

		// Success → commit
		if err := tx.Commit(); err != nil {
			L.Pop(1) // pop callback result
			L.PushNil()
			L.PushString("commit failed: " + err.Error())
			return 2
		}

		// Return callback's return value (already on stack from PCall)
		return 1
	}
}

// dbBegin implements db.begin() → tx or nil, err (manual transaction mode)
func dbBegin(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		tx, err := cfg.DB.BeginTx(L.Context(), nil)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		pushTxProxy(L, tx, cfg)
		return 1
	}
}

// txWrapper wraps *sql.Tx with auto-rollback on Close (for GC safety).
type txWrapper struct {
	tx       *sql.Tx
	finished bool
}

func (w *txWrapper) Close() error {
	if !w.finished {
		w.finished = true
		return w.tx.Rollback()
	}
	return nil
}

func pushTxProxy(L *lua.State, tx *sql.Tx, cfg DBConfig) {
	wrapper := &txWrapper{tx: tx}
	L.PushUserdata(wrapper)

	if L.NewMetatable("lumi.db.tx") {
		methods := map[string]lua.Function{
			"get":      lua.WrapSafe(txGet(cfg)),
			"query":    lua.WrapSafe(txQuery(cfg)),
			"exec":     lua.WrapSafe(txExec(cfg)),
			"commit":   lua.WrapSafe(txCommit),
			"rollback": lua.WrapSafe(txRollback),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")

		// GC: auto-rollback if not committed
		L.PushFunction(lua.WrapSafe(txGC))
		L.SetField(-2, "__gc")
	}
	L.SetMetatable(-2)
}

func getTxWrapper(L *lua.State) *txWrapper {
	return L.UserdataValue(1).(*txWrapper)
}

func txGet(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		w := getTxWrapper(L)
		query := L.CheckString(2)
		args := collectArgs(L, 3)

		rows, err := w.tx.QueryContext(L.Context(), query, args...)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		if !rows.Next() {
			L.PushNil()
			return 1
		}

		row, err := scanRow(rows, cols)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}
		L.PushAny(row)
		return 1
	}
}

func txQuery(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		w := getTxWrapper(L)
		query := L.CheckString(2)
		args := collectArgs(L, 3)

		rows, err := w.tx.QueryContext(L.Context(), query, args...)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		var results []map[string]any
		count := 0
		for rows.Next() {
			if count >= cfg.MaxQueryRows {
				break
			}
			row, err := scanRow(rows, cols)
			if err != nil {
				L.PushNil()
				L.PushString(err.Error())
				return 2
			}
			results = append(results, row)
			count++
		}

		if err := rows.Err(); err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		L.PushAny(results)
		return 1
	}
}

func txExec(cfg DBConfig) lua.Function {
	return func(L *lua.State) int {
		w := getTxWrapper(L)
		query := L.CheckString(2)
		args := collectArgs(L, 3)

		result, err := w.tx.ExecContext(L.Context(), query, args...)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		affected, _ := result.RowsAffected()
		lastID, _ := result.LastInsertId()
		L.PushAny(map[string]any{
			"affected": affected,
			"last_id":  lastID,
		})
		return 1
	}
}

func txCommit(L *lua.State) int {
	w := getTxWrapper(L)
	if w.finished {
		L.PushNil()
		L.PushString("transaction already finished")
		return 2
	}
	w.finished = true
	if err := w.tx.Commit(); err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

func txRollback(L *lua.State) int {
	w := getTxWrapper(L)
	if w.finished {
		L.PushNil()
		L.PushString("transaction already finished")
		return 2
	}
	w.finished = true
	if err := w.tx.Rollback(); err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

func txGC(L *lua.State) int {
	w := L.UserdataValue(1).(*txWrapper)
	w.Close() // auto-rollback if not finished
	return 0
}

// collectArgs reads variadic Lua arguments starting from startIdx.
func collectArgs(L *lua.State, startIdx int) []any {
	top := L.GetTop()
	if startIdx > top {
		return nil
	}
	args := make([]any, 0, top-startIdx+1)
	for i := startIdx; i <= top; i++ {
		args = append(args, L.ToAny(i))
	}
	return args
}

// scanRow scans a single row into map[string]any.
func scanRow(rows *sql.Rows, cols []string) (map[string]any, error) {
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}

	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}

	row := make(map[string]any, len(cols))
	for i, col := range cols {
		val := values[i]
		// Convert []byte to string for readability
		if b, ok := val.([]byte); ok {
			row[col] = string(b)
		} else {
			row[col] = val
		}
	}
	return row, nil
}
