package keyring

// The keyring's contract: a secret round-trips, a tampered value does not
// decrypt into a plausible password, a second keyring cannot open the first's
// values, the legacy cleartext passes through untouched, and the file on disk
// holds no cleartext secret.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	k := Open(filepath.Join(t.TempDir(), "keyring.bin"))
	secret := "p@ss w0rd with spaces & ümlaut"
	enc, err := k.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == secret || enc == "" {
		t.Fatalf("Encrypt returned %q", enc)
	}
	if !strings.HasPrefix(enc, prefix) {
		t.Fatalf("ciphertext %q lacks the %q prefix", enc, prefix)
	}
	got, err := k.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != secret {
		t.Fatalf("round trip = %q, want the secret", got)
	}
	// A second value gets a different ciphertext (a fresh nonce), which also
	// proves two equal secrets are not visibly equal on disk.
	enc2, _ := k.Encrypt(secret)
	if enc2 == enc {
		t.Fatal("two encryptions of one secret are identical")
	}
}

func TestTamperedValueFails(t *testing.T) {
	k := Open(filepath.Join(t.TempDir(), "keyring.bin"))
	enc, err := k.Encrypt("real password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip a byte in the ciphertext body.
	i := strings.LastIndex(enc, ":")
	sealed := []byte(enc[i+1:])
	if sealed[0] == 'A' {
		sealed[0] = 'B'
	} else {
		sealed[0] = 'A'
	}
	tampered := enc[:i+1] + string(sealed)
	if _, err := k.Decrypt(tampered); err == nil {
		t.Fatal("a tampered value decrypted")
	}
	// A truncated value is malformed, not a password.
	if _, err := k.Decrypt(prefix + "garbage"); err == nil {
		t.Fatal("a malformed value decrypted")
	}
}

func TestSecondKeyringCannotOpen(t *testing.T) {
	dir := t.TempDir()
	k1 := Open(filepath.Join(dir, "keyring1.bin"))
	enc, err := k1.Encrypt("one")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// A keyring with its own file (a fresh DEK) cannot open k1's value.
	k2 := Open(filepath.Join(dir, "keyring2.bin"))
	if _, err := k2.Encrypt("x"); err != nil {
		t.Fatalf("k2 setup: %v", err)
	}
	if _, err := k2.Decrypt(enc); err == nil {
		t.Fatal("another keyring's DEK opened the value")
	}
}

func TestLegacyCleartextPassesThrough(t *testing.T) {
	k := Open(filepath.Join(t.TempDir(), "keyring.bin"))
	// An old file's password is cleartext; Decrypt answers it unchanged, so the
	// store keeps working before its first re-save.
	got, err := k.Decrypt("plaintext from an old file")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "plaintext from an old file" {
		t.Fatalf("legacy passthrough = %q", got)
	}
	// An empty password stays empty through Encrypt, which keeps the JSON
	// field empty rather than writing a sealed empty string.
	enc, err := k.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("Encrypt(\"\") = %q, %v; want empty", enc, err)
	}
}

// The file on disk holds no cleartext secret: the seed and salt are public
// values, the DEK and the check are sealed, and connections.json only ever
// carries the prefixed ciphertext.
func TestFileHoldsNoCleartext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keyring.bin")
	k := Open(path)
	secret := "the-very-secret"
	if _, err := k.Encrypt(secret); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("the keyring file holds the secret in the clear")
	}
	if !strings.HasPrefix(string(data), magic) {
		t.Fatal("the keyring file lacks its magic")
	}
	// A second unlock of the same file yields the same DEK: the value round
	// trips across launches.
	k2 := Open(path)
	if _, err := k2.Encrypt("x"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := k2.Decrypt(mustEncrypt(t, k, secret))
	if err != nil {
		t.Fatalf("cross-launch Decrypt: %v", err)
	}
	if got != secret {
		t.Fatalf("cross-launch round trip = %q", got)
	}
}

func mustEncrypt(t *testing.T, k *Keyring, s string) string {
	t.Helper()
	enc, err := k.Encrypt(s)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return enc
}

// A file that is not a keyring (a connections.json copied over it, say) fails
// the load instead of decrypting garbage.
func TestWrongFileRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keyring.bin")
	if err := os.WriteFile(path, []byte(`{"connections": []}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	k := Open(path)
	if _, err := k.Encrypt("x"); err == nil {
		t.Fatal("a non-keyring file was accepted")
	}
}
