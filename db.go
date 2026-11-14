package main

// Database layer: one open dbcore connection plus the metadata caches the
// schema outline reads from. Everything that talks to a server is here; the
// UI never sees a driver type.

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dbcore/dbcore"
	_ "github.com/dbcore/dbcore/mssql"    // registers the SQL Server factory
	_ "github.com/dbcore/dbcore/mysql"    // registers the MySQL factory
	_ "github.com/dbcore/dbcore/oracle"   // registers the Oracle factory
	_ "github.com/dbcore/dbcore/postgres" // registers the PostgreSQL factory
	_ "github.com/dbcore/dbcore/redis"    // registers the Redis factory
	_ "github.com/dbcore/dbcore/sqlite"   // registers the SQLite factory

	"dbstudio-go/internal/tunnel"
)

// DB is one live connection.
type DB struct {
	Driver dbcore.Driver
	SQL    dbcore.SQLDriver
	KV     dbcore.KVDriver
	Kind   dbcore.DatabaseKind

	// Tx is the transaction registry of this connection. It is nil when the
	// driver does not implement SQLTxDriver (Redis), which is what lets the
	// toolbar disable the autocommit switch instead of showing it as active.
	Tx *dbcore.TxManager

	// Tunnel is the SSH bastion this connection goes through, when it has one.
	// It is closed with the connection: a tunnel left open would keep the
	// bastion's channel and the client's process alive for nothing.
	Tunnel *tunnel.Tunnel
}

// OpenDB opens a connection from a saved connection. It is safe to call from
// any goroutine; the caller closes it through Close.
//
// A connection with an SSH bastion is opened through a tunnel: the tunnel's
// dialer is handed to the kernel, which routes every pooled connection through
// it (MySQL and PostgreSQL have their own dialer hooks). Engines that refuse a
// custom dialer say so, and the connect fails with that reason rather than
// quietly connecting to the wrong host.
func OpenDB(ctx context.Context, c Conn) (*DB, error) {
	normalize(&c)
	if c.Kind == "sqlite" && c.File == "" {
		c.File = ":memory:"
	}
	var tun *tunnel.Tunnel
	if c.SSHHost != "" {
		t, err := tunnel.Open(ctx, tunnel.Config{
			Host:          c.SSHHost,
			Port:          c.SSHPort,
			User:          c.SSHUser,
			Password:      c.SSHPassword,
			KeyFile:       c.SSHKeyFile,
			KeyPassphrase: c.SSHPassphrase,
			// An unknown host is refused here, not trusted: a connect that
			// silently trusts a stranger's key is not a tunnel. The UI offers
			// the fingerprint so the user can vouch for it deliberately.
			UnknownHost: nil,
		})
		if err != nil {
			return nil, err
		}
		tun = t
	}
	spec := dbcore.ConnSpec{
		Name:           c.Name,
		Kind:           dbcore.DatabaseKind(c.Kind),
		Host:           c.Host,
		Port:           c.Port,
		User:           c.User,
		Password:       c.Password,
		Database:       c.Database,
		Schema:         c.Schema,
		File:           c.File,
		SSLMode:        c.SSLMode,
		ConnectTimeout: 15 * time.Second,
	}
	if tun != nil {
		spec.Dialer = tun.Dialer()
	}
	d, err := dbcore.Open(ctx, spec, dbcore.PoolConfig{MaxOpen: 8, MaxIdle: 4})
	if err != nil {
		if tun != nil {
			_ = tun.Close()
		}
		return nil, err
	}
	db := &DB{Driver: d, Kind: d.Kind(), Tunnel: tun}
	if sql, ok := d.AsSQL(); ok {
		db.SQL = sql
	}
	if kv, ok := d.AsKV(); ok {
		db.KV = kv
	}
	// The transaction registry, built from the driver itself (not through the
	// pool) so it can hand the same *sql.Tx to ExecuteIn. Redis has no SQL and
	// no SQLTxDriver, so db.Tx stays nil and the UI hides the switch.
	if txd, ok := d.(dbcore.SQLTxDriver); ok {
		db.Tx = dbcore.NewTxManager(txd)
	}
	return db, nil
}

// Close releases the connection. Any open transaction is rolled back first:
// a transaction left open on a connection that is going away would hold the
// server's locks until it timed out.
func (db *DB) Close() error {
	if db == nil || db.Driver == nil {
		return nil
	}
	if db.Tx != nil {
		db.Tx.RollbackAll()
		db.Tx.Close()
		db.Tx = nil
	}
	err := db.Driver.Close()
	if db.Tunnel != nil {
		// The tunnel goes after the driver: closing it first would make the
		// pool's own close fail on dead channels.
		_ = db.Tunnel.Close()
		db.Tunnel = nil
	}
	return err
}

// Meta is the schema outline's data, cached per connection.
type Meta struct {
	schemas []dbcore.SchemaInfo
	tables  map[string][]dbcore.TableInfo  // schema -> tables
	columns map[string][]dbcore.ColumnInfo // schema.table -> columns
}

func newMeta() *Meta {
	return &Meta{tables: map[string][]dbcore.TableInfo{}, columns: map[string][]dbcore.ColumnInfo{}}
}

// SchemaNames lists the namespaces the tree shows at its top.
func (c *Cached) SchemaNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.meta.schemas))
	for i, s := range c.meta.schemas {
		out[i] = s.Name
	}
	return out
}

// TableNames lists the tables of a schema, in the order the driver gave them.
func (c *Cached) TableNames(schema string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	tabs := c.meta.tables[schema]
	out := make([]string, len(tabs))
	for i, t := range tabs {
		out[i] = t.Name
	}
	return out
}

// TableComment returns the comment the driver gave a table, if any.
func (c *Cached) TableComment(schema, table string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.meta.tables[schema] {
		if t.Name == table {
			return t.Comment
		}
	}
	return ""
}

// tableKey identifies a table for the columns cache.
func tableKey(schema, table string) string { return schema + "\x00" + table }

// Cached is the app's cached metadata for one connection.
type Cached struct {
	mu     sync.Mutex
	db     *DB
	meta   *Meta
	schema string
	table  string
}

func newCached(db *DB) *Cached {
	return &Cached{db: db, meta: newMeta()}
}

// setSchema loads and keeps the tables of one schema. It runs off the UI
// thread; the results reach the UI through the store.
func (c *Cached) setSchema(ctx context.Context, schema string) error {
	if c.db == nil || c.db.SQL == nil {
		return fmt.Errorf("not connected")
	}
	if schema == "" {
		schemas, err := c.db.SQL.ListSchemas(ctx)
		if err != nil {
			return err
		}
		if len(schemas) == 0 {
			return fmt.Errorf("no schemas")
		}
		schema = schemas[0].Name
	}
	tables, err := c.db.SQL.ListTables(ctx, schema)
	if err != nil {
		return err
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	c.mu.Lock()
	c.schema = schema
	c.meta.tables[schema] = tables
	c.mu.Unlock()
	return nil
}

// setTables loads the tables of a schema (used when a schema opens).
func (c *Cached) setTables(ctx context.Context, schema string) error {
	if c.db == nil || c.db.SQL == nil {
		return fmt.Errorf("not connected")
	}
	tables, err := c.db.SQL.ListTables(ctx, schema)
	if err != nil {
		return err
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	c.mu.Lock()
	c.meta.tables[schema] = tables
	c.mu.Unlock()
	return nil
}

// setColumns loads a table's columns.
func (c *Cached) setColumns(ctx context.Context, schema, table string) error {
	if c.db == nil || c.db.SQL == nil {
		return fmt.Errorf("not connected")
	}
	cols, err := c.db.SQL.ListColumns(ctx, schema, table)
	if err != nil {
		return err
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].OrdinalPosition < cols[j].OrdinalPosition })
	c.mu.Lock()
	c.meta.columns[tableKey(schema, table)] = cols
	c.mu.Unlock()
	return nil
}

// --- reads used by the UI --------------------------------------------------

func (c *Cached) schemaList() []dbcore.SchemaInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]dbcore.SchemaInfo(nil), c.meta.schemas...)
}

func (c *Cached) currentSchema() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.schema
}

func (c *Cached) currentTable() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.table
}

func (c *Cached) hasTables(schema string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.meta.tables[schema]
	return ok
}

func (c *Cached) hasColumns(schema, table string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.meta.columns[tableKey(schema, table)]
	return ok
}

func (c *Cached) tablesOf(schema string) []dbcore.TableInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]dbcore.TableInfo(nil), c.meta.tables[schema]...)
}

func (c *Cached) columnsOf(schema, table string) []dbcore.ColumnInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]dbcore.ColumnInfo(nil), c.meta.columns[tableKey(schema, table)]...)
}

// LoadSchemas fills the schema list from the server.
func (c *Cached) LoadSchemas(ctx context.Context) error {
	if c.db == nil || c.db.SQL == nil {
		return fmt.Errorf("not connected")
	}
	schemas, err := c.db.SQL.ListSchemas(ctx)
	if err != nil {
		return err
	}
	sort.Slice(schemas, func(i, j int) bool { return schemas[i].Name < schemas[j].Name })
	c.mu.Lock()
	c.meta.schemas = schemas
	c.mu.Unlock()
	return nil
}

// IsKV reports whether the connection is a key-value store rather than SQL.
func (c *Cached) IsKV() bool {
	if c == nil || c.db == nil {
		return false
	}
	return c.db.KV != nil
}
