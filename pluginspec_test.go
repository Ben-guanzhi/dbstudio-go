package main

// 插件连接协议的序列化。
//
// 背景：`json.Marshal(dbcore.ConnSpec)` 对 **Dialer（函数类型）** 一律报错
// ——包括 nil。也就是说在加 pluginSpec 之前，**每一次插件连接都会失败**。
// staticcheck 的 SA1026 抓到；这条测试守住它。

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestPluginSpecMarshalsWithADialer(t *testing.T) {
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	spec := dbcore.ConnSpec{
		Name: "via-tunnel", Kind: dbcore.KindPostgres,
		Host: "127.0.0.1", Port: 55432,
		User: "u", Password: "p", Database: "shop",
		Dialer: dialer,
	}
	got, err := jsonMarshalSpec(spec)
	if err != nil {
		t.Fatalf("jsonMarshalSpec: %v", err)
	}
	// 协议里不该出现 Dialer（函数无法序列化，插件也不需要——隧道在客户端侧）。
	if strings.Contains(got, "Dialer") {
		t.Errorf("协议里出现了 Dialer：%s", got)
	}
	// 该带的都带上了。
	for _, want := range []string{`"Name":"via-tunnel"`, `"Kind":"postgres"`, `"Host":"127.0.0.1"`, `"Port":55432`, `"Database":"shop"`} {
		if !strings.Contains(got, want) {
			t.Errorf("协议里没有 %s：%s", want, got)
		}
	}
}

func TestPluginSpecRoundTripsThroughJSON(t *testing.T) {
	// 插件侧解码的是同名字段——编码后再解码回来，值不变。
	spec := dbcore.ConnSpec{Name: "my", Kind: dbcore.KindMySQL, Host: "db", Port: 3307, User: "u", Password: "p", Database: "d", Schema: "s", File: "", SSLMode: "require"}
	raw, err := jsonMarshalSpec(spec)
	if err != nil {
		t.Fatalf("jsonMarshalSpec: %v", err)
	}
	var back pluginSpec
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatalf("解码: %v", err)
	}
	if back.Host != "db" || back.Port != 3307 || back.SSLMode != "require" {
		t.Errorf("往返后丢了字段：%+v", back)
	}
}
