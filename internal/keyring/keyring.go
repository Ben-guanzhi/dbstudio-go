// Package keyring encrypts the connections file's passwords at rest.
//
// A single-machine client has no user to type a master password, so the threat
// model is narrower than a sync service's: the file must not hold cleartext
// secrets (a copied connections.json, a backup, a file the user pastes into a
// bug report), and a tampered file must not decrypt silently. It cannot protect
// against an attacker who runs as the same user — nothing on the machine can.
//
// The design is polydb's keyring.bin, cut down to one secret kind:
//
//	keyring.bin   = magic + salt + GCM(nonce, wrappedDEK) + GCM(nonce, salt'|
//	                check), where the wrapping KEK = scrypt(salt, seed) and the
//	                seed is stored in the file's clear part.
//
// The seed in the file means the wrapping is obfuscation against reading the
// JSON alone, not against reading both files; the part that is real is the AEAD:
// a password in connections.json is ciphertext with a tag, so an edit is
// detected rather than silently honored. Secrets go into connections.json as
// "enc:v1:<base64>"; anything else there is read as the legacy cleartext it is,
// and encrypted the next time the file is saved.
package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/scrypt"
)

const (
	// scrypt's work factors. polydb's keyring uses the same numbers; a client
	// that unlocks on every connect could afford less, but it unlocks on every
	// save and load, which is once per launch.
	scryptN = 32768
	scryptR = 8
	scryptP = 1
	keyLen  = 32

	saltLen = 32
	seedLen = 32

	// magic prefixes the file, so a future format can refuse an old one
	// instead of misreading it.
	magic = "DBKR1\000"
)

// ErrNoKeyring is what Unlock answers when no keyring file exists yet: the
// first save creates one, so this is not an error a user sees.
var ErrNoKeyring = errors.New("keyring: no keyring file")

// Keyring holds the file's path and, once unlocked, the data-encrypting key.
type Keyring struct {
	path string
	dek  []byte
}

// Open returns a keyring backed by path. It is unlocked lazily: the work is
// scrypt, and a store that saves three times in a launch should pay it once.
func Open(path string) *Keyring {
	return &Keyring{path: path}
}

// unlocked runs fn with the data-encrypting key, deriving or loading it first.
func (k *Keyring) unlocked(fn func(dek []byte) error) error {
	if k.dek != nil {
		return fn(k.dek)
	}
	dek, err := loadOrCreate(k.path)
	if err != nil {
		return err
	}
	k.dek = dek
	return fn(dek)
}

// loadOrCreate reads the keyring file, deriving its wrapping key from the seed
// it stores, or creates the file when none is there.
func loadOrCreate(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return create(path)
	}
	if len(data) < len(magic)+4+seedLen || string(data[:len(magic)]) != magic {
		return nil, fmt.Errorf("keyring: %s is not a keyring file", filepath.Base(path))
	}
	// saltLen | seed | salt | GCM(wrapped DEK) | GCM(check)
	off := len(magic)
	saltLen := binary.LittleEndian.Uint32(data[off : off+4])
	off += 4
	if int(saltLen) > len(data)-off {
		return nil, errors.New("keyring: corrupt header")
	}
	seed := data[off : off+seedLen]
	off += seedLen
	salt := data[off : off+int(saltLen)]
	off += int(saltLen)
	kek, err := scrypt.Key(seed, salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data)-off < gcm.NonceSize()+keyLen {
		return nil, errors.New("keyring: corrupt key section")
	}
	// The DEK section is nonce | sealed, and the check section follows it; the
	// seal is over exactly the DEK, so the slice ends where the next nonce
	// starts.
	sealStart := off + gcm.NonceSize()
	sealed := data[sealStart : sealStart+gcm.Overhead()+keyLen]
	wrapped, err := gcm.Open(nil, data[off:sealStart], sealed, nil)
	if err != nil {
		return nil, errors.New("keyring: the keyring file was modified or is corrupt")
	}
	return wrapped, nil
}

// create makes a new keyring file: a fresh DEK, wrapped by a KEK derived from a
// fresh seed and salt, both stored in (or beside) the ciphertext.
func create(path string) ([]byte, error) {
	dek := make([]byte, keyLen)
	seed := make([]byte, seedLen)
	salt := make([]byte, saltLen)
	for _, b := range [][]byte{dek, seed, salt} {
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
	}
	kek, err := scrypt.Key(seed, salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	nonce2 := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce2); err != nil {
		return nil, err
	}

	var out []byte
	out = append(out, magic...)
	var l [4]byte
	binary.LittleEndian.PutUint32(l[:], saltLen)
	out = append(out, l[:]...)
	out = append(out, seed...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, gcm.Seal(nil, nonce, dek, nil)...)
	// The check value proves the DEK itself loads before a password is trusted
	// to it: a wrong or truncated file fails here, not halfway through a read.
	out = append(out, nonce2...)
	out = append(out, gcm.Seal(nil, nonce2, salt[:8], nil)...)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, err
	}
	return dek, nil
}

// prefix marks a value as this package's ciphertext.
const prefix = "enc:v1:"

// Encrypt seals a secret under the keyring's DEK, as a value connections.json
// stores verbatim.
func (k *Keyring) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	var out string
	err := k.unlocked(func(dek []byte) error {
		block, err := aes.NewCipher(dek)
		if err != nil {
			return err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return err
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
		out = prefix + base64.StdEncoding.EncodeToString(nonce) + ":" +
			base64.StdEncoding.EncodeToString(sealed)
		return nil
	})
	return out, err
}

// Decrypt opens a value Encrypt produced. Anything without the prefix is the
// legacy cleartext older files stored, and is returned as it is — the caller
// cannot tell, and does not need to; the store re-encrypts on its next save.
// A prefixed value that fails to open is an error: a tampered file must not
// decrypt into a plausible password.
func (k *Keyring) Decrypt(value string) (string, error) {
	if !stringsHasPrefix(value, prefix) {
		return value, nil
	}
	rest := value[len(prefix):]
	parts := split2(rest, ":")
	if len(parts) != 2 {
		return "", errors.New("keyring: malformed encrypted value")
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("keyring: malformed nonce")
	}
	sealed, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("keyring: malformed ciphertext")
	}
	var plain []byte
	err = k.unlocked(func(dek []byte) error {
		block, err := aes.NewCipher(dek)
		if err != nil {
			return err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return err
		}
		plain, err = gcm.Open(nil, nonce, sealed, nil)
		if err != nil {
			return errors.New("keyring: the value was modified or belongs to another keyring")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func stringsHasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func split2(s, sep string) []string {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return []string{s[:i], s[i+len(sep):]}
		}
	}
	return nil
}
