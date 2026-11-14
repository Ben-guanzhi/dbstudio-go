package main

// The plugin host (波 100): drivers beyond the built-in MySQL arrive as
// executables beside the app, named `dbstudio-plugin-<kind>[.exe]`. Each is
// discovered at startup and registered into dbcore's factory table lazily —
// nothing is spawned until a connection of that kind is opened, and the
// process it starts IS the connection (the kernel's one-process-one-connection
// arrangement; the spec rides argv[1] as JSON).
//
// 波 100 续（时空可组合性）：发现与撤回对称——UndiscoverPlugins 把注册表带
// 回「此前没有该插件」的状态（注销 kind、不杀进程：每个进程是一条连接，归
// 连接池管，这里没有进程句柄可杀）。撤回受内核的 reliance 守卫保护：
// FactoryReliance > 0（有连接正在打开）时 UnregisterFactory 拒绝，调用方得
// 到错误而不是一个被抽走的工厂——dbcore 文档里明写的 relied_n(γ) 类比。
// 二次发现同 kind 跳过（HasFactory 前置），内置驱动永不遮蔽，热重扫幂等。

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

// discovered remembers which kinds this process registered from which file,
// so UndiscoverPlugins withdraws exactly what a previous scan installed — the
// accumulator discipline: one discover's inverse withdraws its own contribution.
var discovered = map[string]pluginEntry{}

type pluginEntry struct {
	exe  string
	path string
}

// DiscoverPlugins registers every plugin executable in dir into dbcore's
// factory table. A missing directory is silent: the built-in MySQL keeps
// working regardless, and a fresh install has no plugins yet. Re-scanning is
// idempotent — a kind already registered (built-in or previously discovered)
// is skipped, never double-registered.
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
		kind := strings.ToLower(strings.TrimPrefix(base, pluginPrefix))
		if kind == "" || seen[kind] {
			continue
		}
		kind = strings.ToLower(kind)
		exe := filepath.Join(dir, e.Name())
		if dbcore.HasFactory(dbcore.DatabaseKind(kind)) {
			continue // a built-in driver or an earlier scan wins; never shadow
		}
		seen[kind] = true
		dbcore.RegisterFactory(dbcore.DatabaseKind(kind), pluginFactory(exe))
		discovered[kind] = pluginEntry{exe: exe, path: dir}
	}
}

// UndiscoverPlugins withdraws every plugin this process registered from dir
// (every discovered plugin when dir is empty), returning the registry to the
// state before that scan. Withdrawal is refused per kind while a connection of
// that kind is still opening (dbcore's reliance guard); the return value is
// how many kinds were actually withdrawn, and failed kinds are reported so a
// caller can surface them instead of losing the attempt silently.
func UndiscoverPlugins(dir string) (withdrawn int, busy []string) {
	for kind, entry := range discovered {
		if dir != "" && entry.path != dir {
			continue
		}
		if err := dbcore.UnregisterFactory(dbcore.DatabaseKind(kind)); err != nil {
			busy = append(busy, fmt.Sprintf("%s: %v", kind, err))
			continue
		}
		delete(discovered, kind)
		withdrawn++
	}
	sort.Strings(busy)
	return withdrawn, busy
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
// The connection form's Kind list is the built-ins plus these. It reads the
// live discovery state (not the directory), so a rescan that withdrew a plugin
// is reflected without a restart.
func PluginKinds() []string {
	out := make([]string, 0, len(discovered))
	for kind := range discovered {
		out = append(out, kind)
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
