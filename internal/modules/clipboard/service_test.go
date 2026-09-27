package clipboard

import (
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/extapi"
)

// ---------- 测试装配 ----------

type evRec struct {
	name    string
	payload any
}

type svcHarness struct {
	svc    *ClipboardService
	blobs  *fakeBlobIO
	writes []string // writeText 缝录制
	events []evRec  // eventSink 缝录制
	dir    string
}

// newSvcHarness 假 store（假加解密链）+ 假事件面 + 假系统写手/解码器的服务体。
// 不接 Wails、不起监听（无头态），handleCapture 直调即走完整入库链。
func newSvcHarness(t *testing.T) *svcHarness {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clipboard")
	fb := newFakeBlobs()
	store, err := NewStore(dir, fb, fakeSeal, fakeOpen)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	h := &svcHarness{blobs: fb, dir: dir}
	h.svc = &ClipboardService{
		store:     store,
		holder:    extapi.NewLeaseHolder(ID),
		writeText: func(text string) error { h.writes = append(h.writes, text); return nil },
		decodeDIB: func(dib []byte) (image.Image, error) { return testImg, nil },
		eventSink: func(name string, payload any) { h.events = append(h.events, evRec{name, payload}) },
	}
	if err := h.svc.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	return h
}

func (h *svcHarness) stop() { _ = h.svc.stop() }

func (h *svcHarness) eventNames() []string {
	var out []string
	for _, e := range h.events {
		out = append(out, e.name)
	}
	return out
}

func (h *svcHarness) countEvents(name string) int {
	n := 0
	for _, e := range h.events {
		if e.name == name {
			n++
		}
	}
	return n
}

func textCap(text string) capture {
	return capture{kind: KindText, text: text, sourceApp: "单测窗口", sourceExe: "test.exe", at: time.Now()}
}

func (h *svcHarness) updatedPayload(t *testing.T) Entry {
	t.Helper()
	last := h.events[len(h.events)-1]
	if last.name != evUpdated {
		t.Fatalf("最后事件应为 %s，得 %s", evUpdated, last.name)
	}
	e, ok := last.payload.(Entry)
	if !ok {
		t.Fatalf("updated 载荷应为 Entry，得 %T", last.payload)
	}
	return e
}

// ---------- 测试 ----------

// TestClipServiceHeadlessLifecycle 无头 start/stop：监听不启动、热键不绑、
// 全程不 panic，重复起停幂等。
func TestClipServiceHeadlessLifecycle(t *testing.T) {
	h := newSvcHarness(t)
	if h.svc.listener != nil {
		t.Fatal("无头态不应启动监听")
	}
	if err := h.svc.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := h.svc.start(); err != nil {
		t.Fatalf("二次 start: %v", err)
	}
	if err := h.svc.stop(); err != nil {
		t.Fatalf("二次 stop: %v", err)
	}
	// stop 后捕获静默早退（started=false），不入库
	h.svc.handleCapture(textCap("停机期间复制"))
	if n := h.svc.store.Count(); n != 0 {
		t.Fatalf("stop 后不应入库: %d", n)
	}
}

// TestClipServiceTextFlow 文本捕获→List/Get 全链：事件剥正文、Get 带明文并
// 记使用、重复复制顶置计数。
func TestClipServiceTextFlow(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.handleCapture(textCap("hello 世界"))
	list, err := h.svc.List("", "", 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v %+v", err, list)
	}
	if list[0].Text != "" {
		t.Fatal("List 不应携带 Text")
	}
	if list[0].Preview != "hello 世界" || list[0].SourceApp != "单测窗口" || list[0].SourceExe != "test.exe" {
		t.Fatalf("条目装配异常: %+v", list[0])
	}
	upd := h.updatedPayload(t)
	if upd.Text != "" {
		t.Fatal("updated 事件不应携带 Text")
	}

	full, err := h.svc.Get(list[0].ID)
	if err != nil || full.Text != "hello 世界" {
		t.Fatalf("Get: %+v %v", full, err)
	}
	if full.UseCount != 1 || full.LastUsedAt == 0 {
		t.Fatalf("Get 应记使用: %+v", full)
	}
	if _, err := h.svc.Get(list[0].ID); err != nil {
		t.Fatal(err)
	}
	h.svc.handleCapture(textCap("hello 世界")) // 重复复制：顶置计数不新增
	again, _ := h.svc.List("", "", 0)
	if len(again) != 1 {
		t.Fatalf("去重失效: %d", len(again))
	}
	// A4 增量顶置依赖：去重顶置同样必须发 clipboard:updated（§5 两态一事件）
	if n := h.countEvents(evUpdated); n != 2 { // 首采 1 + 去重顶置 1（Get/Set 不发）
		t.Fatalf("updated 事件数异常: %d（事件序列 %v）", n, h.eventNames())
	}
	if dup := h.updatedPayload(t); dup.ID != again[0].ID || dup.Text != "" || dup.Preview != "hello 世界" {
		t.Fatalf("去重顶置的 updated 载荷异常: %+v", dup)
	}
	if again[0].UseCount != 3 { // 1(Get)+1(Get)+1(重复制)
		t.Fatalf("使用计数异常: %+v", again[0])
	}
	if _, err := h.svc.Get("missing-id"); err == nil {
		t.Fatal("Get 缺失 ID 应报错")
	}
}

// TestClipServiceIngestRules 入库口三条规则：排除表不入库、启发式命中入库置
// sensitive、全空白文本丢弃。
func TestClipServiceIngestRules(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	c := textCap("ghp_" + strings.Repeat("Ab1", 7) + "token")
	c.sourceExe = "1password.exe"
	h.svc.handleCapture(c)
	if n := h.svc.store.Count(); n != 0 || len(h.events) != 0 {
		t.Fatalf("排除表命中不应入库/发事件: %d %v", n, h.eventNames())
	}

	secret := "sk-" + strings.Repeat("x1Y2", 5)
	h.svc.handleCapture(textCap(secret))
	list, _ := h.svc.List("", "", 0)
	if len(list) != 1 || !list[0].Sensitive {
		t.Fatalf("启发式命中应入库且置 sensitive: %+v", list)
	}

	h.svc.handleCapture(textCap("   \n\t "))
	if n := h.svc.store.Count(); n != 1 {
		t.Fatalf("全空白文本不应入库: %d", n)
	}

	// 明文非敏感不误标
	h.svc.handleCapture(textCap("普通备忘"))
	list, _ = h.svc.List("", "", 0)
	if len(list) != 2 {
		t.Fatalf("普通文本入库失败: %d", len(list))
	}
	var secretE, plainE *Entry
	for i := range list {
		if list[i].Sensitive {
			secretE = &list[i]
		} else {
			plainE = &list[i]
		}
	}
	if secretE == nil || plainE == nil {
		t.Fatalf("敏感/普通二分失效: %+v", list)
	}
	if secretE.Preview != "sk-"+strings.Repeat("x1Y2", 5) {
		t.Fatalf("敏感条目识别错位: %+v", *secretE)
	}

	// v1.4.2 尺寸闸收尾：>8MiB 拒入库（warn 如实）；恰 8MiB 边界放行
	before := h.svc.store.Count()
	h.svc.handleCapture(textCap(strings.Repeat("x", int(maxTextBytes)+1)))
	if n := h.svc.store.Count(); n != before {
		t.Fatalf("超限文本不应入库: %d→%d", before, n)
	}
	h.svc.handleCapture(textCap(strings.Repeat("y", int(maxTextBytes))))
	if n := h.svc.store.Count(); n != before+1 {
		t.Fatalf("8MiB 恰界文本应入库: %d→%d", before, n)
	}
}

// TestClipServiceExcludedLiveTable 排除表活表（v1.3.4）：初始=出厂全量、
// 活表增删即时影响入库判定，GetStatus 回显与判定同源。
func TestClipServiceExcludedLiveTable(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	c := textCap("从保险库复制的口令")
	c.sourceExe = "bitwarden.exe" // 出厂默认表第 2 项，裁撤即回归事故
	h.svc.handleCapture(c)
	if n := h.svc.store.Count(); n != 0 {
		t.Fatalf("默认表未兜住密码管理器: %d", n)
	}

	h.svc.mu.Lock()
	h.svc.excluded = append(h.svc.excluded, "vault-app.exe")
	h.svc.mu.Unlock()
	c2 := textCap("另一个口令")
	c2.sourceExe = "vault-app.exe"
	h.svc.handleCapture(c2)
	if n := h.svc.store.Count(); n != 0 {
		t.Fatalf("活表新增未生效: %d", n)
	}
	st, err := h.svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	var sawVault bool
	for _, e := range st.ExcludedExes {
		if e == "vault-app.exe" {
			sawVault = true
		}
	}
	if !sawVault {
		t.Fatalf("GetStatus 未回显活表: %v", st.ExcludedExes)
	}
}

// TestClipServiceImagePipeline 图片链：解码→落 blob→元数据入库；重复 DIB 只
// 记账不重复落盘；>16MiB 与解码失败均跳过。
func TestClipServiceImagePipeline(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	dib := []byte("DIB-PAYLOAD-1")
	h.svc.handleCapture(capture{kind: KindImage, dib: dib, sourceApp: "画图", at: time.Now()})
	list, _ := h.svc.List("", "", 0)
	if len(list) != 1 {
		t.Fatalf("图片未入库: %d", len(list))
	}
	e := list[0]
	if e.Kind != KindImage || e.Width != 4 || e.Height != 2 || e.Blob != "blobs/fake-000.png" {
		t.Fatalf("图片元数据异常: %+v", e)
	}
	if e.ByteSize != int64(len("PNGFAKE")) {
		t.Fatalf("ByteSize 应为 PNG 字节数: %d", e.ByteSize)
	}
	full, err := h.svc.Get(e.ID)
	if err != nil || string(full.BlobData) != "PNGFAKE" {
		t.Fatalf("Get 应带 BlobData: %+v %v", full, err)
	}

	h.svc.handleCapture(capture{kind: KindImage, dib: dib, at: time.Now()})
	if h.blobs.saves != 1 {
		t.Fatalf("重复 DIB 应命中预查不重复落盘: saves=%d", h.blobs.saves)
	}
	if n, _ := h.svc.List("", "", 0); len(n) != 1 {
		t.Fatal("重复图片不应新增条目")
	}

	// >16MiB：跳过不入库，且连解码/落盘都不触发（只留 warn）
	savesBefore := h.blobs.saves
	h.svc.handleCapture(capture{kind: KindImage, dib: make([]byte, maxImageBytes+1), at: time.Now()})
	list, _ = h.svc.List("", "image", 0)
	if len(list) != 1 {
		t.Fatalf("16MiB 超限图应被跳过: %+v", list)
	}
	if h.blobs.saves != savesBefore {
		t.Fatal("超限图不应触发解码/落盘")
	}

	// 解码失败：不入库
	h.svc.decodeDIB = func([]byte) (image.Image, error) { return nil, fmt.Errorf("mock 解码失败") }
	h.svc.handleCapture(capture{kind: KindImage, dib: []byte("undecodable"), at: time.Now()})
	list, _ = h.svc.List("", "image", 0)
	if len(list) != 1 {
		t.Fatal("解码失败图片不应入库")
	}

	// 恢复正常解码：新图入库
	h.svc.decodeDIB = func([]byte) (image.Image, error) { return testImg, nil }
	h.svc.handleCapture(capture{kind: KindImage, dib: []byte("DIB-PAYLOAD-2"), at: time.Now()})
	list, _ = h.svc.List("", "image", 0)
	if len(list) != 2 || h.blobs.saves != savesBefore+1 {
		t.Fatalf("正常新图入库异常: %+v saves=%d", list, h.blobs.saves)
	}
}

// TestClipServiceFilesFlow CF_HDROP 捕获：files 上 wire、hash 以 \n join 去重。
func TestClipServiceFilesFlow(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	files := []string{`D:\a.txt`, `D:\b.txt`}
	h.svc.handleCapture(capture{kind: KindFile, files: files, at: time.Now()})
	list, _ := h.svc.List("a.txt", "", 0)
	if len(list) != 1 || list[0].Kind != KindFile || len(list[0].Files) != 2 {
		t.Fatalf("文件条目链路异常: %+v", list)
	}
	h.svc.handleCapture(capture{kind: KindFile, files: files, at: time.Now()})
	list, _ = h.svc.List("", "", 0)
	if len(list) != 1 || list[0].UseCount != 1 {
		t.Fatalf("文件去重顶置失效: %+v", list[0])
	}
}

// TestClipServiceSetBehavior Set 回填：文本走写手；image/file 返回可读的
// "暂不支持回填"错误；缺失 ID 报错。回填不记使用。
func TestClipServiceSetBehavior(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.handleCapture(textCap("要回填的文本"))
	textID := h.mustOne(t).ID
	if err := h.svc.Set(textID); err != nil {
		t.Fatalf("Set 文本: %v", err)
	}
	if len(h.writes) != 1 || h.writes[0] != "要回填的文本" {
		t.Fatalf("写手未按预期调用: %v", h.writes)
	}
	if e, _ := h.svc.List("", "", 0); e[0].UseCount != 0 {
		t.Fatal("Set 不应记使用")
	}

	h.svc.handleCapture(capture{kind: KindImage, dib: []byte("dib-set"), at: time.Now()})
	imgID := h.mustOneByKind(t, KindImage).ID
	err := h.svc.Set(imgID)
	if err == nil || !strings.Contains(err.Error(), "暂不支持") {
		t.Fatalf("图片 Set 应返回可读的暂不支持错误: %v", err)
	}
	h.svc.handleCapture(capture{kind: KindFile, files: []string{"x"}, at: time.Now()})
	fileID := h.mustOneByKind(t, KindFile).ID
	if err := h.svc.Set(fileID); err == nil || !strings.Contains(err.Error(), "暂不支持") {
		t.Fatalf("文件 Set 应返回可读的暂不支持错误: %v", err)
	}
	if err := h.svc.Set("missing"); err == nil {
		t.Fatal("Set 缺失 ID 应报错")
	}
}

// TestClipServiceCreateText 手建片段：manual 不淘汰、空文本报错、同名内容去重。
func TestClipServiceCreateText(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	if _, err := h.svc.CreateText("   "); err == nil {
		t.Fatal("空片段应报错")
	}
	created, err := h.svc.CreateText("固定片段\n第二行")
	if err != nil {
		t.Fatal(err)
	}
	if !created.Manual || created.SourceApp != "hanxi" || created.Sensitive {
		t.Fatalf("手建形态异常: %+v", created)
	}
	if created.Text != "固定片段\n第二行" {
		t.Fatal("CreateText 应回明文")
	}
	again, err := h.svc.CreateText("固定片段\n第二行")
	if err != nil || again.ID != created.ID || !again.Manual {
		t.Fatalf("手建重复应去重顶置: %+v %v", again, err)
	}
	if n := h.svc.store.Count(); n != 1 {
		t.Fatalf("去重后条数: %d", n)
	}
}

// TestClipServicePinDeleteClear 钉选翻转、删除广播 removed、全清按 §5 括注
// 静默（不发逐条 removed）。
func TestClipServicePinDeleteClear(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.handleCapture(textCap("甲"))
	h.svc.handleCapture(textCap("乙"))
	a := h.mustOneByText(t, "甲").ID

	pinned, err := h.svc.TogglePin(a)
	if err != nil || !pinned.Pinned {
		t.Fatalf("TogglePin: %+v %v", pinned, err)
	}
	unpinned, err := h.svc.TogglePin(a)
	if err != nil || unpinned.Pinned {
		t.Fatalf("TogglePin 翻转: %+v %v", unpinned, err)
	}
	if _, err := h.svc.TogglePin("missing"); err == nil {
		t.Fatal("TogglePin 缺失 ID 应报错")
	}

	if err := h.svc.Delete(a); err != nil {
		t.Fatal(err)
	}
	if h.countEvents(evRemoved) != 1 {
		t.Fatalf("Delete 应广播一次 removed: %v", h.eventNames())
	}
	last := h.events[len(h.events)-1]
	if rp, ok := last.payload.(RemovedPayload); !ok || rp.ID != a {
		t.Fatalf("removed 载荷异常: %+v", last.payload)
	}
	if err := h.svc.Delete("missing"); err == nil {
		t.Fatal("Delete 缺失 ID 应报错")
	}

	h.svc.handleCapture(textCap("丙"))
	h.svc.handleCapture(textCap("丁"))
	if err := h.svc.ClearAll(); err != nil {
		t.Fatal(err)
	}
	if n := h.svc.store.Count(); n != 0 {
		t.Fatalf("ClearAll 未清空: %d", n)
	}
	// 契约 §5 括注落地：清空不发逐条 removed（事件风暴对前端无意义）
	if h.countEvents(evRemoved) != 1 {
		t.Fatalf("ClearAll 不应广播逐条 removed: %v", h.eventNames())
	}
}

// TestClipServicePausedFlow SetPaused 双向事件与入库开关。
func TestClipServicePausedFlow(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.handleCapture(textCap("运行中"))
	if err := h.svc.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if h.countEvents(evPaused) != 1 {
		t.Fatal("SetPaused 应广播 paused 事件")
	}
	if pp, ok := h.events[len(h.events)-1].payload.(PausedPayload); !ok || !pp.Paused {
		t.Fatalf("paused 载荷异常: %+v", h.events[len(h.events)-1].payload)
	}
	h.svc.handleCapture(textCap("暂停期间"))
	if n := h.svc.store.Count(); n != 1 {
		t.Fatalf("暂停期不应入库: %d", n)
	}
	st, err := h.svc.GetStatus()
	if err != nil || !st.Paused {
		t.Fatalf("GetStatus.Paused 回显异常: %+v %v", st, err)
	}
	if err := h.svc.SetPaused(false); err != nil {
		t.Fatal(err)
	}
	h.svc.handleCapture(textCap("恢复后"))
	if n := h.svc.store.Count(); n != 2 {
		t.Fatalf("恢复后应继续入库: %d", n)
	}
}

// TestClipServiceGetStatus GetStatus 全字段实况回显。
func TestClipServiceGetStatus(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.handleCapture(capture{kind: KindImage, dib: []byte("st-dib"), at: time.Now()})
	st, err := h.svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.MaxEntries != maxEntries || st.MaxBlobBytes != maxBlobBytes {
		t.Fatalf("钳制上限回显异常: %+v", st)
	}
	if st.EntryCount != 1 || st.BlobBytes != h.blobs.size {
		t.Fatalf("实况异常: %+v", st)
	}
	if len(st.ExcludedExes) != len(DefaultExcludedExes()) {
		t.Fatalf("排除表回显异常: %v", st.ExcludedExes)
	}

	h.blobs.totalErr = fmt.Errorf("mock 总量不可读")
	if _, err := h.svc.GetStatus(); err == nil {
		t.Fatal("GetStatus 应透传 blob 总量错误")
	}
}

// ---------- 断言小件 ----------

func (h *svcHarness) mustOne(t *testing.T) Entry {
	t.Helper()
	list, err := h.svc.List("", "", 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("期望恰一条，得 %+v %v", list, err)
	}
	return list[0]
}

func (h *svcHarness) mustOneByKind(t *testing.T, k Kind) Entry {
	t.Helper()
	list, err := h.svc.List("", string(k), 0)
	if err != nil || len(list) < 1 {
		t.Fatalf("期望至少一条 %s，得 %+v %v", k, list, err)
	}
	return list[0]
}

func (h *svcHarness) mustOneByText(t *testing.T, preview string) Entry {
	t.Helper()
	list, err := h.svc.List("", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.Preview == preview {
			return e
		}
	}
	t.Fatalf("未找到 preview=%q: %+v", preview, list)
	return Entry{}
}
