package tunnel

// Tests against a real SSH server: x/crypto/ssh's server side on a loopback
// port with password auth. That proves the three things that matter — the
// handshake happens, a forwarded channel carries bytes to a service behind the
// bastion, and an unknown host key is refused rather than trusted.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// testServer is a bastion that accepts one password and forwards each
// direct-tcpip channel to target.
type testServer struct {
	addr   string
	pubKey ssh.PublicKey
	mu     sync.Mutex
	closed bool
	ln     net.Listener
}

// startServer runs a bastion in front of target (a host:port it can reach).
func startServer(t *testing.T, target string) *testServer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "tester" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &testServer{addr: ln.Addr().String(), pubKey: signer.PublicKey(), ln: ln}
	t.Cleanup(s.Close)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(conn, cfg, target)
		}
	}()
	return s
}

// serveConn runs the server side of one connection, forwarding direct-tcpip
// channels to target.
func serveConn(conn net.Conn, cfg *ssh.ServerConfig, target string) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			channel, reqs, err := ch.Accept()
			if err != nil {
				continue
			}
			go ssh.DiscardRequests(reqs)
			go io.Copy(io.Discard, channel)
			continue
		}
		var payload struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if err := ssh.Unmarshal(ch.ExtraData(), &payload); err != nil {
			ch.Reject(ssh.Prohibited, "bad payload")
			continue
		}
		upstream, err := net.Dial("tcp", target)
		if err != nil {
			ch.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			upstream.Close()
			continue
		}
		go ssh.DiscardRequests(requests)
		go func() {
			defer channel.Close()
			defer upstream.Close()
			done := make(chan struct{}, 2)
			go func() { io.Copy(upstream, channel); done <- struct{}{} }()
			go func() { io.Copy(channel, upstream); done <- struct{}{} }()
			<-done
		}()
	}
}

func (s *testServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.ln.Close()
}

// knownHostsFor records the bastion's key the way ssh-keyscan would.
func knownHostsFor(t *testing.T, s *testServer) string {
	t.Helper()
	host, _, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatalf("split %s: %v", s.addr, err)
	}
	_ = host
	line := knownhosts.Normalize(s.addr) + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.pubKey)))
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

// config builds the client config for a test server.
func config(t *testing.T, s *testServer) (Config, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	return Config{Host: host, Port: atoi(portStr), User: "tester", Password: "secret"}, atoi(portStr)
}

// echoServer answers with "echo: <request>".
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Answer line by line: an SSH channel does not half-close, so a
				// server that waits for EOF would never answer.
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					_, _ = c.Write([]byte("echo: " + sc.Text() + "\n"))
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

// A tunnel to a bastion carries a forwarded channel to a service behind it.
func TestTunnelForwards(t *testing.T) {
	target := echoServer(t)
	srv := startServer(t, target)
	cfg, _ := config(t, srv)
	cfg.KnownHosts = knownHostsFor(t, srv)

	tun, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tun.Close()
	if tun.ServerVersion() == "" {
		t.Error("the bastion's version is empty")
	}

	conn, err := tun.Dialer()(context.Background(), "tcp", target)
	if err != nil {
		t.Fatalf("dial through tunnel: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "echo: hello\n" {
		t.Fatalf("through the tunnel = %q, want echo: hello", got)
	}
}

// An unknown host is refused when nobody can vouch for it, and the error names
// the fingerprint so it can be checked out of band.
func TestTunnelRefusesUnknownHost(t *testing.T) {
	target := echoServer(t)
	srv := startServer(t, target)
	cfg, _ := config(t, srv)
	cfg.KnownHosts = filepath.Join(t.TempDir(), "known_hosts") // empty

	_, err := Open(context.Background(), cfg)
	if err == nil {
		t.Fatal("a bastion with an unknown host key was trusted")
	}
	if !strings.Contains(err.Error(), "SHA256:") {
		t.Fatalf("the error does not name the fingerprint: %v", err)
	}
}

// A host key that changed under a known host is never overridable: that is the
// attack the check exists for.
func TestTunnelRefusesChangedHostKey(t *testing.T) {
	target := echoServer(t)
	srv := startServer(t, target)
	cfg, _ := config(t, srv)

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	otherSigner, err := ssh.NewSignerFromKey(other)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	line := knownhosts.Normalize(srv.addr) + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(otherSigner.PublicKey())))
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg.KnownHosts = path
	cfg.UnknownHost = func(string, string) bool { return true } // must not matter

	if _, err := Open(context.Background(), cfg); err == nil {
		t.Fatal("a changed host key was accepted")
	}
}

// The hook that answers "trust this unknown host" records the key, so the next
// connection is silent.
func TestTunnelTrustsOnRequest(t *testing.T) {
	target := echoServer(t)
	srv := startServer(t, target)
	cfg, _ := config(t, srv)
	kh := filepath.Join(t.TempDir(), "known_hosts")
	cfg.KnownHosts = kh

	asked := 0
	cfg.UnknownHost = func(host, fp string) bool {
		asked++
		if !strings.HasPrefix(fp, "SHA256:") {
			t.Errorf("fingerprint = %q, want SHA256:...", fp)
		}
		return true
	}
	tun, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tun.Close()
	if asked != 1 {
		t.Fatalf("asked %d time(s), want 1", asked)
	}
	data, err := os.ReadFile(kh)
	if err != nil || len(data) == 0 {
		t.Fatalf("the trusted key was not recorded: %v", err)
	}

	// The second connection already knows the host.
	again := 0
	cfg.UnknownHost = func(string, string) bool { again++; return true }
	tun2, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	tun2.Close()
	if again != 0 {
		t.Fatalf("the recorded host was asked about %d time(s)", again)
	}
}

// Wrong credentials fail at the handshake with its own error.
func TestTunnelRejectsBadPassword(t *testing.T) {
	target := echoServer(t)
	srv := startServer(t, target)
	cfg, _ := config(t, srv)
	cfg.Password = "wrong"
	cfg.KnownHosts = knownHostsFor(t, srv)
	if _, err := Open(context.Background(), cfg); err == nil {
		t.Fatal("a wrong password connected")
	}
}

// The dialer refuses work once the tunnel is closed.
func TestDialerRefusesAfterClose(t *testing.T) {
	target := echoServer(t)
	srv := startServer(t, target)
	cfg, _ := config(t, srv)
	cfg.KnownHosts = knownHostsFor(t, srv)
	tun, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dial := tun.Dialer()
	tun.Close()
	if _, err := dial(context.Background(), "tcp", target); err == nil {
		t.Fatal("a closed tunnel still dialed")
	}
	// Closing twice is not an error.
	if err := tun.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// The dialer honors a cancelled context instead of waiting out the channel.
func TestDialerHonorsContext(t *testing.T) {
	target := echoServer(t)
	srv := startServer(t, target)
	cfg, _ := config(t, srv)
	cfg.KnownHosts = knownHostsFor(t, srv)
	tun, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tun.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tun.Dialer()(ctx, "tcp", target); err == nil {
		t.Fatal("a cancelled context still dialed")
	}
}

// The config's missing pieces are reported by name.
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		cfg  Config
		want string
	}{
		{Config{}, "host"},
		{Config{Host: "h"}, "user"},
		{Config{Host: "h", User: "u"}, "password"},
	}
	for _, c := range cases {
		_, err := Open(context.Background(), c.cfg)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Open(%+v) = %v, want it to name %q", c.cfg, err, c.want)
		}
	}
}

// atoi reads a decimal port from SplitHostPort.
func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
