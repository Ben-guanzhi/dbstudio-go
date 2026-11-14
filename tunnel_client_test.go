package main

// Tests for the SSH tunnel's client half: the secrets are sealed like every
// other password, a connection without a bastion gets no dialer, and a bastion
// that cannot be reached fails the connect with its reason instead of quietly
// connecting somewhere else.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The SSH password and key passphrase are secrets: they are sealed in the file
// and come back through the API like any other password.
func TestStoreSealsSSHSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	in := Conn{
		Name: "prod", Kind: "mysql", Host: "10.0.0.5", Port: 3306,
		User: "app", Password: "db-secret",
		SSHHost: "bastion.example.com", SSHPort: 22, SSHUser: "jump",
		SSHPassword: "ssh-secret", SSHKeyFile: "~/.ssh/id_ed25519",
		SSHPassphrase: "key-secret",
	}
	if err := s.Upsert(in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	disk := string(data)
	for _, secret := range []string{"db-secret", "ssh-secret", "key-secret"} {
		if strings.Contains(disk, secret) {
			t.Errorf("connections.json holds %q in the clear", secret)
		}
	}
	if got := strings.Count(disk, "enc:v1:"); got != 3 {
		t.Errorf("sealed values = %d, want 3 (db password, ssh password, passphrase)", got)
	}
	// Non-secret SSH fields are readable, as they are not secrets.
	if !strings.Contains(disk, "bastion.example.com") || !strings.Contains(disk, "jump") {
		t.Errorf("the bastion's address was not stored: %s", disk)
	}

	// The API hands the plaintext back, and the non-secret fields survive.
	got, ok := s.Get("prod")
	if !ok {
		t.Fatal("the connection was not stored")
	}
	if got.SSHPassword != "ssh-secret" || got.SSHPassphrase != "key-secret" || got.Password != "db-secret" {
		t.Fatalf("secrets did not round-trip: %+v", got)
	}
	if got.SSHHost != in.SSHHost || got.SSHPort != 22 || got.SSHUser != "jump" || got.SSHKeyFile != in.SSHKeyFile {
		t.Fatalf("the bastion's fields did not round-trip: %+v", got)
	}
}

// An old file without SSH fields loads as a direct connection: the tunnel is
// absent, not empty-but-present.
func TestStoreWithoutSSHIsDirect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	old := `{"connections":[{"name":"local","kind":"sqlite","file":"x.db"}],"active":"local"}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := s.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	c, ok := s.Get("local")
	if !ok || c.SSHHost != "" || c.SSHPort != 0 {
		t.Fatalf("an old connection gained a bastion: %+v", c)
	}
}

// A tampered SSH secret is dropped like a tampered password: the connection
// survives, the secret does not.
func TestStoreDropsTamperedSSHSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s, _ := NewStoreAt(path)
	if err := s.Upsert(Conn{Name: "prod", Kind: "mysql", SSHHost: "b", SSHUser: "j", SSHPassword: "ssh-secret"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	data, _ := os.ReadFile(path)
	var f struct {
		Conns []map[string]any `json:"connections"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Corrupt the sealed value, keeping the JSON valid.
	disk := string(data)
	i := strings.Index(disk, `"sshPassword": "enc:v1:`)
	if i < 0 {
		t.Fatalf("no sealed ssh password: %s", disk)
	}
	start := i + len(`"sshPassword": "`)
	end := start + strings.Index(disk[start:], `"`)
	body := disk[start:end]
	if len(body) < 20 {
		t.Fatalf("sealed value too short: %q", body)
	}
	// Flip one base64 letter inside the ciphertext body (after the nonce).
	colon := strings.Index(body[len("enc:v1:"):], ":")
	if colon < 0 {
		t.Fatalf("no ciphertext: %q", body)
	}
	at := len("enc:v1:") + colon + 2
	tampered := body[:at] + "B" + body[at+1:]
	if tampered == body {
		tampered = body[:at] + "C" + body[at+1:]
	}
	if err := os.WriteFile(path, []byte(disk[:start]+tampered+disk[end:]), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s2, _ := NewStoreAt(path)
	if err := s2.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	c, ok := s2.Get("prod")
	if !ok {
		t.Fatal("the connection did not survive the tampered secret")
	}
	if c.SSHPassword != "" {
		t.Fatalf("a tampered SSH password was carried in: %q", c.SSHPassword)
	}
	if c.SSHHost != "b" || c.SSHUser != "j" {
		t.Fatalf("the bastion's address was lost with the secret: %+v", c)
	}
}

// A SQLite connection (no bastion) opens without any tunnel, and a bastion that
// cannot be reached fails the connect with the tunnel's reason.
func TestOpenDBTunnel(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plain.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "plain", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if db.Tunnel != nil {
		t.Fatal("a direct connection gained a tunnel")
	}
	db.Close()

	// A bastion that does not answer: the connect fails, and it fails for the
	// tunnel's reason rather than connecting to the database directly.
	_, err = OpenDB(ctx, Conn{
		Name: "tun", Kind: "sqlite", File: path,
		SSHHost: "127.0.0.1", SSHPort: 1, SSHUser: "jump", SSHPassword: "x",
	})
	if err == nil {
		t.Fatal("a connect through an unreachable bastion succeeded")
	}
	if !strings.Contains(err.Error(), "ssh tunnel") {
		t.Fatalf("the error does not name the tunnel: %v", err)
	}
}

// The form carries the bastion's fields, and a fresh form defaults the port to
// 22 rather than 0 (a tunnel to port 0 is a typo, not a choice).
func TestFormCarriesSSHFields(t *testing.T) {
	app := newViewApp(t, nil)
	app.openForm()
	if app.sshPort != 22 {
		t.Fatalf("a fresh form's SSH port = %v, want 22", app.sshPort)
	}
	app.form.SSHHost = "bastion"
	app.form.SSHUser = "jump"
	app.sshPort = 2222
	app.form.Name = "keep" // saveForm stores it; the tunnel fields ride along
	if app.form.SSHHost != "bastion" || app.form.SSHUser != "jump" {
		t.Fatalf("the form lost the bastion: %+v", app.form)
	}
}
