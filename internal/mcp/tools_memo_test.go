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

// ---------- N16 C 批：时间窗检索与统计工具 ----------

type memoStatsEnvelope struct {
	TotalCount    int `json:"totalCount"`
	PinnedCount   int `json:"pinnedCount"`
	DistinctTags  int `json:"distinctTags"`
	TagsTruncated bool
	Tags          []struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	} `json:"tagCloud"`
	OldestUpdatedAt string `json:"oldestUpdatedAt"`
	NewestUpdatedAt string `json:"newestUpdatedAt"`
}

func memoStats(t *testing.T, c *client.Client, args map[string]any) (*memoStatsEnvelope, string) {
	t.Helper()
	res, text := callText(t, c, toolMemoStats, args)
	if res.IsError {
		t.Fatalf("unexpected memo stats error: %s", text)
	}
	var env memoStatsEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return &env, text
}

// TestMemoTimeWindow since/until 按更新时间过滤：RFC3339 字面时刻、
// 纯日期按本机时区且 until 含全天；非法格式 fail-loud 指引不静默放行。
func TestMemoTimeWindow(t *testing.T) {
	// 钉本机时区的确定时刻，避开 UTC/本地解释漂移（date-only 语义的验收点）。
	day1 := time.Date(2026, 9, 14, 23, 30, 0, 0, time.Local) // "前一天深夜"
	day2Noon := time.Date(2026, 9, 15, 12, 0, 0, 0, time.Local)
	day2Late := time.Date(2026, 9, 15, 23, 50, 0, 0, time.Local)
	day3 := time.Date(2026, 9, 16, 1, 0, 0, 0, time.Local)
	src := &fakeMemo{items: []memo.MemoItem{
		mkItem("d1", "前一天深夜", "x", false, nil, day1),
		mkItem("d2a", "当日中午", "x", false, nil, day2Noon),
		mkItem("d2b", "当日深夜", "x", false, nil, day2Late),
		mkItem("d3", "次日凌晨", "x", false, nil, day3),
	}}
	c := memoSetup(t, src)

	env, _ := memoSearch(t, c, map[string]any{"since": "2026-09-15", "until": "2026-09-15"})
	if env.Count != 2 || env.Results[0].ID != "d2b" || env.Results[1].ID != "d2a" {
		t.Fatalf("date-only until must cover whole local day, desc order: %+v", env.Results)
	}
	env, _ = memoSearch(t, c, map[string]any{"since": day2Late.Format(time.RFC3339)})
	if env.Count != 2 || env.Results[0].ID != "d3" || env.Results[1].ID != "d2b" {
		t.Fatalf("RFC3339 since must be exact-instant lower bound: %+v", env.Results)
	}
	// until 取 d1 自身的 RFC3339 时刻（闭区间含 d1，d2a 之后）：不依赖本机时区偏移
	env, _ = memoSearch(t, c, map[string]any{"until": day1.Format(time.RFC3339)})
	if env.Count != 1 || env.Results[0].ID != "d1" {
		t.Fatalf("RFC3339 until must be inclusive exact-instant upper bound: %+v", env.Results)
	}
	// 时间窗与 keyword/tag 条件取交集
	env, _ = memoSearch(t, c, map[string]any{"since": "2026-09-16", "keyword": "凌晨"})
	if env.Count != 1 || env.Results[0].ID != "d3" {
		t.Fatalf("window must intersect with keyword: %+v", env.Results)
	}
}

func TestMemoBadTimeParamFailsLoud(t *testing.T) {
	c := memoSetup(t, &fakeMemo{items: []memo.MemoItem{
		mkItem("a", "任意", "x", false, nil, time.Now()),
	}})
	for _, args := range []map[string]any{
		{"since": "昨天"},
		{"until": "2026/09/15"},
		{"since": "2026-09-15T99:99:00Z"},
	} {
		res, text := callText(t, c, toolMemo, args)
		if !res.IsError || !strings.Contains(text, "格式非法") {
			t.Errorf("bad time param %v must guide, got: %s", args, text)
		}
	}
	// 统计工具同款校验
	res, text := callText(t, c, toolMemoStats, map[string]any{"since": "上周"})
	if !res.IsError || !strings.Contains(text, "格式非法") {
		t.Errorf("stats must validate same way, got: %s", text)
	}
}

// TestMemoStatsAggregates 统计口径：非遮罩计数/置顶/标签云（补 # 归一、计数降序
// 同数按名升序的确定性输出）/更新时间范围；时间窗参数生效。
func TestMemoStatsAggregates(t *testing.T) {
	// 截到秒：窗口边界经 RFC3339 格式化会丢亚秒位，闭区间等值断言需要对齐精度。
	base := time.Now().Truncate(time.Second)
	pinned := mkItem("p", "置顶条", "x", false, []string{"#SQL", "todo"}, base)
	pinned.IsPinned = true
	created := mkItem("c", "早创建晚更新", "x", false, []string{"sql"}, base) // 无 # 前缀 → 归一为 #sql
	created.CreatedAt = base.Add(-72 * time.Hour)
	src := &fakeMemo{items: []memo.MemoItem{
		pinned,
		created,
		mkItem("q", "普通", "x", false, []string{"#todo"}, base.Add(-time.Hour)),
		mkItem("m", "遮罩", "TOP_SECRET_STATS", true, []string{"#SecretTag"}, base.Add(time.Hour)),
	}}
	c := memoSetup(t, src)

	env, raw := memoStats(t, c, map[string]any{})
	if env.TotalCount != 3 || env.PinnedCount != 1 {
		t.Fatalf("masked item must be excluded from counts: %+v", env)
	}
	if env.OldestUpdatedAt == "" || env.NewestUpdatedAt == "" {
		t.Fatalf("updated range must be present: %+v", env)
	}
	// 标签云：#SQL(1) #todo(2, 含归一 todo) #sql(1)——"SQL" 与 "sql" 大小写敏感
	// 对齐 GUI GetStats；计数降序：#todo=2 在前。
	if env.DistinctTags != 3 {
		t.Fatalf("tag cloud = %+v", env.Tags)
	}
	if env.Tags[0].Tag != "#todo" || env.Tags[0].Count != 2 {
		t.Fatalf("tag cloud must normalize # and sort by count desc: %+v", env.Tags)
	}
	// 确定性：同库两次调用字节一致（map 无序漂移回归锚）。
	_, raw2 := memoStats(t, c, map[string]any{})
	if raw != raw2 {
		t.Errorf("stats output must be deterministic: %s vs %s", raw, raw2)
	}
	// 时间窗收窄：只要 base 前一小时窗口内的
	env, _ = memoStats(t, c, map[string]any{
		"since": base.Add(-time.Hour).Format(time.RFC3339),
		"until": base.Format(time.RFC3339),
	})
	if env.TotalCount != 3 { // pinned/created(=base)/q(base-1h) 都在闭区间内
		t.Fatalf("window counts wrong: %+v", env)
	}
	env, _ = memoStats(t, c, map[string]any{"since": base.Add(time.Minute).Format(time.RFC3339)})
	if env.TotalCount != 0 { // 只剩遮罩条在窗口内 → 0，且不暴露其存在
		t.Fatalf("masked-only window must count zero: %+v", env)
	}
}

// TestMemoStatsMaskedNeverLeaked 统计面泄露回归：遮罩条目的 id/标题/正文/标签
// 不得以任何形态出现（含标签云与任何 "masked 计数" 式间接字段——存在性即敏感）。
func TestMemoStatsMaskedNeverLeaked(t *testing.T) {
	now := time.Now()
	src := &fakeMemo{items: []memo.MemoItem{
		mkItem("memo_secret", "遮罩笔记", "token=TOP_SECRET_VALUE", true, []string{"#SecretTag"}, now),
	}}
	c := memoSetup(t, src)
	env, raw := memoStats(t, c, map[string]any{})
	for _, banned := range []string{"memo_secret", "遮罩笔记", "TOP_SECRET_VALUE", "SecretTag", "maskedCount"} {
		if strings.Contains(raw, banned) {
			t.Errorf("masked item leaked into stats via %q: %s", banned, raw)
		}
	}
	if env.TotalCount != 0 || len(env.Tags) != 0 {
		t.Fatalf("masked-only library stats must be all-zero: %+v", env)
	}
}

// TestMemoStatsGateMatrix memo 一键控全族：未授权/停用两件同拦，后端零触发。
func TestMemoStatsGateMatrix(t *testing.T) {
	deps, access, _ := newTestServer(t)
	deps.Memo = &fakeMemo{items: []memo.MemoItem{mkItem("a", "x", "y", false, nil, time.Now())}}
	// 无授权文件：两件都 fail-closed
	c := inProcClient(t, deps)
	for _, tool := range []string{toolMemo, toolMemoStats} {
		res, text := callText(t, c, tool, nil)
		if !res.IsError || !strings.Contains(text, "未获授权") {
			t.Errorf("%s must deny without grant: %s", tool, text)
		}
	}
	// 已授权但模块停用：两件都给启用指引
	grant(t, access, map[string]bool{"memo": true})
	deps.Gate = newFakeGate()
	c = inProcClient(t, deps)
	for _, tool := range []string{toolMemo, toolMemoStats} {
		res, text := callText(t, c, tool, nil)
		if !res.IsError || !strings.Contains(text, "停用") {
			t.Errorf("%s must guide on disabled gate: %s", tool, text)
		}
	}
}

// TestMemoStatsDescriptionHonesty 统计工具 description 必须声明遮罩剔除与只读。
func TestMemoStatsDescriptionHonesty(t *testing.T) {
	tool, _ := buildMemoStatsTool(Deps{})
	for _, phrase := range []string{"只读", "整条剔除", "IsMasked"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description must contain %q: %s", phrase, tool.Description)
		}
	}
}
