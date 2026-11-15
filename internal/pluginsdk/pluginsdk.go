// Package pluginsdk is the plugin-side SDK for DB Studio's database plugins:
// implement Handler, call Main, and the executable is a driver the app
// discovers beside itself.
//
// The wire is dbcore/plugindriver's — the same protocol the kernel host
// speaks — so this package is a thin serve loop over the kernel's own message
// types (Request/Response/Banner, MethodExecute/...): no custom vocabulary.
// The process IS one connection (one process per connection is the kernel's
// arrangement: it keeps a crash contained and every call free of connection
// ids); the host hands the connection's spec to the plugin as argv[1] as a
// JSON-encoded dbcore.ConnSpec. The plugin connects at startup, before the
// banner, and answers catalog/query calls over the handle it holds.
//
//	go run ./cmd/dbstudio-plugin-postgres   # plugins are plain executables
package pluginsdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/plugindriver"
)

// Handler is the plugin side of the protocol: the SQL surface dbstudio's grid
// needs. A method returns a JSON-marshalable result or an error; Serve encodes
// either into a Response. Unimplemented surfaces return an error — the host
// degrades to what the plugin does have.
type Handler interface {
	// Kind is the engine this plugin serves, as dbcore names it
	// ("postgres", "oracle", ...). It names the banner.
	Kind() string
	// Name is the display name ("PostgreSQL"), shown in diagnostics.
	Name() string
	// Open connects using the spec the host passed as argv[1]. Called once,
	// before the banner: a plugin that cannot connect fails at launch with a
	// real message instead of answering calls into the void.
	Open(spec dbcore.ConnSpec) error
	// Close releases the connection; the host closes stdin right after, so
	// returning is enough.
	Close()
	// Ping answers the host's liveness probe.
	Ping(ctx context.Context) error
	// ListSchemas lists databases (mysql) or schemas (postgres). Engines
	// without the level return one empty-string entry.
	ListSchemas(ctx context.Context) ([]dbcore.SchemaInfo, error)
	// ListTables lists the tables and views of one schema.
	ListTables(ctx context.Context, schema string) ([]dbcore.TableInfo, error)
	// ListColumns lists the columns of one table.
	ListColumns(ctx context.Context, schema, table string) ([]dbcore.ColumnInfo, error)
	// Execute runs a statement: a row set, or rows affected.
	Execute(ctx context.Context, query string, args []dbcore.Value) (*dbcore.QueryResult, error)
	// CreateTableSQL returns the CREATE statement, when the engine keeps one.
	CreateTableSQL(ctx context.Context, schema, table string) (string, error)
	// BrowseRows pages a table's rows; rendered by the plugin from the parts.
	BrowseRows(ctx context.Context, schema, table string, req *dbcore.TableRowsRequest) (*dbcore.TableRowsResult, error)
	// BrowseRowsCount answers a table's row count for the pager.
	BrowseRowsCount(ctx context.Context, schema, table string, req *dbcore.TableRowsRequest) (uint64, error)
}

// Main is a plugin's whole main: connect from argv[1]'s spec, banner, serve
// until stdin closes, then exit quietly.
func Main(h Handler) {
	spec, err := specFromArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", h.Name(), err)
		os.Exit(2)
	}
	if err := h.Open(spec); err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect: %v\n", h.Name(), err)
		os.Exit(3)
	}
	banner := plugindriver.Banner{
		Proto: plugindriver.ProtocolName,
		// A plugin built for the kernel protocol is version-locked to it: the
		// host refuses a binary whose protocol is newer than it speaks.
		Version: plugindriver.ProtocolVersion,
		Kind:    h.Kind(),
		Name:    h.Name(),
	}
	if err := Serve(h, banner, os.Stdin, os.Stdout); err != nil {
		log.Fatalf("%s: serve: %v", h.Name(), err)
	}
	os.Exit(0)
}

// specFromArgs decodes argv[1]'s dbcore.ConnSpec. The host always passes it;
// a bare launch is a developer running the executable by hand, and the message
// says so instead of failing deep in the protocol.
func specFromArgs() (dbcore.ConnSpec, error) {
	var spec dbcore.ConnSpec
	if len(os.Args) < 2 {
		return spec, fmt.Errorf("missing connection spec: launch me through DB Studio (argv[1] carries the JSON spec)")
	}
	if err := json.Unmarshal([]byte(os.Args[1]), &spec); err != nil {
		return spec, fmt.Errorf("decode connection spec: %w", err)
	}
	return spec, nil
}

// Serve runs the request loop until r closes. Each request is served in its
// own goroutine (bounded), which is what lets a cancel reach the plugin while
// a query is still running.
func Serve(h Handler, banner plugindriver.Banner, r io.Reader, w io.Writer) error {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if err := writeLine(w, banner); err != nil {
		return fmt.Errorf("write banner: %w", err)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // SQL can be large
	// The bound is the plugin's own: the reader blocks instead of spawning, so
	// memory and connection pressure stay flat no matter who drives it.
	inflight := make(chan struct{}, 8)
	var wmu sync.Mutex
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req plugindriver.Request
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("bad request line: %v", err)
			continue
		}
		inflight <- struct{}{}
		go func(req plugindriver.Request) {
			defer func() { <-inflight }()
			res := plugindriver.Response{ID: req.ID}
			result, err := dispatch(h, req)
			if err != nil {
				res.Error = err.Error()
			} else {
				b, jerr := json.Marshal(result)
				if jerr != nil {
					res.Error = fmt.Sprintf("marshal result: %v", jerr)
				} else {
					res.Result = b
				}
			}
			frame, _ := json.Marshal(res)
			wmu.Lock()
			fmt.Fprintf(w, "%s\n", frame)
			wmu.Unlock()
		}(req)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read requests: %w", err)
	}
	h.Close()
	return nil
}

// dispatch routes one request to its handler method, translating the kernel's
// payload types so a plugin never touches the wire.
func dispatch(h Handler, req plugindriver.Request) (interface{}, error) {
	ctx := context.Background()
	switch req.Method {
	case plugindriver.MethodPing:
		if err := h.Ping(ctx); err != nil {
			return nil, err
		}
		return pingOK{}, nil
	case plugindriver.MethodClose:
		h.Close()
		return nil, nil
	case plugindriver.MethodListSchemas:
		return h.ListSchemas(ctx)
	case plugindriver.MethodListTables:
		var a plugindriver.SchemaArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		return h.ListTables(ctx, a.Schema)
	case plugindriver.MethodListColumns:
		var a plugindriver.SchemaArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		return h.ListColumns(ctx, a.Schema, a.Table)
	case plugindriver.MethodExecute:
		var a execArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		values := make([]dbcore.Value, len(a.Args))
		for i, c := range a.Args {
			v, err := plugindriver.DecodeValue(c)
			if err != nil {
				return nil, err
			}
			values[i] = v
		}
		return h.Execute(ctx, a.SQL, values)
	case plugindriver.MethodCreateTableDB:
		var a plugindriver.SchemaArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		return h.CreateTableSQL(ctx, a.Schema, a.Table)
	case plugindriver.MethodBrowseRows:
		var a browseArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		req2, err := a.request()
		if err != nil {
			return nil, err
		}
		return h.BrowseRows(ctx, a.Schema, a.Table, req2)
	case plugindriver.MethodBrowseCount:
		var a browseArgs
		if err := json.Unmarshal(req.Params, &a); err != nil {
			return nil, err
		}
		req2, err := a.request()
		if err != nil {
			return nil, err
		}
		n, err := h.BrowseRowsCount(ctx, a.Schema, a.Table, req2)
		if err != nil {
			return nil, err
		}
		return browseCount{Total: n}, nil
	}
	return nil, fmt.Errorf("unknown method %q", req.Method)
}

// execArgs is MethodExecute's payload (the host sends sql plus tagged cells).
type execArgs struct {
	SQL  string                     `json:"sql"`
	Args []plugindriver.CellPayload `json:"args,omitempty"`
}

// browseArgs is MethodBrowseRows/Count's payload; it re-exports the kernel's
// BrowseArgs with the parts decoded into the dbcore request the handler takes.
type browseArgs struct {
	Schema   string                     `json:"schema,omitempty"`
	Table    string                     `json:"table"`
	Page     int                        `json:"page,omitempty"`
	PageSize int                        `json:"pageSize,omitempty"`
	Sort     []plugindriver.SortArg     `json:"sort,omitempty"`
	Filter   []plugindriver.FilterArg   `json:"filter,omitempty"`
	After    []plugindriver.CellPayload `json:"after,omitempty"`
}

type browseCount struct {
	Total uint64 `json:"total"`
}

type pingOK struct{}

// request reassembles the dbcore request the handler's BrowseRows takes.
func (a browseArgs) request() (*dbcore.TableRowsRequest, error) {
	req := &dbcore.TableRowsRequest{
		Page:     a.Page,
		PageSize: a.PageSize,
	}
	for _, s := range a.Sort {
		req.Sort = append(req.Sort, dbcore.SortKey{Column: s.Column, Desc: s.Desc})
	}
	for _, f := range a.Filter {
		v, err := plugindriver.DecodeValue(f.Value)
		if err != nil {
			return nil, err
		}
		req.Filter = append(req.Filter, dbcore.FilterExpr{Column: f.Column, Op: dbcore.FilterOp(f.Op), Value: v})
	}
	if len(a.After) > 0 {
		after := make([]dbcore.Value, len(a.After))
		for i, c := range a.After {
			v, err := plugindriver.DecodeValue(c)
			if err != nil {
				return nil, err
			}
			after[i] = v
		}
		req.After = after
	}
	return req, nil
}

func writeLine(w io.Writer, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
