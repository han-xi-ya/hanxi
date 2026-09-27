package clipboard

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- 包内共用测试假件 ----------

// fakeBlobIO blobIO 假件：内存记账，Delete 自动核减总量（体积淘汰可收敛）。
type fakeBlobIO struct {
	mu        sync.Mutex
	size      int64 // 每个 blob 记账字节
	files     map[string]int64
	saves     int
	deletes   []string
	loadCalls []string
	nextRelN  int
	saveErr   error
	loadErr   error
	deleteErr error
	totalErr  error
}

func newFakeBlobs() *fakeBlobIO {
	return &fakeBlobIO{files: map[string]int64{}, size: 1 << 20}
}

func (f *fakeBlobIO) SaveImage(img image.Image) (string, int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.saveErr != nil {
		return "", 0, 0, f.saveErr
	}
	rel := fmt.Sprintf("fake-%03d.png", f.nextRelN)
	f.nextRelN++
	f.files[rel] = f.size
	b := img.Bounds()
	return rel, b.Dx(), b.Dy(), nil
}

func (f *fakeBlobIO) Load(rel string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadCalls = append(f.loadCalls, rel)
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if _, ok := f.files[rel]; !ok {
		return nil, fmt.Errorf("fake blob 不存在: %s", rel)
	}
	return []byte("PNGFAKE"), nil
}

func (f *fakeBlobIO) Delete(rel string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletes = append(f.deletes, rel)
	delete(f.files, rel)
	return nil
}

func (f *fakeBlobIO) TotalBytes() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.totalErr != nil {
		return 0, f.totalErr
	}
	var sum int64
	for _, s := range f.files {
		sum += s
	}
	return sum, nil
}

func (f *fakeBlobIO) deleteCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

// fakeSeal/fakeOpen 可逆假件（"enc:"+base64）：验证加解密调用链而不依赖 DPAPI。
func fakeSeal(b []byte) (string, error) { return "enc:" + base64.StdEncoding.EncodeToString(b), nil }

func fakeOpen(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "enc:") {
		return nil, fmt.Errorf("假密文前缀不合法: %q", s)
	}
	return base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "enc:"))
}

func newTestStore(t *testing.T) (*Store, *fakeBlobIO, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clipboard")
	fb := newFakeBlobs()
	st, err := NewStore(dir, fb, fakeSeal, fakeOpen)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st, fb, dir
}

func entryText(id, text string) Entry {
	return Entry{
		ID: id, Hash: hashOf(KindText, text, nil, nil), Kind: KindText, Text: text,
		Preview: text, ByteSize: int64(len(text)), CreatedAt: time.Now().UnixMilli(),
	}
}

var testImg = image.NewRGBA(image.Rect(0, 0, 4, 2))

// ---------- 测试 ----------

// TestClipStoreInitAndPersistence 新建空库落基线文件；Add 后整库落盘、
// 重开装载一致（text 经假加密链往返）。
func TestClipStoreInitAndPersistence(t *testing.T) {
	st, _, dir := newTestStore(t)
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatalf("构造未落空库基线: %v", err)
	}
	added, evicted, err := st.Add(entryText("id-a", "alpha"), time.Now())
	if err != nil || len(evicted) != 0 || added.ID != "id-a" {
		t.Fatalf("Add: %+v %v %v", added, evicted, err)
	}

	// 盘上是密文（fake 前缀可验证调用链真的走了 seal）
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"text": "alpha"`) {
		t.Fatal("明文被直接落盘，seal 链未生效")
	}
	if !strings.Contains(string(raw), "enc:") {
		t.Fatal("盘上未见密文前缀")
	}

	reopened, err := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get("id-a")
	if !ok || got.Text != "alpha" {
		t.Fatalf("重开装载: %+v ok=%v", got, ok)
	}
}

// TestClipStorePlaintextRoundTrip 默认恒等缝（契约 §12 v1.6 机主裁决取消 DPAPI）：
// 写入→盘上明文→重开还原。加解密链路的可逆性由 fakeSeal/fakeOpen 注入用例
// （TestClipStoreSealChain 谱系）钉住；本用例钉的是"默认形态=明文"这一事实——
// 将来若复议恢复加密，本用例红即为契约变更信号。
func TestClipStorePlaintextRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clipboard")
	st, err := NewStore(dir, newFakeBlobs(), nil, nil)
	if err != nil {
		t.Fatalf("NewStore(恒等默认): %v", err)
	}
	const plain = "中文口令与符号 ✓ 往返"
	e := entryText("id-d", plain)
	e.Preview = "口令摘要"
	if _, _, err := st.Add(e, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "index.json"))
	if !strings.Contains(string(raw), plain) {
		t.Fatal("v1.6 明文裁决失守：盘上找不到正文（默认缝被擅自加了密？）")
	}
	reopened, err := NewStore(dir, newFakeBlobs(), nil, nil)
	if err != nil {
		t.Fatalf("恒等重开: %v", err)
	}
	got, ok := reopened.Get("id-d")
	if !ok || got.Text != plain {
		t.Fatalf("DPAPI 往返不符: %+v ok=%v", got, ok)
	}
}

// TestClipStoreDedupTop 同 hash 重复：顶置 + useCount++/lastUsedAt 刷新，
// 不新增条目，pinned 保留、ID 维持原条。
func TestClipStoreDedupTop(t *testing.T) {
	st, _, _ := newTestStore(t)
	now := time.Now()
	if _, _, err := st.Add(entryText("id-a", "alpha"), now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Add(entryText("id-b", "beta"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TogglePin("id-a"); err != nil {
		t.Fatal(err)
	}
	dup := entryText("id-c-new", "alpha") // 新 ID 同文：应命中去重顶置
	moved, evicted, err := st.Add(dup, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(evicted) != 0 || moved.ID != "id-a" || st.Count() != 2 {
		t.Fatalf("去重形态异常: %+v evicted=%v count=%d", moved, evicted, st.Count())
	}
	if !moved.Pinned || moved.UseCount != 1 || moved.LastUsedAt == 0 {
		t.Fatalf("顶置记账异常: %+v", moved)
	}
	list := st.List("", "", 0)
	if list[0].ID != "id-a" {
		t.Fatalf("顶置未生效: %v", list[0].ID)
	}
}

// TestClipStoreEvictCountAndExemptions 超 500 条 LRU 淘汰（尾部先裁），
// pinned 与 manual 豁免；全库钉选时允许超限如实保留。
func TestClipStoreEvictCountAndExemptions(t *testing.T) {
	st, _, _ := newTestStore(t)
	now := time.Now()

	anchor, _, err := st.Add(entryText("anchor", "锚定条目"), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.TogglePin(anchor.ID); err != nil {
		t.Fatal(err)
	}
	manual := entryText("manual", "固定片段")
	manual.Manual = true
	if _, _, err := st.Add(manual, now); err != nil {
		t.Fatal(err)
	}

	var totalEvicted int
	for i := range maxEntries + 3 { // 再灌 503 条
		_, evicted, err := st.Add(entryText(fmt.Sprintf("fill-%03d", i), fmt.Sprintf("fill %03d", i)), now)
		if err != nil {
			t.Fatal(err)
		}
		totalEvicted += len(evicted)
	}
	if st.Count() != maxEntries {
		t.Fatalf("条数钳制失败: %d", st.Count())
	}
	if _, ok := st.Get("anchor"); !ok {
		t.Fatal("pinned 条目被误淘汰")
	}
	if _, ok := st.Get("manual"); !ok {
		t.Fatal("manual 条目被误淘汰")
	}
	if totalEvicted == 0 {
		t.Fatal("超限未产生淘汰清单（removed 事件会漏报）")
	}
	// 最早灌入的 fill-000/001/002 应已被裁（尾部 LRU）
	if _, ok := st.Get("fill-000"); ok {
		t.Fatal("LRU 尾部未按序淘汰")
	}

	// 全库钉选：允许超限不误杀
	allPinned, _, _ := newTestStore(t)
	for i := range maxEntries + 2 {
		e := entryText(fmt.Sprintf("p-%03d", i), fmt.Sprintf("p %03d", i))
		e.Pinned = true
		if _, _, err := allPinned.Add(e, now); err != nil {
			t.Fatal(err)
		}
	}
	if allPinned.Count() != maxEntries+2 {
		t.Fatalf("全钉选应允许超限，得 %d", allPinned.Count())
	}
}

// TestClipStoreBlobBudget blobs 总量超 100MiB：LRU 裁图片并实删 blob，
// pinned 图片豁免。
func TestClipStoreBlobBudget(t *testing.T) {
	st, fb, _ := newTestStore(t)
	fb.size = maxBlobBytes / 2 // 每图 50MiB
	now := time.Now()

	addImg := func(id string) {
		e := Entry{ID: id, Kind: KindImage, CreatedAt: now.UnixMilli()}
		e.Hash = hashOf(KindImage, "", nil, []byte(id))
		rel, w, h, err := fb.SaveImage(testImg)
		if err != nil {
			t.Fatal(err)
		}
		e.Blob = blobRelPrefix + rel
		e.Width, e.Height, e.ByteSize = w, h, fb.size
		if _, _, err := st.Add(e, now); err != nil {
			t.Fatal(err)
		}
	}

	addImg("img-1")
	addImg("img-2") // 100MiB 恰至上限，不触发
	if st.Count() != 2 {
		t.Fatalf("预算未超即不应淘汰: %d", st.Count())
	}
	addImg("img-3") // 150MiB → LRU 裁 img-1
	if st.Count() != 2 {
		t.Fatalf("体积淘汰未生效: %d", st.Count())
	}
	if _, ok := st.Get("img-1"); ok {
		t.Fatal("LRU 图片未被裁")
	}
	if dels := fb.deleteCalls(); len(dels) == 0 || dels[0] == "" {
		t.Fatalf("被淘汰条目的 blob 未实删: %v", dels)
	}
}

// TestClipStoreQuarantineCorruptIndex 坏库隔离：不可解析的 index.json 改名保留
// 取证副本，空库启动且新基线落盘。
func TestClipStoreQuarantineCorruptIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clipboard")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(`{"version":1,"entr`), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if err != nil {
		t.Fatalf("坏库应隔离后空库起步，得错误: %v", err)
	}
	if st.Count() != 0 {
		t.Fatal("隔离后未空库")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "index.json.corrupt-*"))
	if len(matches) != 1 {
		t.Fatalf("取证副本缺失: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatalf("新基线未落盘: %v", err)
	}
}

// TestClipStoreDecryptFailDrop 单条密文不可还原（契约 §12 v1.2.3 逐条通道）：
// 该条跳过+保真隔离原件，好条目照常装载，整库不连坐改名（剔除固化回盘）。
func TestClipStoreDecryptFailDrop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clipboard")
	seed := diskIndex{Version: indexVersion, Entries: []Entry{
		{ID: "bad", Kind: KindText, Text: "not-a-fake-cipher", Hash: "h-bad", ByteSize: 3, CreatedAt: 1},
		{ID: "good", Kind: KindText, Text: mustFakeSeal("hi"), Hash: "h-good", ByteSize: 2, CreatedAt: 2},
	}}
	data, _ := json.Marshal(seed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if err != nil {
		t.Fatalf("逐条容错下不应整库报错: %v", err)
	}
	if st.Count() != 1 {
		t.Fatalf("坏条未隔离: %d", st.Count())
	}
	got, ok := st.Get("good")
	if !ok || got.Text != "hi" {
		t.Fatalf("好条装载异常: %+v ok=%v", got, ok)
	}
	// 原 index 未被连坐改名；坏条原文进了旁路取证副本
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatalf("整库文件被连坐: %v", err)
	}
	qc, _ := filepath.Glob(filepath.Join(dir, "index.json.corrupt-*"))
	if len(qc) != 1 {
		t.Fatalf("坏条取证副本缺失: %v", qc)
	}
	// 外层 json.Marshal 为紧凑形，取证副本须与坏条原始字节逐字一致
	if bq, _ := os.ReadFile(qc[0]); !strings.Contains(string(bq), `"id":"bad"`) {
		t.Fatalf("取证副本未保真坏条原文: %s", bq)
	}
	// 剔除固化：重开不应再出现坏条
	reopened, err := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Count() != 1 {
		t.Fatalf("剔除未固化: %d", reopened.Count())
	}
}

// TestClipStoreBadEntryShapeIsolation 信封合法、单条 JSON 形态坏/未知 Kind：
// 逐条跳过+隔离，其余照常装载（v1.2.3 反例——旧版整档 Unmarshal 会全库连坐）。
func TestClipStoreBadEntryShapeIsolation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clipboard")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := entryText("g-1", "ok")
	g.Text = mustFakeSeal("ok") // 装载侧 text 必须是密文形态
	good, _ := json.Marshal(g)
	// 信封合法、单条类型畸形（kind 数字 + text 对象）：只有逐条延迟解码能接住，
	// 整档 Unmarshal 形态会连坐全库
	seed := `{"version":1,"entries":[` + string(good) + `,{"id":"broken","kind":123,"text":{"oops":true}}]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if err != nil {
		t.Fatal(err)
	}
	if st.Count() != 1 {
		t.Fatalf("坏形态条目未被剔除: %d", st.Count())
	}

	// 未知 Kind 同样逐条隔离（前向未知形态，GUI 装载不硬抗）
	g2 := entryText("g-2", "still good")
	g2.Text = mustFakeSeal("still good")
	weird := diskIndex{Version: indexVersion, Entries: []Entry{
		{ID: "w-1", Kind: Kind("widget"), Hash: "hw", CreatedAt: 1},
		g2,
	}}
	data, _ := json.Marshal(weird)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	st2, err := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Count() != 1 {
		t.Fatalf("未知 Kind 应被剔除: %d", st2.Count())
	}
	if _, ok := st2.Get("g-2"); !ok {
		t.Fatal("未知 Kind 剔除不得波及好条目")
	}
}

func mustFakeSeal(s string) string {
	v, err := fakeSeal([]byte(s))
	if err != nil {
		panic(err)
	}
	return v
}

// TestClipStoreListFilter q 子串匹配（Preview/来源/Files/Tags 拼接）、kind 过滤、
// limit 钳制与出口剥正文。
func TestClipStoreListFilter(t *testing.T) {
	st, _, _ := newTestStore(t)
	now := time.Now()
	ta := entryText("t-a", "Alpha 备忘")
	ta.SourceApp = "记事本"
	ta.AutoTags = []string{"cjk"}
	tb := Entry{ID: "f-b", Kind: KindFile, Hash: "hf-b", CreatedAt: now.UnixMilli(),
		Preview: previewOf(KindFile, "", []string{`D:\notes\shopping.txt`}, 0, 0),
		Files:   []string{`D:\notes\shopping.txt`}}
	ic := Entry{ID: "i-c", Kind: KindImage, Hash: "hi-c", CreatedAt: now.UnixMilli(), Preview: "图片 4×2"}
	for _, e := range []Entry{ta, tb, ic} {
		if _, _, err := st.Add(e, now); err != nil {
			t.Fatal(err)
		}
	}

	if got := st.List("", "", 0); len(got) != 3 {
		t.Fatalf("全量列表: %d", len(got))
	}
	if got := st.List("SHOPPING", "", 0); len(got) != 1 || got[0].ID != "f-b" {
		t.Fatalf("Files 子串匹配失效: %+v", got)
	}
	if got := st.List("记事本", "", 0); len(got) != 1 || got[0].ID != "t-a" {
		t.Fatalf("SourceApp 匹配失效: %+v", got)
	}
	if got := st.List("", string(KindImage), 0); len(got) != 1 || got[0].ID != "i-c" {
		t.Fatalf("kind 过滤失效: %+v", got)
	}
	if got := st.List("", "all", 0); len(got) != 3 {
		t.Fatalf(`kind="all" 应不过滤: %d`, len(got))
	}
	if got := st.List("", "", 9999); len(got) > maxEntries {
		t.Fatal("limit 未钳制")
	}
	if got := st.List("", "", 2); len(got) != 2 {
		t.Fatalf("limit 截断失效: %d", len(got))
	}
	for _, e := range st.List("", "", 0) {
		if e.Text != "" {
			t.Fatalf("List 出口应剥 Text: %+v", e)
		}
	}
}

// TestClipStoreTouchAndDelete Semantics：Touch 记账落盘；Delete 带 blob 先删
// blob（失败则条目原样），成功则整库写回。
func TestClipStoreTouchAndDelete(t *testing.T) {
	st, fb, dir := newTestStore(t)
	now := time.Now()
	if _, _, err := st.Add(entryText("id-1", "one"), now); err != nil {
		t.Fatal(err)
	}
	touched, err := st.Touch("id-1", now.Add(time.Second))
	if err != nil || touched.UseCount != 1 || touched.LastUsedAt == 0 {
		t.Fatalf("Touch: %+v %v", touched, err)
	}
	if _, err := st.Touch("missing", now); err == nil {
		t.Fatal("Touch 缺失 ID 应报错")
	}
	reopened, _ := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if r, _ := reopened.Get("id-1"); r.UseCount != 1 {
		t.Fatal("Touch 未落盘")
	}

	// blob 条目删除：先删 blob 成功才动账本
	rel, _, _, err := fb.SaveImage(testImg)
	if err != nil {
		t.Fatal(err)
	}
	imgEntry := Entry{ID: "img-x", Kind: KindImage, Hash: "hx", Blob: blobRelPrefix + rel, CreatedAt: now.UnixMilli()}
	if _, _, err := st.Add(imgEntry, now); err != nil {
		t.Fatal(err)
	}
	fb.deleteErr = fmt.Errorf("mock blob 删除失败")
	if _, err := st.Delete("img-x"); err == nil {
		t.Fatal("blob 删除失败时 Delete 应报错")
	}
	if _, ok := st.Get("img-x"); !ok {
		t.Fatal("blob 删除失败不应删条目（内存与盘不分叉）")
	}
	fb.deleteErr = nil
	if _, err := st.Delete("img-x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Get("img-x"); ok {
		t.Fatal("Delete 未移除条目")
	}
	if _, err := st.Delete("missing"); err == nil {
		t.Fatal("Delete 缺失 ID 应报错")
	}
}

// TestClipStoreClearAll 全清：条目归零、blobs 目录清空重建（含孤儿文件一并销毁）。
func TestClipStoreClearAll(t *testing.T) {
	st, fb, dir := newTestStore(t)
	now := time.Now()
	if _, _, err := st.Add(entryText("c-1", "one"), now); err != nil {
		t.Fatal(err)
	}
	rel, _, _, _ := fb.SaveImage(testImg)
	_ = os.WriteFile(filepath.Join(dir, "blobs", rel), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "blobs", "orphan.png"), []byte("x"), 0o644)

	if err := st.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if st.Count() != 0 {
		t.Fatal("全清后条目未归零")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatalf("blobs 目录应重建为空: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("blobs 残留: %v", entries)
	}
	reopened, _ := NewStore(dir, newFakeBlobs(), fakeSeal, fakeOpen)
	if reopened.Count() != 0 {
		t.Fatal("全清未落盘")
	}
}

// TestClipStoreCountAndBlobBytes Status 数据源透传（含 blob 读失败如实上浮）。
func TestClipStoreCountAndBlobBytes(t *testing.T) {
	st, fb, _ := newTestStore(t)
	if n := st.Count(); n != 0 {
		t.Fatalf("初始计数: %d", n)
	}
	if b, err := st.BlobBytes(); err != nil || b != 0 {
		t.Fatalf("初始总量: %d %v", b, err)
	}
	fb.totalErr = fmt.Errorf("mock 总量不可读")
	if _, err := st.BlobBytes(); err == nil {
		t.Fatal("BlobBytes 应透传错误")
	}
}
