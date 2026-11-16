package main

// 隧道的端到端验证：起一个**真的本地 SSH 服务器**。
//
// 前面那条测试用「直连的 dialer」验证了转发链，但 SSH 那段是假的。这里补上：
// 一个监听 127.0.0.1 的 SSH 服务器（自签密钥 + 口令认证），把 direct-tcpip
// 通道转发到目标（一个真的 TCP 回声服务器）。于是链路是真的：
//
//	测试 → 本地转发端口 → 隧道 Dialer → SSH 服务器 → 目标 → 回声

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"dbstudio-go/internal/tunnel"
	"golang.org/x/crypto/ssh"
)

// startSSHServer 起一个本地 SSH 服务器，把收到的 TCP 通道转发到 target。
// 返回它的地址。
func startSSHServer(t *testing.T, target string) (string, int) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成主机密钥: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("主机密钥签名器: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			return nil, nil // 测试服务器接受任何口令
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSH(c, cfg, target)
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

// serveSSH 处理一条 SSH 连接：接受 direct-tcpip 通道，把通道桥到 target。
func serveSSH(c net.Conn, cfg *ssh.ServerConfig, target string) {
	conn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "direct-tcpip" {
			_ = newCh.Reject(ssh.UnknownChannelType, "只转发 TCP")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go ssh.DiscardRequests(chReqs)
		go bridge(ch, target)
	}
}

// bridge 把一条通道双向拷贝到 target。
func bridge(ch ssh.Channel, target string) {
	defer ch.Close()
	up, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, ch); done <- struct{}{} }()
	go func() { _, _ = io.Copy(ch, up); done <- struct{}{} }()
	<-done
}

// TestTunnelEndToEnd 是隧道的端到端验证：SSH 服务器是真的，回声服务器是真的，
// 链路必须把字节送过去。
func TestTunnelEndToEnd(t *testing.T) {
	target := echoServer(t)
	sshHost, sshPort := startSSHServer(t, target)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tun, err := tunnel.Open(ctx, tunnel.Config{
		Host:     sshHost,
		Port:     sshPort,
		User:     "tester",
		Password: "whatever",
		// 测试服务器是自签密钥：明确接受它的指纹（真实使用时 UnknownHost
		// 为 nil，未知主机一律拒绝）。
		UnknownHost: func(host, fingerprint string) bool { return true },
	})
	if err != nil {
		t.Fatalf("开隧道: %v", err)
	}
	defer tun.Close()

	fwd, err := startPortForward(ctx, tun.Dialer(), target)
	if err != nil {
		t.Fatalf("本地转发: %v", err)
	}
	defer fwd.close()

	// 连转发端口（工具会连这里），数据应经 SSH 到达回声服务器。
	c, err := net.Dial("tcp", fwd.addr())
	if err != nil {
		t.Fatalf("连转发端口: %v", err)
	}
	defer c.Close()

	msg := "SELECT 1;\n"
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatalf("写: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("读回声: %v", err)
	}
	if string(buf) != msg {
		t.Errorf("回声 = %q, want %q", buf, msg)
	}
}

// TestTunnelRefusesAnUnknownHost：未知主机被拒绝——这是备份不走错服务器的底线。
func TestTunnelRefusesAnUnknownHost(t *testing.T) {
	target := echoServer(t)
	sshHost, sshPort := startSSHServer(t, target)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := tunnel.Open(ctx, tunnel.Config{
		Host:        sshHost,
		Port:        sshPort,
		User:        "tester",
		Password:    "whatever",
		UnknownHost: nil, // 与 openTunnel 的真实策略一致
	})
	if err == nil {
		t.Error("未知主机没有被拒绝")
	}
}
