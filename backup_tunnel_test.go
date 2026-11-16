package main

// 端口转发：让独立进程的 dump 工具走 SSH 隧道的那一段。
//
// 隧道本身由 internal/tunnel 负责（它有自己的测试）；这里测的是**本地监听 →
// 经 dialer 转发到远端**这条链：工具连的是 127.0.0.1 的随机端口，数据要原样
// 到达远端。用一个真的 TCP 回声服务器当远端，dialer 直连它——链路是实的。

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

// echoServer 起一个回声服务器，返回它的地址。
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	return ln.Addr().String()
}

// directDialer 返回一个直连的 dialer（测试里替代隧道）。
func directDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func TestPortForwardCarriesBytes(t *testing.T) {
	remote := echoServer(t)
	fwd, err := startPortForward(context.Background(), directDialer, remote)
	if err != nil {
		t.Fatalf("startPortForward: %v", err)
	}
	defer fwd.close()

	c, err := net.Dial("tcp", fwd.addr())
	if err != nil {
		t.Fatalf("连转发端口: %v", err)
	}
	defer c.Close()

	msg := "SELECT 1;\n"
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("读回声: %v", err)
	}
	if string(buf) != msg {
		t.Errorf("回声 = %q, want %q", buf, msg)
	}
}

func TestTunneledConnReplacesTheAddress(t *testing.T) {
	remote := echoServer(t)
	fwd, err := startPortForward(context.Background(), directDialer, remote)
	if err != nil {
		t.Fatal(err)
	}
	defer fwd.close()

	conn := Conn{Kind: "postgres", Host: "db.internal", Port: 5432, User: "u", Database: "d"}
	got := tunneledConn(conn, fwd)

	host, portStr, err := net.SplitHostPort(fwd.addr())
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != host {
		t.Errorf("Host = %q, want %q", got.Host, host)
	}
	if p, _ := strconv.Atoi(portStr); got.Port != p {
		t.Errorf("Port = %d, want %d", got.Port, p)
	}
	// 其它字段不动——工具还要用它们。
	if got.User != "u" || got.Database != "d" || got.Kind != "postgres" {
		t.Errorf("tunneledConn 改了别的字段：%+v", got)
	}
}

func TestTunneledConnLeavesANonForwardedConnAlone(t *testing.T) {
	// 拿一个坏地址的转发（已关闭）时，连接原样返回——不能凭空造地址。
	fwd := &portForward{}
	conn := Conn{Kind: "postgres", Host: "db.internal", Port: 5432}
	got := tunneledConn(conn, fwd)
	if got.Host != "db.internal" || got.Port != 5432 {
		t.Errorf("坏转发改了连接：%+v", got)
	}
}

func TestOpenTunnelNeedsAnSSHHost(t *testing.T) {
	// 没有 SSH 主机就没有隧道（返回 nil，不是错误）。
	tun, err := openTunnel(context.Background(), Conn{Kind: "mysql", Host: "db"})
	if err != nil {
		t.Fatalf("openTunnel: %v", err)
	}
	if tun != nil {
		t.Error("没有 SSH 主机却开了隧道")
	}
	if !strings.Contains("x", "x") {
		t.Error("占位")
	}
}
