package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"

	"hanxi/internal/jsonstore"
	"hanxi/internal/modules/memo"
)

// ---------- memoDiskReader：零落盘直读矩阵 ----------

func mkItem(id, title, content string, masked bool, tags []string, updated time.Time) memo.MemoItem {
	return memo.MemoItem{
		ID: id, Title: title, Content: content, Tags: tags,
		IsMasked: masked, ColorTag: "blue",
		CreatedAt: updated.Add(-time.Hour), UpdatedAt: updated,
	}
}

func writeMemoFiles(t *testing.T, dir string, items ...memo.MemoItem) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		data, err := memo.EncodeMemo(it)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, it.ID+".md"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestMemoDiskReaderFileStore(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memo")
	now := time.Now()
	writeMemoFiles(t, dir,
		mkItem("memo_1", "数据库连接", "postgres://localhost/db", false, []string{"#SQL"}, now),
		mkItem("memo_2", "生产 API Key", "token=LEAK_ME_NOT", true, []string{"#Token"}, now.Add(time.Second)),
	)
	// 坏条目（无 frontmatter）：跳过、不隔离、不动别条
	if err := os.WriteFile(filepath.Join(dir, "memo_bad.md"), []byte("not a memo"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirEntries(t, dir)

	r := &memoDiskReader{dir: dir, legacyPath: filepath.Join(root, "state", "memo.json")}
	items, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (坏条跳过): %+v", len(items), items)
	}
	// 零落盘断言：目录清单不因读取而变化（无 .bad- 隔离副本、无迁移产物）
	after := dirEntries(t, dir)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("reader must not write into memo dir: before=%v after=%v", before, after)
	}
	// 遮罩过滤发生在工具层——reader 如实装载含遮罩条目
	var sawMasked bool
	for _, it := range items {
		if it.IsMasked {
			sawMasked = true
		}
	}
	if !sawMasked {
		t.Error("reader should load masked items (filtering is the tool layer's job)")
	}
}

func TestMemoDiskReaderLegacyFallback(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "state", "memo.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	items := []memo.MemoItem{mkItem("m1", "旧库条目", "内容", false, nil, time.Now())}
	if err := jsonstore.Save(legacy, items); err != nil {
		t.Fatal(err)
	}
	r := &memoDiskReader{dir: filepath.Join(root, "memo"), legacyPath: legacy}
	got, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "旧库条目" {
		t.Fatalf("legacy fallback failed: %+v", got)
	}
}

func TestMemoDiskReaderCorruptBehaviors(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "memo.json")
	if err := os.WriteFile(legacy, []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 旧库损坏：报错且不隔离改名（fail-loud 零落盘）
	r := &memoDiskReader{dir: filepath.Join(root, "memo"), legacyPath: legacy}
	if _, err := r.Load(); err == nil {
		t.Fatal("corrupt legacy must fail loud")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("corrupt legacy file must stay untouched (quarantine is GUI's job): %v", err)
	}

	// 文件库有货但全部解析失败：不回落旧库（防读陈旧数据）
	dir := filepath.Join(root, "memo")
	writeMemoFiles(t, dir) // 空目录 → hasFiles=false，先确认回落路径
	if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, hasFiles, err := r.loadDir()
	if err != nil || len(items) != 0 || !hasFiles {
		t.Fatalf("all-bad dir: items=%v hasFiles=%v err=%v", items, hasFiles, err)
	}
	if _, err := r.Load(); err != nil {
		t.Fatalf("hasFiles path must not fall back to corrupt legacy as error: %v", err)
	}
}

// ---------- 工具面：遮罩不下发泄露回归 + 过滤语义 ----------

type fakeMemo struct{ items []memo.MemoItem }

func (f *fakeMemo) Load() ([]memo.MemoItem, error) { return f.items, nil }

func memoSetup(t *testing.T, src MemoSource, ids ...string) *client.Client {
	t.Helper()
	deps, access, _ := newTestServer(t)
	deps.Memo = src
	grant(t, access, map[string]bool{"memo": true})
	return inProcClient(t, deps)
}

func memoSearch(t *testing.T, c *client.Client, args map[string]any) (*memoEnvelope, string) {
	t.Helper()
	res, text := callText(t, c, toolMemo, args)
	if res.IsError {
		t.Fatalf("unexpected memo error: %s", text)
	}
	var env memoEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return &env, text
}

type memoEnvelope struct {
	Count     int            `json:"count"`
	Truncated bool           `json:"truncated"`
	Results   []memoWireItem `json:"results"`
}

type memoWireItem struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// TestMemoMaskedNeverLeaked 决策 2 泄露回归锚点：遮罩条目的 id/标题/正文任何形态
// 都不得出现在模型可见输出中（哪怕关键词恰好命中）。
func TestMemoMaskedNeverLeaked(t *testing.T) {
	now := time.Now()
	src := &fakeMemo{items: []memo.MemoItem{
		mkItem("memo_public", "公开笔记", "普通内容 keyword=hit", false, []string{"#demo"}, now),
		mkItem("memo_secret", "遮罩笔记 keyword=hit", "token=TOP_SECRET_VALUE keyword=hit", true, []string{"#demo"}, now.Add(time.Second)),
	}}
	c := memoSetup(t, src)

	env, raw := memoSearch(t, c, map[string]any{"keyword": "keyword=hit"})
	for _, banned := range []string{"memo_secret", "遮罩笔记", "TOP_SECRET_VALUE"} {
		if strings.Contains(raw, banned) {
			t.Errorf("masked item content leaked into payload via %q: %s", banned, raw)
		}
	}
	if env.Count != 1 || env.Results[0].ID != "memo_public" {
		t.Fatalf("only public item may appear: %+v", env)
	}
	// 全库仅遮罩条目：命中查询返回空列表（可发现性：模型据 description 知晓过滤存在）
	c2 := memoSetup(t, &fakeMemo{items: []memo.MemoItem{src.items[1]}})
	env2, _ := memoSearch(t, c2, map[string]any{"keyword": "TOP_SECRET"})
	if env2.Count != 0 {
		t.Fatalf("masked-only library must return empty, got %+v", env2)
	}
}

// TestMemoFilterAndOrder keyword/tag/limit 语义与更新时间倒序。
func TestMemoFilterAndOrder(t *testing.T) {
	base := time.Now()
	src := &fakeMemo{items: []memo.MemoItem{
		mkItem("old", "老笔记", "zzz", false, []string{"#SQL"}, base.Add(-2*time.Hour)),
		mkItem("new", "新笔记", "aaa note", false, []string{"#SQL", "#todo"}, base),
		mkItem("mid", "中笔记", "note bbb", false, nil, base.Add(-time.Hour)),
	}}
	c := memoSetup(t, src)

	env, _ := memoSearch(t, c, map[string]any{})
	if env.Count != 3 || env.Results[0].ID != "new" || env.Results[2].ID != "old" {
		t.Fatalf("default listing must be updatedAt desc: %+v", env.Results)
	}

	env, _ = memoSearch(t, c, map[string]any{"keyword": "NOTE"})
	if env.Count != 2 {
		t.Fatalf("keyword match wrong: %+v", env.Results)
	}
	env, _ = memoSearch(t, c, map[string]any{"tag": "sql"})
	if env.Count != 2 {
		t.Fatalf("tag match must tolerate # and case: %+v", env.Results)
	}
	env, _ = memoSearch(t, c, map[string]any{"limit": 2.0})
	if env.Count != 2 || !env.Truncated {
		t.Fatalf("limit cap must report truncated: %+v", env)
	}
}

// TestMemoContentRedacted 非遮罩条目的标题/正文也过 Redact 口径（防未标记密钥裸奔出网）。
func TestMemoContentRedacted(t *testing.T) {
	src := &fakeMemo{items: []memo.MemoItem{
		mkItem("a", "含密钥标题 password=hunter2", "正文 secret=MYVAL99 与 token=MYTOKENXYZ", false, nil, time.Now()),
	}}
	c := memoSetup(t, src)
	_, raw := memoSearch(t, c, map[string]any{})
	for _, banned := range []string{"hunter2", "MYVAL99", "MYTOKENXYZ"} {
		if strings.Contains(raw, banned) {
			t.Errorf("unmarked secret %q must be scrubbed via Redact: %s", banned, raw)
		}
	}
}

// TestMemoGateDisabled memo 模块停用：指引错误、后端零触发。
func TestMemoGateDisabled(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"memo": true})
	deps.Memo = &fakeMemo{}
	deps.Gate = newFakeGate() // 全停用（含 memo）
	c := inProcClient(t, deps)
	res, text := callText(t, c, toolMemo, map[string]any{})
	if !res.IsError || !strings.Contains(text, "停用") {
		t.Fatalf("disabled memo must guide, got: %s", text)
	}
}

// TestMemoDescriptionHonesty description 必须声明遮罩不下发政策（模型的知情口径）。
func TestMemoDescriptionHonesty(t *testing.T) {
	tool, _ := buildMemoTool(Deps{})
	for _, phrase := range []string{"整条不下发", "只读"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description must contain %q: %s", phrase, tool.Description)
		}
	}
}
