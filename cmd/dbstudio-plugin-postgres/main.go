// dbstudio-plugin-postgres: PostgreSQL support for DB Studio, delivered as a
// plugin executable (波 100's driver story: the binary ships with MySQL and
// SQLite built in; every other engine arrives as one of these).
//
// The whole plugin is a Handler over database/sql plus pluginsdk.Main — the
// SDK owns the wire, the banner, the spec on argv, and the lifecycle; this
// file owns only PostgreSQL's dialect.
//
//	go build -o dbstudio-plugin-postgres.exe ./cmd/dbstudio-plugin-postgres
package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/postgres"

	"dbstudio-go/internal/pluginsdk"
)


// pgHandler is one connection: the plugin process IS the connection, so the
// handle is opened in Open from argv's spec and lives for the process.
type pgHandler struct {
	spec dbcore.ConnSpec
	drv  *postgres.PostgresDriver
}

func (h *pgHandler) Kind() string { return "postgres" }
func (h *pgHandler) Name() string { return "PostgreSQL" }

// Open dials PostgreSQL through the kernel's own driver: the plugin is a thin
// process shell over the same code a built-in would run.
func (h *pgHandler) Open(spec dbcore.ConnSpec) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	d, err := postgres.Open(dsn(spec))
	if err != nil {
		return err
	}
	if err := d.Ping(ctx); err != nil {
		_ = d.Close()
		return err
	}
	h.spec = spec
	h.drv = d
	return nil
}

// dsn assembles the postgres:// URL from the spec, defaulting the port and
// letting the SSL mode through as the sslmode parameter.
func dsn(spec dbcore.ConnSpec) string {
	port := "5432"
	if spec.Port > 0 {
		port = fmt.Sprint(spec.Port)
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(spec.User, spec.Password),
		Host:   net.JoinHostPort(spec.Host, port),
		Path:   "/" + spec.Database,
	}
	q := url.Values{}
	if spec.SSLMode != "" {
		q.Set("sslmode", spec.SSLMode)
	} else {
		q.Set("sslmode", "prefer")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (h *pgHandler) Close() {
	if h.drv != nil {
		_ = h.drv.Close()
	}
}

func (h *pgHandler) Ping(ctx context.Context) error { return h.drv.Ping(ctx) }

func (h *pgHandler) ListSchemas(ctx context.Context) ([]dbcore.SchemaInfo, error) {
	return h.drv.ListSchemas(ctx)
}

func (h *pgHandler) ListTables(ctx context.Context, schema string) ([]dbcore.TableInfo, error) {
	return h.drv.ListTables(ctx, schema)
}

func (h *pgHandler) ListColumns(ctx context.Context, schema, table string) ([]dbcore.ColumnInfo, error) {
	return h.drv.ListColumns(ctx, schema, table)
}

func (h *pgHandler) Execute(ctx context.Context, query string, args []dbcore.Value) (*dbcore.QueryResult, error) {
	// The plugin process is one connection, so the statement runs directly on
	// the kernel driver; bound values ride as driver arguments, never as
	// concatenated text.
	return h.drv.Execute(ctx, query, args...)
}

func (h *pgHandler) CreateTableSQL(ctx context.Context, schema, table string) (string, error) {
	return h.drv.CreateTableSQL(ctx, schema, table)
}

func (h *pgHandler) BrowseRows(ctx context.Context, schema, table string, req *dbcore.TableRowsRequest) (*dbcore.TableRowsResult, error) {
	return h.drv.BrowseRows(ctx, schema, table, req)
}

func (h *pgHandler) BrowseRowsCount(ctx context.Context, schema, table string, req *dbcore.TableRowsRequest) (uint64, error) {
	return h.drv.BrowseRowsCount(ctx, schema, table, req)
}

func main() {
	pluginsdk.Main(&pgHandler{})
}
