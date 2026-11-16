package main

// 让 dump 工具走 SSH 隧道。
//
// dbstudio 自己的连接直接用隧道的 Dialer（Go 代码里拨号），但 mysqldump /
// pg_dump 是**独立进程**，它们只会连一个 TCP 地址。所以在本地开一个监听端口，
// 把每个连接经隧道转发到数据库——工具对隧道无感知，与 TablePlus 的做法一致。
//
// 波 101 续五十五。

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"dbstudio-go/internal/tunnel"
)

// openTunnel 按连接里的 SSH 字段开隧道。未知主机**拒绝**而不是信任：一次静默
// 信任陌生人密钥的备份不是一条隧道（与连接时的策略一致）。
func openTunnel(ctx context.Context, conn Conn) (*tunnel.Tunnel, error) {
	if conn.SSHHost == "" {
		return nil, nil
	}
	return tunnel.Open(ctx, tunnel.Config{
		Host:          conn.SSHHost,
		Port:          conn.SSHPort,
		User:          conn.SSHUser,
		Password:      conn.SSHPassword,
		KeyFile:       conn.SSHKeyFile,
		KeyPassphrase: conn.SSHPassphrase,
		UnknownHost:   nil,
	})
}

// portForward 是本地监听端口到远端地址的转发：每个进来的连接都经 dialer
// 开到 remote，然后双向拷贝。
type portForward struct {
	ln     net.Listener
	closed chan struct{}
}

// startPortForward 在 127.0.0.1 上开一个随机端口，把连接经 dialer 转发到
// remote（"host:port"）。
func startPortForward(ctx context.Context, dialer func(ctx context.Context, network, addr string) (net.Conn, error), remote string) (*portForward, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("本地转发端口: %w", err)
	}
	f := &portForward{ln: ln, closed: make(chan struct{})}
	go f.serve(ctx, dialer, remote)
	return f, nil
}

func (f *portForward) serve(ctx context.Context, dialer func(ctx context.Context, network, addr string) (net.Conn, error), remote string) {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return // 监听已关
		}
		go func(c net.Conn) {
			defer c.Close()
			up, err := dialer(ctx, "tcp", remote)
			if err != nil {
				return
			}
			defer up.Close()
			// 双向拷贝，任一侧断开即结束这条连接。
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
			go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
			<-done
		}(c)
	}
}

// addr 是转发监听的地址（127.0.0.1:随机端口），dump 工具连它。
func (f *portForward) addr() string { return f.ln.Addr().String() }

// close 关掉监听与在途连接。
func (f *portForward) close() {
	select {
	case <-f.closed:
		return
	default:
	}
	close(f.closed)
	_ = f.ln.Close()
}

// tunneledConn 把一个连接改写成「经隧道」的版本：主机换成转发地址，端口换成
// 转发端口。原连接的其它字段不动。
func tunneledConn(conn Conn, f *portForward) Conn {
	// 坏转发（未启动、已关闭）不改写连接——宁可让工具去连原地址报错，
	// 也不要凭空造一个地址。
	if f == nil || f.ln == nil {
		return conn
	}
	host, portStr, err := net.SplitHostPort(f.ln.Addr().String())
	if err != nil {
		return conn
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return conn
	}
	out := conn
	out.Host = host
	out.Port = port
	return out
}

// sshTimeout 是备份建隧道的合理上限：隧道本身有默认超时，这里再给一层，
// 让备份不会因隧道挂住而永远不返回。
var sshTimeout = 20 * time.Second
