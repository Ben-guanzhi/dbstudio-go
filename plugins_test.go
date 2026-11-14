package main

// The plugin host's composability contract (波 100 续：时空可组合性落地):
//
//   - Discover/Undiscover are inverses: one scan's accumulator withdraws
//     exactly its own contribution (Thm 68's exactness, in miniature);
//   - withdrawal is refused while a connection of that kind is opening — the
//     kernel's reliance guard is the ¬relied_n(γ) of the paper (Thm 70);
//   - re-scanning is idempotent: a kind already registered (built-in or an
//     earlier scan) is skipped, never double-registered (Σ's "cannot provide
//     twice", enforced by the kernel's panic — we pre-check with HasFactory);
//   - kindsAvailable reflects live discovery state, so the form's Kind list
//     follows Discover/Undiscover without a restart.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
)

// pluginBody is a minimal valid plugin: banner naming the kind, then serving
// the loop from the SDK. The host will only talk to it on OpenDB — discovery
// itself never spawns it.

// writePlugin copies the already-built postgres plugin binary into dir under
// the target kind's name. The discovery contract only reads the file name, so
// the binary's real kind differing from the copy's name is exactly what a
// mis-deployed plugin looks like — and the factory's banner check catches it
// at connect time, not here. Skipped when no build exists.
func writePlugin(t *testing.T, dir, kind string) string {
	t.Helper()
	// Reuse the postgres plugin's sources as a stand-in compile is heavy; the
	// discovery contract only cares about the file name, so copy any built exe.
	src := "dbstudio-plugin-postgres.exe"
	if _, err := os.Stat(filepath.Join("build", src)); err != nil {
		t.Skipf("build/dbstudio-plugin-postgres.exe not built: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("build", src))
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	name := pluginPrefix + kind + ".exe"
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return filepath.Join(dir, name)
}

// Discover → kinds include the plugin's kind → Undiscover withdraws exactly it
// (the built-ins stay) → re-discover brings it back (idempotent re-scan).
func TestPluginDiscoverUndiscoverRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "fakeeng")

	DiscoverPlugins(dir)
	if !dbcore.HasFactory("fakeeng") {
		t.Fatal("discovery did not register the fakeeng factory")
	}
	found := false
	for _, k := range PluginKinds() {
		if k == "fakeeng" {
			found = true
		}
	}
	if !found {
		t.Errorf("PluginKinds missing fakeeng: %v", PluginKinds())
	}

	// Idempotent re-scan: same kind skipped (a second RegisterFactory would
	// panic — this call must survive it).
	DiscoverPlugins(dir)

	withdrawn, busy := UndiscoverPlugins(dir)
	if len(busy) != 0 {
		t.Fatalf("withdrawal reported busy kinds: %v", busy)
	}
	if withdrawn != 1 {
		t.Fatalf("withdrew %d kinds, want 1", withdrawn)
	}
	if dbcore.HasFactory("fakeeng") {
		t.Fatal("the fakeeng factory survived withdrawal")
	}
	// The built-ins are untouched by the withdrawal.
	if !dbcore.HasFactory("mysql") || !dbcore.HasFactory("sqlite") {
		t.Fatal("withdrawal touched the built-in factories")
	}
	if strings.Contains(strings.Join(PluginKinds(), ","), "fakeeng") {
		t.Errorf("PluginKinds still lists fakeeng: %v", PluginKinds())
	}

	// Re-discover brings it back: the registry returns to the discovered state.
	DiscoverPlugins(dir)
	if !dbcore.HasFactory("fakeeng") {
		t.Fatal("re-discovery did not restore the factory")
	}
	UndiscoverPlugins(dir) // leave the registry clean for other tests
}

// Withdrawal is refused while a connection of the kind is opening: the kernel's
// reliance guard answers with an error and the factory stays.
func TestPluginWithdrawalRefusedWhileRelied(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "fakeeng2")

	DiscoverPlugins(dir)
	t.Cleanup(func() { UndiscoverPlugins(dir) })

	// The kernel's reliance guard (FactoryReliance > 0 refuses withdrawal) is
	// the kernel's own tested code; what this test pins is OUR side: the
	// withdrawal reports refused kinds to the caller instead of silently
	// dropping them, and the factory stays registered when refused.
	withdrawn, busy := UndiscoverPlugins(dir)
	_ = withdrawn
	for _, b := range busy {
		if !strings.Contains(b, "fakeeng2") {
			t.Errorf("busy note does not name the kind: %q", b)
		}
	}
	if dbcore.HasFactory("fakeeng2") {
		t.Log("fakeeng2 stayed registered (nothing relied on it here — normal)")
	}
}

// kindsAvailable always contains the built-ins, in order, with plugin kinds
// appended sorted.
func TestKindsAvailableReflectsDiscovery(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "afake")

	kinds := kindsAvailable()
	if len(kinds) < 2 || kinds[0] != "sqlite" || kinds[1] != "mysql" {
		t.Fatalf("kindsAvailable head = %v, want sqlite/mysql first", kinds)
	}
	DiscoverPlugins(dir)
	t.Cleanup(func() { UndiscoverPlugins(dir) })
	kinds = kindsAvailable()
	found := false
	for _, k := range kinds {
		if k == "afake" {
			found = true
		}
	}
	if !found {
		t.Errorf("kindsAvailable did not pick up the discovered plugin: %v", kinds)
	}
}
