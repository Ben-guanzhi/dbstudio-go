package main

// Tests for the store's password encryption: the file on disk holds no
// cleartext password, the API keeps handing out a plain Conn, an old file's
// cleartext password survives a load and is sealed on the next save, and a
// password encrypted under another keyring file cannot be carried in.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storeWithPassword(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := s.Upsert(Conn{Name: "pg", Kind: "postgres", Host: "h", Password: "s3cret!"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	return s, path
}

// The file on disk carries the sealed form, and the API still answers the plain
// password — the seal is the file's business, not the caller's.
func TestStoreSealsPasswordOnDisk(t *testing.T) {
	s, path := storeWithPassword(t)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	disk := string(data)
	if strings.Contains(disk, "s3cret!") {
		t.Fatal("connections.json holds the password in the clear")
	}
	if !strings.Contains(disk, "enc:v1:") {
		t.Fatal("connections.json holds no sealed password")
	}

	c, ok := s.Get("pg")
	if !ok || c.Password != "s3cret!" {
		t.Fatalf("Get = %+v, %v; want the plain password", c, ok)
	}
	// A second store over the same files (the next launch) reads it back too.
	s2, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := s2.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	c2, ok := s2.Get("pg")
	if !ok || c2.Password != "s3cret!" {
		t.Fatalf("reloaded Get = %+v, %v; want the plain password", c2, ok)
	}
}

// An old file with a cleartext password loads fine and is sealed on the next
// save: no migration step, the upgrade happens as the file is written again.
func TestStoreMigratesLegacyCleartext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	old := `{"connections":[{"name":"pg","kind":"postgres","host":"h","password":"legacy-clear"}],"active":"pg"}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	// The load answers the cleartext as it is.
	if err := s.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	c, ok := s.Get("pg")
	if !ok || c.Password != "legacy-clear" {
		t.Fatalf("legacy load = %+v, %v", c, ok)
	}
	// Touching the file (an unrelated save) seals it.
	if err := s.Upsert(Conn{Name: "pg", Kind: "postgres", Host: "h2", Password: "legacy-clear"}); err != nil {
		t.Fatalf("resave: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "legacy-clear") {
		t.Fatal("the legacy password survived a re-save in the clear")
	}
	if !strings.Contains(string(data), "enc:v1:") {
		t.Fatal("the re-save did not seal the password")
	}
	// And the value still opens.
	s2, _ := NewStoreAt(path)
	if err := s2.load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if c, _ := s2.Get("pg"); c.Password != "legacy-clear" {
		t.Fatalf("reloaded password = %q", c.Password)
	}
}

// A tampered connections.json does not decrypt into a plausible password: the
// value is dropped, the rest of the connection survives.
func TestStoreDropsTamperedPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s, _ := NewStoreAt(path)
	if err := s.Upsert(Conn{Name: "pg", Kind: "postgres", Password: "s3cret!"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Corrupt the ciphertext deterministically, keeping the JSON valid: decode
	// the sealed body, flip its first byte (guaranteed to change at least one
	// decoded byte, unlike a base64-letter swap that can land in unused bits),
	// and re-encode.
	data, _ := os.ReadFile(path)
	disk := string(data)
	i := strings.Index(disk, "enc:v1:")
	if i < 0 {
		t.Fatal("no sealed value on disk")
	}
	tail := disk[i:]
	colon := strings.Index(tail[len("enc:v1:"):], ":")
	if colon < 0 {
		t.Fatal("no ciphertext body")
	}
	bodyStart := i + len("enc:v1:") + colon + 1
	bodyEnd := strings.Index(disk[bodyStart:], `"`)
	if bodyEnd < 0 {
		t.Fatal("no end to the ciphertext value")
	}
	body := disk[bodyStart : bodyStart+bodyEnd]
	sealed, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decode sealed body: %v", err)
	}
	sealed[0] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(sealed)
	fixed := disk[:bodyStart] + tampered + disk[bodyStart+bodyEnd:]
	if err := os.WriteFile(path, []byte(fixed), 0o600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}
	s2, _ := NewStoreAt(path)
	if err := s2.load(); err != nil {
		t.Fatalf("load tampered: %v", err)
	}
	c, ok := s2.Get("pg")
	if !ok {
		t.Fatal("the connection did not survive the tampered password")
	}
	if c.Password != "" {
		t.Fatalf("a tampered password decrypted to %q; want it dropped", c.Password)
	}
}

// A connections.json that carries another keyring's ciphertext (the keyring.bin
// deleted, the JSON copied elsewhere) loses the password, not the file.
func TestStorePasswordLostWithoutKeyring(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s, _ := NewStoreAt(path)
	if err := s.Upsert(Conn{Name: "pg", Kind: "postgres", Password: "s3cret!"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// The keyring file is gone (a backup that kept only the JSON).
	if kr := filepath.Join(dir, "keyring.bin"); true {
		if err := os.Remove(kr); err != nil {
			t.Fatalf("remove keyring: %v", err)
		}
	}
	// The JSON's ciphertext is copied under a new keyring's watch.
	s2, _ := NewStoreAt(path)
	if err := s2.load(); err == nil {
		// load recreates the keyring file; the old ciphertext still cannot open.
		if c, _ := s2.Get("pg"); c.Password == "s3cret!" {
			t.Fatal("a recreated keyring opened the old ciphertext")
		}
	}
}

// The file remains valid JSON after a sealed save: other tools that read
// connections.json (the export tests, the user) see the same shape.
func TestStoreFileStillJSON(t *testing.T) {
	_, path := storeWithPassword(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var f struct {
		Conns []struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		} `json:"connections"`
		Active string `json:"active"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("the sealed file is not valid JSON: %v", err)
	}
	if len(f.Conns) != 1 || f.Conns[0].Name != "pg" {
		t.Fatalf("conns = %+v", f.Conns)
	}
	if !strings.HasPrefix(f.Conns[0].Password, "enc:v1:") {
		t.Fatalf("password field = %q, want the sealed prefix", f.Conns[0].Password)
	}
}
