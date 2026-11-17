package main

// SQL 代码片段库（对齐 Navicat 片段 / DataGrip live templates / HeidiSQL
// 代码片段）：可命名保存的 SQL 块，比历史多一个**用户起的名字**，比收藏多
// 一个**跨连接**的语义——历史/收藏绑着当时的连接，片段是「这段 SQL 本身」。
//
// 存储：snippets.json，与 connections.json 同目录；条目按名字唯一（同名覆盖
// = 更新），列表按名字排序（稳定顺序）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Snippet 是一条可复用的 SQL 块。
type Snippet struct {
	Name    string    `json:"name"`           // 用户起的名字，唯一键
	SQL     string    `json:"sql"`            // 语句本体
	Note    string    `json:"note,omitempty"` // 可选说明
	At      time.Time `json:"at"`             // 最近更新时间
}

// SnippetStore 是片段库，持锁读写 snippads.json（写走原子替换）。
type SnippetStore struct {
	mu       sync.Mutex
	path     string
	snippets []Snippet
}

// NewSnippetStore 打开（或创建）dir 下的片段库。
func NewSnippetStore(dir string) *SnippetStore {
	return &SnippetStore{path: filepath.Join(dir, "snippets.json")}
}

// Load 读盘；文件不存在/损坏视为空库（首次使用是正常路径，不是错误）。
func (s *SnippetStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		s.snippets = nil
		return nil
	}
	var v struct {
		Snippets []Snippet `json:"snippets"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		s.snippets = nil
		return nil
	}
	s.snippets = v.Snippets
	return nil
}

// List 返回按名字排序的副本。
func (s *SnippetStore) List() []Snippet {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Snippet(nil), s.snippets...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Put 保存一个片段：同名覆盖（= 更新），空白名拒绝。
func (s *SnippetStore) Put(sn Snippet) error {
	sn.Name = strings.TrimSpace(sn.Name)
	if sn.Name == "" {
		return errSnippetName
	}
	sn.SQL = strings.TrimRight(sn.SQL, "\n")
	s.mu.Lock()
	defer s.mu.Unlock()
	sn.At = time.Now()
	for i := range s.snippets {
		if s.snippets[i].Name == sn.Name {
			s.snippets[i] = sn
			return s.saveLocked()
		}
	}
	s.snippets = append(s.snippets, sn)
	return s.saveLocked()
}

// Delete 按名字删除；不存在时报错（UI 能区分「删了」和「本来就没了」）。
func (s *SnippetStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.snippets {
		if s.snippets[i].Name == name {
			s.snippets = append(s.snippets[:i], s.snippets[i+1:]...)
			return s.saveLocked()
		}
	}
	return errSnippetName
}

// saveLocked 原子写盘（tmp + rename）。调用方必须持锁。
func (s *SnippetStore) saveLocked() error {
	raw, err := json.MarshalIndent(struct {
		Snippets []Snippet `json:"snippets"`
	}{s.snippets}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, raw, 0o600)
}

type snippetError string

func (e snippetError) Error() string { return string(e) }

const errSnippetName = snippetError("片段名不能为空")
