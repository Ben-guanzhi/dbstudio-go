package main

// The plugin host (波 100): drivers beyond the built-in MySQL arrive as
// executables beside the app, named `dbstudio-plugin-<kind>[.exe]`. Each is
// discovered at startup and registered into dbcore's factory table lazily —
// nothing is spawned until a connection of that kind is opened, and the
// process it starts IS the connection (the kernel's one-process-one-connection
// arrangement; the spec rides argv[1] as JSON).
//
// The discovery contract is the executable's name, which is why the form's
// Kind list can offer every engine at startup without launching anything:
// `dbstudio-plugin-postgres.exe` means the postgres kind is available.
// Duplicate names resolve first-wins, matching the dedup everywhere else.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/plugindriver"
)

// pluginPrefix names a DB Studio driver plugin. The rest of the file name is
// the dbcore kind it serves.
const pluginPrefix = "dbstudio-plugin-"

// pluginExts are the executable extensions a discovery scan accepts, so a
// Linux-built plugin dropped into a Windows directory is skipped rather than
// launched into a failed handshake.
var pluginExts = map[string]bool{".exe": true}

// DiscoverPlugins registers every plugin executable in dir into dbcore's
// factory table. A missing directory is silent: the built-in MySQL keeps
// working regardless, and a fresh install has no plugins yet.
//
// The factory is lazy on the inside: dbcore.Open calls it per connection, and
// the factory starts the plugin process with the spec on argv — the process
// dies with the driver's Close, which the pool already drives.
func DiscoverPlugins(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !pluginExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !strings.HasPrefix(base, pluginPrefix) {
			continue
		}
		kind := strings.TrimPrefix(base, pluginPrefix)
		if kind == "" || seen[kind] {
			continue
		}
		kind = strings.ToLower(kind)
		exe := filepath.Join(dir, e.Name())
		if dbcore.HasFactory(dbcore.DatabaseKind(kind)) {
			continue // a built-in driver wins; a plugin never shadows it
		}
		seen[kind] = true
		dbcore.RegisterFactory(dbcore.DatabaseKind(kind), pluginFactory(exe))
	}
}

// pluginFactory returns the dbcore.Factory that serves kind from one plugin
// executable: each connection starts its own process with the spec on argv[1],
// the banner verifies the kind before any request, and the returned Driver is
// the kernel's own SQLDriver-backed implementation — OpenDB's pool and every
// grid path see no difference from a built-in.
func pluginFactory(exe string) dbcore.Factory {
	return func(ctx context.Context, spec dbcore.ConnSpec, _ dbcore.PoolConfig) (dbcore.Driver, error) {
		specJSON, err := jsonMarshalSpec(spec)
		if err != nil {
			return nil, err
		}
		d, err := plugindriver.StartWithOpts(ctx, exe, []string{specJSON}, plugindriver.StartOpts{
			HideWindow: true,
		})
		if err != nil {
			return nil, err
		}
		// The banner's kind is the plugin's own claim; a mismatched executable
		// (renamed wrongly) is refused here rather than answering calls into
		// the wrong engine.
		if want := spec.Kind; d.Kind() != want {
			_ = d.Close()
			return nil, fmt.Errorf("plugin %s serves %q, not %q", filepath.Base(exe), d.Kind(), want)
		}
		return d, nil
	}
}

// PluginKinds lists the engine kinds the plugins beside the app serve, sorted.
// The connection form's Kind list is the built-ins plus these.
func PluginKinds() []string {
	dir, err := os.Executable()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !strings.HasPrefix(base, pluginPrefix) {
			continue
		}
		kind := strings.ToLower(strings.TrimPrefix(base, pluginPrefix))
		if kind != "" && !dbcore.HasFactory(dbcore.DatabaseKind(kind)) {
			out = append(out, kind)
		}
	}
	sort.Strings(out)
	return out
}

// jsonMarshalSpec encodes the spec the plugin's argv[1] carries. Field names
// follow dbcore.ConnSpec's JSON tags, so the plugin side decodes the same
// struct without a second wire type.
func jsonMarshalSpec(spec dbcore.ConnSpec) (string, error) {
	b, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encode connection spec: %w", err)
	}
	return string(b), nil
}
