// Package tunnel connects to a database through an SSH bastion.
//
// Production databases often listen on a private network and are reached through
// one jump host. The kernel already models this: a connection can be opened
// through a custom Dialer, and MySQL and PostgreSQL route it into their driver's
// own dialer hook, so the pool's connections all go through the tunnel. What the
// client needs is the tunnel itself: an SSH client, a host-key check, and a
// dialer that opens a forwarded channel to the database's address.
//
// Host keys are verified against known_hosts, not ignored. A tunnel that trusts
// any host key protects nothing against the very attacker a bastion is supposed
// to stop, so an unknown host is an error that names the fingerprint, and
// trusting it is an explicit act recorded in the app's own known_hosts file.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Config is one bastion: where it is, who to be, and how to prove it.
type Config struct {
	Host string
	Port int
	User string
	// Password is the SSH password (one way to authenticate).
	Password string
	// KeyFile is a private key path (~ allowed); KeyPEM is the key's text. A key
	// is preferred over a password when both are given, as ssh does.
	KeyFile string
	KeyPEM  string
	// KeyPassphrase unlocks an encrypted key.
	KeyPassphrase string
	// KnownHosts is the file host keys are checked against; UnknownHost is
	// called when a host key is not in it, and returns whether to trust and
	// record it. A nil UnknownHost refuses every unknown host.
	KnownHosts  string
	UnknownHost func(host string, fingerprint string) bool
	// Timeout bounds the connect; 0 uses defaultTimeout.
	Timeout time.Duration
}

// defaultTimeout bounds opening the SSH connection itself.
const defaultTimeout = 15 * time.Second

// Tunnel is an open SSH connection that forwards TCP channels.
type Tunnel struct {
	client *ssh.Client
	mu     sync.Mutex
	closed bool
}

// Open connects to the bastion.
func Open(ctx context.Context, cfg Config) (*Tunnel, error) {
	if cfg.Host == "" {
		return nil, errors.New("ssh tunnel: no host")
	}
	if cfg.User == "" {
		return nil, errors.New("ssh tunnel: no user")
	}
	auth, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	// ssh.ClientConfig has no context; the dial does, and the handshake is
	// bounded by Timeout.
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port(cfg.Port)))
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh tunnel: dial %s: %w", addr, err)
	}
	// Bound the handshake: a peer that accepts TCP and then says nothing must
	// not hold the connect forever.
	_ = conn.SetDeadline(time.Now().Add(timeout))
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         timeout,
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh tunnel: handshake with %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return &Tunnel{client: ssh.NewClient(clientConn, chans, reqs)}, nil
}

// port is the bastion's port, 22 when unset.
func port(p int) int {
	if p <= 0 {
		return 22
	}
	return p
}

// authMethods picks the credentials: a key if there is one, then a password.
func authMethods(cfg Config) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if pem, err := keyMaterial(cfg); err != nil {
		return nil, err
	} else if len(pem) > 0 {
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			if cfg.KeyPassphrase != "" {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(cfg.KeyPassphrase))
			}
			if err != nil {
				return nil, fmt.Errorf("ssh tunnel: read key: %w", err)
			}
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}
	if len(methods) == 0 {
		return nil, errors.New("ssh tunnel: no password and no key")
	}
	return methods, nil
}

// keyMaterial is the private key's bytes from KeyPEM or KeyFile.
func keyMaterial(cfg Config) ([]byte, error) {
	if cfg.KeyPEM != "" {
		return []byte(cfg.KeyPEM), nil
	}
	if cfg.KeyFile == "" {
		return nil, nil
	}
	path := expandHome(cfg.KeyFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ssh tunnel: read key file: %w", err)
	}
	return data, nil
}

// expandHome resolves a leading ~ against the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(os.PathSeparator)))
}

// hostKeyCallback checks the bastion's key against known_hosts, and asks about a
// host it has never seen. An unknown host is refused when nobody can answer.
func hostKeyCallback(cfg Config) (ssh.HostKeyCallback, error) {
	known := cfg.KnownHosts
	if known == "" {
		known = DefaultKnownHosts()
	}
	var check ssh.HostKeyCallback
	if known != "" {
		if _, err := os.Stat(known); err == nil {
			cb, err := knownhosts.New(known)
			if err != nil {
				return nil, fmt.Errorf("ssh tunnel: read %s: %w", known, err)
			}
			check = cb
		}
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if check != nil {
			err := check(hostname, remote, key)
			if err == nil {
				return nil
			}
			var keyErr *knownhosts.KeyError
			if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
				// The host is known and the key differs: that is the attack this
				// check exists for, and it is never overridable from here.
				return err
			}
		}
		fingerprint := ssh.FingerprintSHA256(key)
		if cfg.UnknownHost == nil || !cfg.UnknownHost(hostname, fingerprint) {
			return fmt.Errorf("ssh tunnel: %s is not a known host (fingerprint %s); add it to %s to trust it",
				hostname, fingerprint, known)
		}
		return appendKnownHost(known, hostname, key)
	}, nil
}

// appendKnownHost records a trusted host key, so the next connection is silent.
func appendKnownHost(path, host string, key ssh.PublicKey) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(host)}, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("ssh tunnel: record host key: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// DefaultKnownHosts is the app's own known_hosts file: ~/.ssh/known_hosts when
// it exists, otherwise the file this tool keeps for itself.
func DefaultKnownHosts() string {
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"known_hosts", "known_hosts2"} {
			p := filepath.Join(home, ".ssh", name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "dbstudio", "ssh_known_hosts")
}

// Dialer is the kernel-shaped dialer: it opens a forwarded channel to addr
// (host:port, relative to the bastion's network) for each connection the pool
// makes, so the tunnel carries every physical connection rather than one.
func (t *Tunnel) Dialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.mu.Lock()
		closed := t.closed
		t.mu.Unlock()
		if closed {
			return nil, errors.New("ssh tunnel: closed")
		}
		// ssh.Client.Dial has no context: bound it with a deadline on the
		// channel by running it beside the context.
		type result struct {
			conn net.Conn
			err  error
		}
		ch := make(chan result, 1)
		go func() {
			conn, err := t.client.Dial(network, addr)
			ch <- result{conn, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-ch:
			if r.err != nil {
				return nil, fmt.Errorf("ssh tunnel: forward to %s: %w", addr, r.err)
			}
			return r.conn, nil
		}
	}
}

// Close ends the SSH connection and every channel through it.
func (t *Tunnel) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	return t.client.Close()
}

// ServerVersion is the bastion's identification string, for the status line.
func (t *Tunnel) ServerVersion() string { return string(t.client.ServerVersion()) }
