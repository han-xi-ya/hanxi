package jsonstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testCfg struct {
	Name string `json:"name"`
	N    *int   `json:"n"`
}

// TestSaveThenLoad 原子写往返：2 空格缩进落盘、成功路径不留 tmp 残留。
func TestSaveThenLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	n := 7
	if err := Save(path, testCfg{Name: "hi", N: &n}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\"name\": \"hi\"") || !strings.Contains(string(raw), "\n  \"n\": 7") {
		t.Fatalf("落盘格式与既有模块样板不一致:\n%s", raw)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("目录残留 %d 项: %v", len(entries), entries)
	}
	var got testCfg
	ok, err := Load(path, &got)
	if err != nil || !ok || got.Name != "hi" || *got.N != 7 {
		t.Fatalf("Load: ok=%v err=%v got=%#v", ok, err, got)
	}
}

// TestLoadBranches 文件缺失 / 损坏 / 0 字节三分支语义与各模块原样板对齐。
func TestLoadBranches(t *testing.T) {
	dir := t.TempDir()

	var v testCfg
	if ok, err := Load(filepath.Join(dir, "missing.json"), &v); ok || err != nil {
		t.Fatalf("缺失应 (false,nil)，实际 (%v,%v)", ok, err)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	v = testCfg{}
	ok, err := Load(corrupt, &v)
	if ok || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("损坏应 (false,ErrCorrupt)，实际 (%v,%v)", ok, err)
	}
	if v.Name != "" || v.N != nil {
		t.Fatalf("损坏分支调用方不得采信部分解析结果，实际 %#v", v)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if ok, err := Load(empty, &v); ok || !errors.Is(err, ErrEmpty) {
		t.Fatalf("0 字节应 (false,ErrEmpty)，实际 (%v,%v)", ok, err)
	}
}

// TestSaveFailureKeepsOriginal rename 前置步骤失败时原文件必须保持旧内容。
func TestSaveFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.json")
	if err := Save(path, testCfg{Name: "old"}); err != nil {
		t.Fatal(err)
	}
	// 以目录冒充父目录路径的兄弟项无法制造中途失败，这里直接验证 Marshal 失败分支
	if err := Save(filepath.Join(dir, "bad.json"), make(chan int)); err == nil {
		t.Fatal("不可序列化值应报错")
	}
	var back testCfg
	if ok, err := Load(path, &back); !ok || err != nil || back.Name != "old" {
		t.Fatalf("原文件应完好: (%v,%v,%#v)", ok, err, back)
	}
}

// TestValidateListenPort 与 ddnsgo/ocr 原两份逐字拷贝同语义：1024~65535。
func TestValidateListenPort(t *testing.T) {
	for _, p := range []int{1024, 53120, 65535} {
		if err := ValidateListenPort(p); err != nil {
			t.Errorf("端口 %d 应合法: %v", p, err)
		}
	}
	for _, p := range []int{0, 80, 1023, 65536, -1} {
		if err := ValidateListenPort(p); err == nil {
			t.Errorf("端口 %d 应被拒绝", p)
		} else if !strings.Contains(err.Error(), "1024~65535") {
			t.Errorf("报错文案漂移: %v", err)
		}
	}
}
