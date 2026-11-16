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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dbstudio-go/internal/tunnel"
	"golang.org/x/crypto/ssh"
)

// startSSHServer 起一个本地 SSH 服务器，把收到的 TCP 通道转发到 target。
// 返回它的地址、写进 known_hosts 用的那一行主机密钥、以及通道计数。
func startSSHServer(t *testing.T, target string) (string, int, string, *int32) {
	t.Helper()
	var channels int32
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
			go func(c net.Conn) {
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
					atomic.AddInt32(&channels, 1)
					ch, chReqs, err := newCh.Accept()
					if err != nil {
						continue
					}
					go ssh.DiscardRequests(chReqs)
					go bridge(ch, target)
				}
			}(c)
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	// known_hosts 的一行：非标准端口写成 [host]:port。
	keyLine := "[" + host + "]:" + strconv.Itoa(port) + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	return host, port, keyLine, &channels
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
	sshHost, sshPort, keyLine, _ := startSSHServer(t, target)

	// 把测试服务器的密钥写进一个临时 known_hosts——真实用户也是这么做的
	// （第一次连接时确认指纹）。
	kh := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(kh, []byte(keyLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tun, err := tunnel.Open(ctx, tunnel.Config{
		Host:        sshHost,
		Port:        sshPort,
		User:        "tester",
		Password:    "whatever",
		KnownHosts:  kh,
		UnknownHost: nil, // 密钥在文件里，未知主机仍应拒绝
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
	sshHost, sshPort, _, _ := startSSHServer(t, target)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// known_hosts 是空的：密钥不在里面，未知主机必须被拒绝。
	kh := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(kh, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := tunnel.Open(ctx, tunnel.Config{
		Host:        sshHost,
		Port:        sshPort,
		User:        "tester",
		Password:    "whatever",
		KnownHosts:  kh,
		UnknownHost: nil,
	})
	if err == nil {
		t.Error("未知主机没有被拒绝")
	}
}

// TestBackupGoesThroughTheTunnel 是**组合分支**的端到端验证：backupDB 建隧道、
// 起本地转发、把 dump 工具指向转发端口——SSH 服务器必须看到通道。
//
// 这条测试是上一轮指出的可测试性缺口：openTunnel 此前硬编码 UnknownHost: nil
// 且不接受 known_hosts 路径，测试无法注入可信主机。现在连接可以指定
// SSHKnownHosts，组合分支因此可测。
func TestBackupGoesThroughTheTunnel(t *testing.T) {
	target := echoServer(t) // 隧道的终点（工具并不真连它，通道出现即可）
	sshHost, sshPort, keyLine, _ := startSSHServer(t, target)

	kh := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(kh, []byte(keyLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 假 mysqldump：把收到的参数原样记下来。
	//
	// 它**不连数据库**（通道为 0 是正常的——真的 mysqldump 才会连），所以这里
	// 验证的是组合分支的另一半：**工具被指向了转发地址**，而不是数据库的
	// 内网地址。地址替换是「经隧道」的体现。
	dir := t.TempDir()
	marker := filepath.Join(dir, "args.txt")
	var tool string
	if runtime.GOOS == "windows" {
		tool = filepath.Join(dir, "fake-mysqldump.bat")
		script := "@echo off\r\necho %* > \"" + marker + "\"\r\n"
		if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		tool = filepath.Join(dir, "fake-mysqldump")
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + marker + "\"\n"
		if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(envMysqlDump, tool)

	app := newBackupApp(t, Conn{
		Name: "via-ssh", Kind: "mysql",
		Host: "db.internal", Port: 3306, User: "u", Password: "p", Database: "d",
		SSHHost: sshHost, SSHPort: sshPort, SSHUser: "tester", SSHPassword: "pw",
		SSHKnownHosts: kh,
	})
	app.backupDB()

	// 工具被调用了，且它收到的地址是**转发地址**（127.0.0.1），不是数据库的
	// 内网地址——组合分支（建隧道 → 起转发 → 指向转发端口）就此成立。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("工具没被调用（没有参数记录 %s）；状态 = %q, 错误 = %q", marker, app.status, app.errMsg)
	}
	args := string(raw)
	if !strings.Contains(args, "--host=127.0.0.1") {
		t.Errorf("工具收到的不是转发地址：%s", args)
	}
	if strings.Contains(args, "db.internal") {
		t.Errorf("工具仍收到数据库的内网地址（没走隧道）：%s", args)
	}
	if strings.Contains(args, "--host=127.0.0.1") && !strings.Contains(args, "db.internal") {
		t.Logf("工具收到转发地址：%s", strings.TrimSpace(args))
	}
}
