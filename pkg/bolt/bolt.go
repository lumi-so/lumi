// Package bolt provides bbolt (BoltDB) embedded KV database support for Lumi.
// It exposes core bbolt operations (open, close, CRUD, transactions, cursors)
// to Lua via go-lua bindings. All operations are synchronous.
package bolt

import (
	"fmt"
	"strings"
	"time"

	bbolt "go.etcd.io/bbolt"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// Config configures the bolt module. Currently a placeholder.
type Config struct{}

// Register registers the "bolt" module in the Lua state.
func Register(L *lua.State, cfg Config) {
	lua.RegisterModule(L, "bolt", map[string]lua.Function{
		"open": lua.WrapSafe(openDB),
	})
}

// ---------------------------------------------------------------------------
// open(path [, mode [, opts]]) → db
// ---------------------------------------------------------------------------

func openDB(L *lua.State) int {
	path := L.CheckString(1)

	// Parse mode: "r", "w", "c" (default "c")
	mode := "c"
	if L.GetTop() >= 2 {
		if s, ok := L.ToString(2); ok && s != "" {
			mode = s
		}
	}

	opts := bbolt.DefaultOptions
	if L.GetTop() >= 3 && L.IsTable(3) {
		opts = parseOpenOpts(L, 3)
	}

	switch mode {
	case "r":
		opts.ReadOnly = true
	case "w":
		opts.ReadOnly = false
	case "c":
		opts.ReadOnly = false
	default:
		L.PushNil()
		L.PushString("invalid mode: " + mode + " (expected r, w, or c)")
		return 2
	}

	db, err := bbolt.Open(path, 0666, opts)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushDB(L, db, path)
	return 1
}

func parseOpenOpts(L *lua.State, idx int) *bbolt.Options {
	opts := bbolt.DefaultOptions

	// timeout
	L.GetField(idx, "timeout")
	if n, ok := L.ToInteger(-1); ok && n > 0 {
		opts.Timeout = time.Duration(n) * time.Millisecond
	}
	L.Pop(1)

	// no_grow_sync
	L.GetField(idx, "no_grow_sync")
	if L.ToBoolean(-1) {
		opts.NoGrowSync = true
	}
	L.Pop(1)

	// no_sync
	L.GetField(idx, "no_sync")
	if L.ToBoolean(-1) {
		opts.NoSync = true
	}
	L.Pop(1)

	// no_freelist_sync
	L.GetField(idx, "no_freelist_sync")
	if L.ToBoolean(-1) {
		opts.NoFreelistSync = true
	}
	L.Pop(1)

	// freelist_type
	L.GetField(idx, "freelist_type")
	if s, ok := L.ToString(-1); ok && s != "" {
		opts.FreelistType = bbolt.FreelistType(s)
	}
	L.Pop(1)

	// mmap_flags
	L.GetField(idx, "mmap_flags")
	if n, ok := L.ToInteger(-1); ok {
		opts.MmapFlags = int(n)
	}
	L.Pop(1)

	// initial_mmap_size
	L.GetField(idx, "initial_mmap_size")
	if n, ok := L.ToInteger(-1); ok {
		opts.InitialMmapSize = int(n)
	}
	L.Pop(1)

	// page_size
	L.GetField(idx, "page_size")
	if n, ok := L.ToInteger(-1); ok {
		opts.PageSize = int(n)
	}
	L.Pop(1)

	return opts
}

// ---------------------------------------------------------------------------
// DB wrapper
// ---------------------------------------------------------------------------

type boltDB struct {
	db   *bbolt.DB
	path string
}

func pushDB(L *lua.State, db *bbolt.DB, path string) {
	d := &boltDB{db: db, path: path}
	L.PushUserdata(d)

	if L.NewMetatable("lumi.bolt.db") {
		methods := map[string]lua.Function{
			"get":                       lua.WrapSafe(dbGet),
			"put":                       lua.WrapSafe(dbPut),
			"delete":                    lua.WrapSafe(dbDelete),
			"list":                      lua.WrapSafe(dbList),
			"list_prefix":              lua.WrapSafe(dbListPrefix),
			"bucket_exists":            lua.WrapSafe(dbBucketExists),
			"begin_read":               lua.WrapSafe(dbBeginRead),
			"begin_write":              lua.WrapSafe(dbBeginWrite),
			"begin_batch":              lua.WrapSafe(dbBeginBatch),
			"close":                    lua.WrapSafe(dbClose),
			"sync":                     lua.WrapSafe(dbSync),
			"path":                     lua.WrapSafe(dbPath),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getDB(L *lua.State) *boltDB {
	return L.UserdataValue(1).(*boltDB)
}

// db:get(bucket, key) → value | nil, err
func dbGet(L *lua.State) int {
	d := getDB(L)
	bucket := L.CheckString(2)
	key := L.CheckString(3)

	var value []byte
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v != nil {
			value = make([]byte, len(v))
			copy(value, v)
		}
		return nil
	})
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	if value == nil {
		L.PushNil()
		return 1
	}
	L.PushString(string(value))
	return 1
}

// db:put(bucket, key, value) → nil, err
func dbPut(L *lua.State) int {
	d := getDB(L)
	bucket := L.CheckString(2)
	key := L.CheckString(3)
	value := L.CheckString(4)

	err := d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}
		return b.Put([]byte(key), []byte(value))
	})
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// db:delete(bucket, key) → nil, err
func dbDelete(L *lua.State) int {
	d := getDB(L)
	bucket := L.CheckString(2)
	key := L.CheckString(3)

	err := d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}
		return b.Delete([]byte(key))
	})
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// db:list(bucket) → {key1, key2, ...} | nil, err
func dbList(L *lua.State) int {
	d := getDB(L)
	bucket := L.CheckString(2)

	var keys []string
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			keys = append(keys, string(k))
		}
		return nil
	})
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	L.CreateTable(len(keys), 0)
	for i, k := range keys {
		L.PushString(k)
		L.RawSetI(-2, int64(i+1))
	}
	return 1
}

// db:list_prefix(bucket, prefix) → {key1, key2, ...} | nil, err
func dbListPrefix(L *lua.State) int {
	d := getDB(L)
	bucket := L.CheckString(2)
	prefix := L.CheckString(3)

	var keys []string
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if strings.HasPrefix(string(k), prefix) {
				keys = append(keys, string(k))
			}
		}
		return nil
	})
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	L.CreateTable(len(keys), 0)
	for i, k := range keys {
		L.PushString(k)
		L.RawSetI(-2, int64(i+1))
	}
	return 1
}

// db:bucket_exists(bucket) → bool
func dbBucketExists(L *lua.State) int {
	d := getDB(L)
	bucket := L.CheckString(2)

	var exists bool
	_ = d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		exists = (b != nil)
		return nil
	})

	L.PushBoolean(exists)
	return 1
}

// db:begin_read() → tx | nil, err
func dbBeginRead(L *lua.State) int {
	d := getDB(L)

	tx, err := d.db.Begin(false)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushTx(L, tx, false, false)
	return 1
}

// db:begin_write() → tx | nil, err
func dbBeginWrite(L *lua.State) int {
	d := getDB(L)

	tx, err := d.db.Begin(true)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushTx(L, tx, true, false)
	return 1
}

// db:begin_batch() → tx | nil, err
func dbBeginBatch(L *lua.State) int {
	d := getDB(L)

	tx, err := d.db.Begin(true)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushTx(L, tx, true, true)
	return 1
}

// db:close()
func dbClose(L *lua.State) int {
	d := getDB(L)
	_ = d.db.Close()
	return 0
}

// db:sync() → nil, err
func dbSync(L *lua.State) int {
	d := getDB(L)
	err := d.db.Sync()
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// db:path() → string
func dbPath(L *lua.State) int {
	d := getDB(L)
	L.PushString(d.path)
	return 1
}

// ---------------------------------------------------------------------------
// Tx wrapper
// ---------------------------------------------------------------------------

type boltTx struct {
	tx       *bbolt.Tx
	writable bool
	batch    bool
	done     bool
}

func pushTx(L *lua.State, tx *bbolt.Tx, writable, batch bool) {
	t := &boltTx{tx: tx, writable: writable, batch: batch}
	L.PushUserdata(t)

	if L.NewMetatable("lumi.bolt.tx") {
		methods := map[string]lua.Function{
			"bucket":                    lua.WrapSafe(txBucket),
			"create_bucket":             lua.WrapSafe(txCreateBucket),
			"create_bucket_if_not_exists": lua.WrapSafe(txCreateBucketIfNotExists),
			"delete_bucket":             lua.WrapSafe(txDeleteBucket),
			"commit":                    lua.WrapSafe(txCommit),
			"close":                     lua.WrapSafe(txClose),
			"writable":                  lua.WrapSafe(txWritable),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getTx(L *lua.State) *boltTx {
	return L.UserdataValue(1).(*boltTx)
}

// tx:bucket(name) → bucket | nil
func txBucket(L *lua.State) int {
	t := getTx(L)
	name := L.CheckString(2)

	b := t.tx.Bucket([]byte(name))
	if b == nil {
		L.PushNil()
		return 1
	}

	pushBucket(L, b, t)
	return 1
}

// tx:create_bucket(name) → bucket | nil, err
func txCreateBucket(L *lua.State) int {
	t := getTx(L)
	name := L.CheckString(2)

	if !t.writable {
		L.PushNil()
		L.PushString("tx is not writable")
		return 2
	}

	b, err := t.tx.CreateBucket([]byte(name))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushBucket(L, b, t)
	return 1
}

// tx:create_bucket_if_not_exists(name) → bucket | nil, err
func txCreateBucketIfNotExists(L *lua.State) int {
	t := getTx(L)
	name := L.CheckString(2)

	if !t.writable {
		L.PushNil()
		L.PushString("tx is not writable")
		return 2
	}

	b, err := t.tx.CreateBucketIfNotExists([]byte(name))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushBucket(L, b, t)
	return 1
}

// tx:delete_bucket(name) → nil, err
func txDeleteBucket(L *lua.State) int {
	t := getTx(L)
	name := L.CheckString(2)

	if !t.writable {
		L.PushNil()
		L.PushString("tx is not writable")
		return 2
	}

	err := t.tx.DeleteBucket([]byte(name))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	L.PushBoolean(true)
	return 1
}

// tx:commit() → nil, err
func txCommit(L *lua.State) int {
	t := getTx(L)

	if t.done {
		L.PushNil()
		L.PushString("tx already closed")
		return 2
	}
	if !t.writable {
		L.PushNil()
		L.PushString("read-only tx cannot commit")
		return 2
	}

	err := t.tx.Commit()
	t.done = true
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// tx:close() — rollback (read tx) or commit+close (batch)
func txClose(L *lua.State) int {
	t := getTx(L)

	if t.done {
		return 0
	}

	if t.batch {
		err := t.tx.Commit()
		t.done = true
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}
		L.PushBoolean(true)
		return 1
	}

	_ = t.tx.Rollback()
	t.done = true
	return 0
}

// tx:writable() → bool
func txWritable(L *lua.State) int {
	t := getTx(L)
	L.PushBoolean(t.writable)
	return 1
}

// ---------------------------------------------------------------------------
// Bucket wrapper
// ---------------------------------------------------------------------------

type boltBucket struct {
	bucket *bbolt.Bucket
	tx     *boltTx
}

func pushBucket(L *lua.State, b *bbolt.Bucket, tx *boltTx) {
	bb := &boltBucket{bucket: b, tx: tx}
	L.PushUserdata(bb)

	if L.NewMetatable("lumi.bolt.bucket") {
		methods := map[string]lua.Function{
			"get":       lua.WrapSafe(bucketGet),
			"put":       lua.WrapSafe(bucketPut),
			"delete":    lua.WrapSafe(bucketDelete),
			"list":      lua.WrapSafe(bucketList),
			"list_prefix": lua.WrapSafe(bucketListPrefix),
			"cursor":    lua.WrapSafe(bucketCursor),
			"for_each":  lua.WrapSafe(bucketForEach),
			"sequence":  lua.WrapSafe(bucketSequence),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getBucket(L *lua.State) *boltBucket {
	return L.UserdataValue(1).(*boltBucket)
}

// bucket:get(key) → value | nil
func bucketGet(L *lua.State) int {
	b := getBucket(L)
	key := L.CheckString(2)

	v := b.bucket.Get([]byte(key))
	if v == nil {
		L.PushNil()
		return 1
	}
	L.PushString(string(v))
	return 1
}

// bucket:put(key, value) → nil, err
func bucketPut(L *lua.State) int {
	b := getBucket(L)
	key := L.CheckString(2)
	value := L.CheckString(3)

	err := b.bucket.Put([]byte(key), []byte(value))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// bucket:delete(key) → nil, err
func bucketDelete(L *lua.State) int {
	b := getBucket(L)
	key := L.CheckString(2)

	err := b.bucket.Delete([]byte(key))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// bucket:list() → {key1, ...}
func bucketList(L *lua.State) int {
	b := getBucket(L)

	c := b.bucket.Cursor()
	var keys []string
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		keys = append(keys, string(k))
	}

	L.CreateTable(len(keys), 0)
	for i, k := range keys {
		L.PushString(k)
		L.RawSetI(-2, int64(i+1))
	}
	return 1
}

// bucket:list_prefix(prefix) → {key1, ...}
func bucketListPrefix(L *lua.State) int {
	b := getBucket(L)
	prefix := L.CheckString(2)

	c := b.bucket.Cursor()
	var keys []string
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		if strings.HasPrefix(string(k), prefix) {
			keys = append(keys, string(k))
		}
	}

	L.CreateTable(len(keys), 0)
	for i, k := range keys {
		L.PushString(k)
		L.RawSetI(-2, int64(i+1))
	}
	return 1
}

// bucket:cursor() → cursor
func bucketCursor(L *lua.State) int {
	b := getBucket(L)
	c := b.bucket.Cursor()

	pushCursor(L, c, b)
	return 1
}

// bucket:for_each(fn) → nil, err
// fn(key, value) is called for each pair. Return true from fn to stop iteration.
func bucketForEach(L *lua.State) int {
	b := getBucket(L)

	if !L.IsFunction(2) {
		L.PushNil()
		L.PushString("expected function as argument")
		return 2
	}

	// Store the function in the registry.
	L.PushValue(2)
	fnRef := L.Ref(lua.RegistryIndex)

	c := b.bucket.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		L.RawGetI(lua.RegistryIndex, int64(fnRef))
		L.PushString(string(k))
		L.PushString(string(v))

		err := L.CallSafe(2, 1)
		if err != nil {
			L.Unref(lua.RegistryIndex, fnRef)
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		stop := L.ToBoolean(-1)
		L.Pop(1)

		if stop {
			break
		}
	}

	L.Unref(lua.RegistryIndex, fnRef)
	L.PushBoolean(true)
	return 1
}

// bucket:sequence() → uint64 | nil, err
func bucketSequence(L *lua.State) int {
	b := getBucket(L)

	seq, err := b.bucket.NextSequence()
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	L.PushInteger(int64(seq))
	return 1
}

// ---------------------------------------------------------------------------
// Cursor wrapper
// ---------------------------------------------------------------------------

type boltCursor struct {
	cursor *bbolt.Cursor
	bucket *boltBucket
}

func pushCursor(L *lua.State, c *bbolt.Cursor, bucket *boltBucket) {
	bc := &boltCursor{cursor: c, bucket: bucket}
	L.PushUserdata(bc)

	if L.NewMetatable("lumi.bolt.cursor") {
		methods := map[string]lua.Function{
			"first":  lua.WrapSafe(cursorFirst),
			"next":   lua.WrapSafe(cursorNext),
			"prev":   lua.WrapSafe(cursorPrev),
			"last":   lua.WrapSafe(cursorLast),
			"seek":   lua.WrapSafe(cursorSeek),
			"key":    lua.WrapSafe(cursorKey),
			"value":  lua.WrapSafe(cursorValue),
			"delete": lua.WrapSafe(cursorDelete),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getCursor(L *lua.State) *boltCursor {
	return L.UserdataValue(1).(*boltCursor)
}

// cursor:first() → key, value | nil, nil
func cursorFirst(L *lua.State) int {
	c := getCursor(L)
	k, v := c.cursor.First()
	if k == nil {
		L.PushNil()
		L.PushNil()
		return 2
	}
	L.PushString(string(k))
	L.PushString(string(v))
	return 2
}

// cursor:next() → key, value | nil, nil
func cursorNext(L *lua.State) int {
	c := getCursor(L)
	k, v := c.cursor.Next()
	if k == nil {
		L.PushNil()
		L.PushNil()
		return 2
	}
	L.PushString(string(k))
	L.PushString(string(v))
	return 2
}

// cursor:prev() → key, value | nil, nil
func cursorPrev(L *lua.State) int {
	c := getCursor(L)
	k, v := c.cursor.Prev()
	if k == nil {
		L.PushNil()
		L.PushNil()
		return 2
	}
	L.PushString(string(k))
	L.PushString(string(v))
	return 2
}

// cursor:last() → key, value | nil, nil
func cursorLast(L *lua.State) int {
	c := getCursor(L)
	k, v := c.cursor.Last()
	if k == nil {
		L.PushNil()
		L.PushNil()
		return 2
	}
	L.PushString(string(k))
	L.PushString(string(v))
	return 2
}

// cursor:seek(prefix) → key, value | nil, nil
func cursorSeek(L *lua.State) int {
	c := getCursor(L)
	seek := L.CheckString(2)
	k, v := c.cursor.Seek([]byte(seek))
	if k == nil {
		L.PushNil()
		L.PushNil()
		return 2
	}
	L.PushString(string(k))
	L.PushString(string(v))
	return 2
}

// cursor:key() → key | nil
func cursorKey(L *lua.State) int {
	c := getCursor(L)
	k, _ := c.cursor.First()
	if k == nil {
		L.PushNil()
		return 1
	}
	L.PushString(string(k))
	return 1
}

// cursor:value() → value | nil
func cursorValue(L *lua.State) int {
	c := getCursor(L)
	_, v := c.cursor.First()
	if v == nil {
		L.PushNil()
		return 1
	}
	L.PushString(string(v))
	return 1
}

// cursor:delete() → nil, err
func cursorDelete(L *lua.State) int {
	c := getCursor(L)

	err := c.cursor.Delete()
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}
