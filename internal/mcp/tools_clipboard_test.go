package mcp

// tools_clipboard_test.go 钉剪贴板只读工具（hanxi_clipboard_search）的六条硬账：
// 零落盘直读的三档失败语义（真空库/整档 fail-loud/单条跳过不连坐）、敏感条目
// 双层不下发、明文解密面（真 = windows.DPAPIDecrypt，单测 = 假件）的失败降级、
// 过滤组合与条数钳制、出机脱敏与两级截断（单条 64KiB / 整页预算）。
//
// 夹具真/假分工：DPAPI 与本机用户账户绑定，真密文进不了版本库、异机 CI 也解不开，
// 故 testdata/clipboard/index.json 的 Text 存"可解的假密文"（base64("fake-dpapi:"+明文)），
// 由 fakeDPAPIDecrypt 解；真件只是 newClipboardDiskReader 里把 decrypt 字段接成
// windows.DPAPIDecrypt（该构造函数不在测试路径上，避免触发 settings 建目录）。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/server"
)

// ---------- 假解密面（模拟 DPAPI 的"绑机绑账户"语义） ----------

const fakeDPAPIPrefix = "fake-dpapi:"

// fakeDPAPIEncrypt 产夹具态密文（落盘态 = base64(假 DPAPI(UTF-8))）。
func fakeDPAPIEncrypt(plain string) string {
	return base64.StdEncoding.EncodeToString([]byte(fakeDPAPIPrefix + plain))
}

// fakeDPAPIDecrypt 是真件的假件替身：前缀不符即报"非本机加密"，与异机库解不开的
// 真态同构（真 DPAPI 报 CryptUnprotectData 失败，这里报前缀不符）。
func fakeDPAPIDecrypt(cipherB64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return nil, fmt.Errorf("fake dpapi: invalid base64: %w", err)
	}
	if !strings.HasPrefix(string(raw), fakeDPAPIPrefix) {
		return nil, errors.New("fake dpapi: not protected by this machine/account")
	}
	return []byte(strings.TrimPrefix(string(raw), fakeDPAPIPrefix)), nil
}

// ---------- 条目与信封构造件 ----------

func mkClip(id, kind, preview string, created int64) clipboardDiskEntry {
	return clipboardDiskEntry{ID: id, Kind: kind, Preview: preview, CreatedAt: created}
}

// clipEnvelope 工具出参信封（外层 listResult 契约 + 条目字段）。
type clipEnvelope struct {
	Count     int            `json:"count"`
	Truncated bool           `json:"truncated"`
	Results   []clipWireItem `json:"results"`
}

type clipWireItem struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Preview        string   `json:"preview"`
	Text           string   `json:"text"`
	Files          []string `json:"files"`
	AutoTags       []string `json:"autoTags"`
	Pinned         bool     `json:"pinned"`
	SourceApp      string   `json:"sourceApp"`
	CreatedAt      string   `json:"createdAt"`
	Width          int      `json:"width"`
	Height         int      `json:"height"`
	ByteSize       int64    `json:"byteSize"`
	Truncated      bool     `json:"truncated"`
	TextOmitted    bool     `json:"textOmitted"`
	FilesTruncated bool     `json:"filesTruncated"`
	IdentityOnly   bool     `json:"identityOnly"`

	// raw 保留条目原始 JSON，供"某字段键根本不存在"式断言（区别于零值）。
	raw map[string]json.RawMessage
}

func (c *clipWireItem) UnmarshalJSON(data []byte) error {
	type alias clipWireItem
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = clipWireItem(a)
	return json.Unmarshal(data, &c.raw)
}

func (c clipWireItem) has(key string) bool {
	_, ok := c.raw[key]
	return ok
}

// fakeClipboard 是 ClipboardSource 的假件（真 = clipboardDiskReader）。
type fakeClipboard struct {
	items []clipboardDiskEntry
	err   error
	calls int
}

func (f *fakeClipboard) Load() ([]clipboardDiskEntry, error) {
	f.calls++
	return f.items, f.err
}

// clipboardSetup 接线取数面并保证复位（hook 是包级状态，绝不影响包内其他测试）。
func clipboardSetup(t *testing.T, src ClipboardSource) {
	t.Helper()
	SetClipboardSource(src)
	t.Cleanup(func() { SetClipboardSource(nil) })
}

// clipboardOK 调一次工具并解出信封；出错即 fail。
func clipboardOK(t *testing.T, args map[string]any) (clipEnvelope, string) {
	t.Helper()
	res, text := callHandler(t, buildClipboardSearchTool, args)
	if res.IsError {
		t.Fatalf("unexpected clipboard error: %s", text)
	}
	var env clipEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return env, text
}

func clipErr(t *testing.T, args map[string]any) string {
	t.Helper()
	res, text := callHandler(t, buildClipboardSearchTool, args)
	if !res.IsError {
		t.Fatalf("expected error, got: %s", text)
	}
	return text
}

// clipFixture 夹具路径与读取（testdata/clipboard/ 下只放本线的测试数据）。
func clipFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "clipboard", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s missing: %v", path, err)
	}
	return path
}

func mustFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// ---------- 零落盘直读：三档失败语义 ----------

// TestClipboardDiskReaderFixture 逐条容错矩阵：好条目装载并解密、敏感条目不出
// reader、字段类型写坏/无 id/异机密文三类坏条目跳过且不连坐；读全程零落盘。
func TestClipboardDiskReaderFixture(t *testing.T) {
	path := clipFixture(t, "index.json")
	before := mustFileBytes(t, path)
	beforeEntries := dirEntries(t, filepath.Dir(path))

	r := &clipboardDiskReader{path: path, decrypt: fakeDPAPIDecrypt}
	items, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]clipboardDiskEntry{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, want := range []string{
		"clip_1000000000000001", "clip_1000000000000002", "clip_1000000000000003",
		"clip_1000000000000004", "clip_1000000000000005", "clip_1000000000000006",
		"clip_future_kind",
	} {
		if _, ok := byID[want]; !ok {
			t.Errorf("valid entry %s must load, got ids: %v", want, idsOf(items))
		}
	}
	// 明文解密：text 类出 reader 即明文，非 text 类不经解密面。
	if got := byID["clip_1000000000000001"].Text; got != "git push origin dev" {
		t.Errorf("text not decrypted: %q", got)
	}
	// 三类坏条目：字段类型写坏 / 无 id / 异机密文，一律缺席（其余照常，不连坐）。
	for _, banned := range []string{"clip_badfield", "clip_foreign_cipher"} {
		if _, ok := byID[banned]; ok {
			t.Errorf("broken entry %s must be skipped", banned)
		}
	}
	for _, it := range items {
		if strings.TrimSpace(it.ID) == "" {
			t.Error("entry without id must be skipped")
		}
		if it.Sensitive {
			t.Errorf("sensitive entry %s must not leave the reader（密文不出 reader）", it.ID)
		}
	}
	// 未来未知 kind 降级保留（不因契约外的取值丢条目）。
	if fut, ok := byID["clip_future_kind"]; !ok || fut.Kind != "sticky" {
		t.Errorf("unknown kind entry must survive as-is: %+v", fut)
	}
	// 零落盘：文件字节与目录清单都不得因读取而变化（不迁移、不隔离、不建文件）。
	if after := mustFileBytes(t, path); !bytes.Equal(before, after) {
		t.Error("reader must not rewrite index.json")
	}
	if after := dirEntries(t, filepath.Dir(path)); fmt.Sprint(beforeEntries) != fmt.Sprint(after) {
		t.Errorf("reader must not write into the clipboard dir: before=%v after=%v", beforeEntries, after)
	}
}

func idsOf(items []clipboardDiskEntry) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// TestClipboardDiskReaderMissingLibraryIsVoid 文件/目录不存在 = 合法真空库：
// 空集 + 无错误，且不创建任何东西（剪贴板模块从未启用过的新机形态）。
func TestClipboardDiskReaderMissingLibraryIsVoid(t *testing.T) {
	root := t.TempDir()
	path := clipboardIndexPath(filepath.Join(root, "not-yet"))
	r := &clipboardDiskReader{path: path, decrypt: fakeDPAPIDecrypt}
	items, err := r.Load()
	if err != nil {
		t.Fatalf("missing library is a legal void state, got err: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("missing library must load empty, got %v", idsOf(items))
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("reader must not create the clipboard dir (err=%v)", err)
	}
	// 空库文件（entries 缺失/为空）同为合法态。
	empty := filepath.Join(root, "empty", "index.json")
	if err := os.MkdirAll(filepath.Dir(empty), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, []byte(`{"version":1,"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (&clipboardDiskReader{path: empty, decrypt: fakeDPAPIDecrypt}).Load()
	if err != nil || len(got) != 0 {
		t.Fatalf("empty index must load as void: items=%v err=%v", got, err)
	}
}

// TestClipboardDiskReaderFailLoud 整档损坏与 version 非 1：报错指引且原档零改动
// （修复/重建是 GUI 通道的职责，无头只报不修，同 memo 旧库口径）。
func TestClipboardDiskReaderFailLoud(t *testing.T) {
	root := t.TempDir()
	corrupt := filepath.Join(root, "index.json")
	if err := os.WriteFile(corrupt, []byte(`{"version":1,"entries":[`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &clipboardDiskReader{path: corrupt, decrypt: fakeDPAPIDecrypt}
	if _, err := r.Load(); err == nil {
		t.Fatal("corrupt index must fail loud")
	} else if !strings.Contains(err.Error(), "hanxi 主程序") {
		t.Errorf("error must guide to GUI repair: %v", err)
	}
	if !bytes.Equal(mustFileBytes(t, corrupt), []byte(`{"version":1,"entries":[`)) {
		t.Error("corrupt index must stay untouched")
	}

	ver := clipFixture(t, "version-unsupported.json")
	_, err := (&clipboardDiskReader{path: ver, decrypt: fakeDPAPIDecrypt}).Load()
	if err == nil || !strings.Contains(err.Error(), "version=2") {
		t.Fatalf("unsupported version must fail loud with the actual number, got: %v", err)
	}
}

// TestClipboardDiskReaderNoDecryptorWired 未注入解密面（构造缺件的防御态）不得
// panic：text 条目按解密失败同样降级跳过，元数据类条目照常装载。
func TestClipboardDiskReaderNoDecryptorWired(t *testing.T) {
	path := clipFixture(t, "index.json")
	items, err := (&clipboardDiskReader{path: path}).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind == clipKindText {
			t.Errorf("text item %s must be skipped without a decryptor, not panic/leak ciphertext", it.ID)
		}
	}
	if len(items) == 0 {
		t.Error("non-text items must still load without a decryptor")
	}
}

// ---------- 工具面：过滤组合与条数钳制 ----------

// TestClipboardSearchOverFixture 真磁盘件（reader）+ 真工具面全链：排序、kind/tag/
// keyword 过滤、limit 钳制与 truncated 表达，以及夹具内坏条目/敏感条目的缺席。
func TestClipboardSearchOverFixture(t *testing.T) {
	clipboardSetup(t, &clipboardDiskReader{path: clipFixture(t, "index.json"), decrypt: fakeDPAPIDecrypt})

	env, raw := clipboardOK(t, map[string]any{})
	if env.Count != 7 {
		t.Fatalf("fixture must yield 7 usable items (敏感/坏条已剔): %v / %s", ids(env), raw)
	}
	// createdAt 倒序，同毫秒按 id 升序定序（0005/0006 同刻即该断言的锚点）。
	wantOrder := []string{
		"clip_future_kind", "clip_1000000000000001", "clip_1000000000000002",
		"clip_1000000000000003", "clip_1000000000000004", "clip_1000000000000005",
		"clip_1000000000000006",
	}
	if got := ids(env); strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("order must be createdAt desc then id asc: %v", got)
	}

	// kind 归一：大小写/空白/复数写法与 all/留空同义。
	for _, arg := range []any{"image", " IMAGE ", "image "} {
		env, _ = clipboardOK(t, map[string]any{"kind": arg})
		if env.Count != 1 || env.Results[0].ID != "clip_1000000000000003" {
			t.Fatalf("kind=%q filter wrong: %v", arg, ids(env))
		}
	}
	env, _ = clipboardOK(t, map[string]any{"kind": "files"})
	if env.Count != 1 || env.Results[0].ID != "clip_1000000000000004" {
		t.Fatalf("files must be a synonym of file: %v", ids(env))
	}
	env, _ = clipboardOK(t, map[string]any{"kind": "text"})
	if got := strings.Join(ids(env), ","); got !=
		"clip_1000000000000001,clip_1000000000000002,clip_1000000000000005,clip_1000000000000006" {
		t.Fatalf("kind=text wrong: %v", got)
	}
	env, _ = clipboardOK(t, map[string]any{"kind": "all"})
	if env.Count != 7 {
		t.Fatalf("kind=all must not filter: %v", ids(env))
	}
	// 未知 kind 取值 fail-loud（静默忽略比报错更糟：模型会误信已过滤）。
	text := clipErr(t, map[string]any{"kind": "video"})
	if !strings.Contains(text, "kind 参数") || !strings.Contains(text, "text / image / file / all") {
		t.Errorf("bad kind must guide, got: %s", text)
	}

	// tag：精确、容忍 # 前缀与大小写。
	env, _ = clipboardOK(t, map[string]any{"tag": "#URL"})
	if env.Count != 1 || env.Results[0].ID != "clip_1000000000000005" {
		t.Fatalf("tag filter wrong: %v", ids(env))
	}
	env, _ = clipboardOK(t, map[string]any{"tag": "code"})
	if env.Count != 1 || env.Results[0].ID != "clip_1000000000000001" {
		t.Fatalf("tag=code filter wrong: %v", ids(env))
	}

	// keyword 命中面：正文/来源进程名/文件路径都能命中（来源进程是"从哪复制的"式找回）。
	env, _ = clipboardOK(t, map[string]any{"keyword": "git push"})
	if env.Count != 1 || env.Results[0].ID != "clip_1000000000000001" {
		t.Fatalf("keyword on text wrong: %v", ids(env))
	}
	env, _ = clipboardOK(t, map[string]any{"keyword": "CHROME"})
	if env.Count != 1 || env.Results[0].ID != "clip_1000000000000005" {
		t.Fatalf("keyword must match sourceExe (and prove sensitive chrome item dropped): %v", ids(env))
	}
	env, _ = clipboardOK(t, map[string]any{"keyword": "go.mod"})
	if env.Count != 1 || env.Results[0].ID != "clip_1000000000000004" {
		t.Fatalf("keyword must match file paths: %v", ids(env))
	}
	// 条件取交集。
	env, _ = clipboardOK(t, map[string]any{"kind": "text", "tag": "email", "keyword": "收件"})
	if env.Count != 1 || env.Results[0].ID != "clip_1000000000000006" {
		t.Fatalf("filters must intersect: %v", ids(env))
	}
	env, _ = clipboardOK(t, map[string]any{"kind": "text", "tag": "email", "keyword": "不存在的词"})
	if env.Count != 0 || env.Truncated {
		t.Fatalf("no-hit must be an honest empty list: %+v", env)
	}

	// limit 钳制与 truncated：命中数被上限切掉时必须表达"大概率还有更多"。
	env, _ = clipboardOK(t, map[string]any{"limit": 2.0})
	if env.Count != 2 || !env.Truncated || env.Results[0].ID != "clip_future_kind" {
		t.Fatalf("limit cap must report truncated: %+v", env)
	}
}

func ids(env clipEnvelope) []string {
	out := make([]string, 0, len(env.Results))
	for _, it := range env.Results {
		out = append(out, it.ID)
	}
	return out
}

// TestClipboardLimitClamp limit 越界不报错而是收敛：<1 回落默认 50（不给"0 就要 200 条"
// 的反直觉放大），>200 钳到契约上限 200。
func TestClipboardLimitClamp(t *testing.T) {
	var items []clipboardDiskEntry
	for i := 0; i < 260; i++ {
		items = append(items, mkClip(fmt.Sprintf("clip_%06d", i), clipKindImage, "图", int64(1790000000000-i)))
	}
	clipboardSetup(t, &fakeClipboard{items: items})

	env, _ := clipboardOK(t, map[string]any{"limit": 0.0})
	if env.Count != 50 || !env.Truncated {
		t.Fatalf("limit<1 must fall back to default 50: %+v", env)
	}
	env, _ = clipboardOK(t, map[string]any{"limit": -7.0})
	if env.Count != 50 {
		t.Fatalf("negative limit must fall back to default 50: %+v", env)
	}
	env, _ = clipboardOK(t, map[string]any{"limit": 99999.0})
	if env.Count != maxClipboardLimit || !env.Truncated {
		t.Fatalf("limit>200 must clamp to 200: count=%d", env.Count)
	}
	env, _ = clipboardOK(t, map[string]any{"limit": 3.0})
	if env.Count != 3 || !env.Truncated {
		t.Fatalf("in-range limit must be honored: %+v", env)
	}
	// 命中数不超 limit 时不得虚报 truncated。
	env, _ = clipboardOK(t, map[string]any{"limit": 200.0, "keyword": "不存在的词"})
	if env.Count != 0 || env.Truncated {
		t.Fatalf("empty result must not claim truncated: %+v", env)
	}
}

// TestClipboardKeywordScopes 关键词命中面的正/反向：只命中正文明文也算命中
// （MCP 面比 GUI 的 List 多覆盖正文，见 buildClipboardSearchTool 注记），
// 非 text 类的 Text 不参与匹配（下发不了正文，命中即假阳性）。
func TestClipboardKeywordScopes(t *testing.T) {
	onlyBody := mkClip("clip_body", clipKindText, "标题里没有这个词", 1790000001000)
	onlyBody.Text = "正文里藏着 needleword 关键字"
	imageWithText := mkClip("clip_img", clipKindImage, "截图", 1790000002000)
	imageWithText.Text = "needleword 不该被 image 命中"
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{onlyBody, imageWithText}})

	env, _ := clipboardOK(t, map[string]any{"keyword": "needleword"})
	if env.Count != 1 || env.Results[0].ID != "clip_body" {
		t.Fatalf("keyword must hit text body only: %v", ids(env))
	}
	env, _ = clipboardOK(t, map[string]any{"keyword": "  NEEDLEWORD  "})
	if env.Count != 1 {
		t.Fatalf("keyword must tolerate case and surrounding spaces: %v", ids(env))
	}
}

// ---------- 敏感与遮罩口径 ----------

// TestClipboardSensitiveNeverLeaked 契约 §8 泄露回归锚点：sensitive 条目的 id/摘要/
// 正文不得以任何形态出现在模型可见输出中，也不计入命中数（连"过滤了几条"都不外泄）。
func TestClipboardSensitiveNeverLeaked(t *testing.T) {
	normal := mkClip("clip_public", clipKindText, "公开条目 keyword=hit", 1790000001000)
	normal.Text = "公开条目 keyword=hit"
	secret := mkClip("clip_secret", clipKindText, "敏感条目 keyword=hit", 1790000009000)
	secret.Text = "sk-SUPERSECRETVALUE0123456789 keyword=hit"
	secret.Sensitive = true
	secret.Pinned = true
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{normal, secret}})

	env, raw := clipboardOK(t, map[string]any{"keyword": "keyword=hit"})
	for _, banned := range []string{"clip_secret", "敏感条目", "SUPERSECRET", "maskedCount", "sensitive"} {
		if strings.Contains(raw, banned) {
			t.Errorf("sensitive item leaked via %q: %s", banned, raw)
		}
	}
	if env.Count != 1 || env.Results[0].ID != "clip_public" {
		t.Fatalf("only the non-sensitive item may appear: %+v", env)
	}
	// 全库仅敏感条目：命中查询返回真空列表（可发现性靠 description 声明政策）。
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{secret}})
	env, raw = clipboardOK(t, map[string]any{})
	if env.Count != 0 || strings.Contains(raw, "clip_secret") {
		t.Fatalf("sensitive-only library must look empty: %s", raw)
	}
	// 工具层不信任任何实现的 Load：假件把 sensitive 条目端上来也照样被挡
	// （reader 层的密文不出 reader 是另一道，见 TestClipboardDiskReaderFixture）。
}

// TestClipboardImageMetadataOnly 图片只给元数据：无正文键、无 blob 路径（指向机主
// 磁盘的文件名对模型无用，且属白增泄露面）。
func TestClipboardImageMetadataOnly(t *testing.T) {
	img := mkClip("clip_img", clipKindImage, "截图 800x600", 1790000001000)
	img.Width, img.Height, img.ByteSize = 800, 600, 40960
	img.Blob = "blobs/deadbeefdeadbeef.png"
	img.Text = "异态：图片条目带了正文字段"
	f := mkClip("clip_file", clipKindFile, "3 个文件", 1790000002000)
	f.Files = []string{`C:\Temp\a.txt`, `C:\Temp\b.txt`}
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{img, f}})

	env, raw := clipboardOK(t, map[string]any{})
	if env.Count != 2 {
		t.Fatalf("both items must list: %+v", env)
	}
	image, file := env.Results[1], env.Results[0] // createdAt 倒序：file 在前
	if image.has("text") || image.has("files") {
		t.Errorf("image item must carry metadata only: %s", raw)
	}
	if image.Width != 800 || image.Height != 600 || image.ByteSize != 40960 {
		t.Fatalf("image metadata missing: %+v", image)
	}
	if strings.Contains(raw, "deadbeef") || strings.Contains(raw, "blobs/") {
		t.Errorf("blob path must not be emitted: %s", raw)
	}
	if len(file.Files) != 2 || file.has("text") {
		t.Fatalf("file item must list paths without body: %+v", file)
	}
	if !file.has("files") || file.Pinned {
		t.Errorf("pinned must be an explicit bool (zero value ≠ 缺字段): %+v", file.raw)
	}
}

// ---------- 截断与脱敏 ----------

// TestClipboardTextTruncation 单条正文 >64KiB 在 rune 边界截断并置 truncated；
// 未超限的正文不得被砍。
func TestClipboardTextTruncation(t *testing.T) {
	big := mkClip("clip_big", clipKindText, "长文本", 1790000002000)
	big.Text = strings.Repeat("字", 40000) // 120000 字节，非 3 的整除边界
	small := mkClip("clip_small", clipKindText, "短文本", 1790000001000)
	small.Text = "正好在预算内的一行"
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{big, small}})

	env, _ := clipboardOK(t, map[string]any{"limit": 1.0})
	if !env.Results[0].Truncated {
		t.Fatal("oversized text must set truncated")
	}
	if len(env.Results[0].Text) > maxClipboardItemText {
		t.Fatalf("text exceeds 64KiB cap: %d bytes", len(env.Results[0].Text))
	}
	if !utf8.ValidString(env.Results[0].Text) {
		t.Error("truncation must not cut a rune in half")
	}
	env, _ = clipboardOK(t, map[string]any{})
	if env.Results[1].Truncated || env.Results[1].Text != small.Text {
		t.Fatalf("in-budget text must pass through intact: %+v", env.Results[1])
	}
}

// TestClipboardPageBudgetCountsSerialized 整页出机预算（M3 改账）：按**脱敏并序列化后**
// 的实际字节计账，正文/摘要/文件路径/元数据同入账；页满时停止追加并如实置信封
// truncated，已下发的条目必带降级标志——"预算耗尽 ≠ 无结果、更 ≠ 空壳"。
func TestClipboardPageBudgetCountsSerialized(t *testing.T) {
	const (
		n       = 10
		bodyLen = 60000 // 单条正文长度：够整页撞预算，又远小于单条 64KiB 上限
	)
	var items []clipboardDiskEntry
	for i := 0; i < n; i++ {
		it := mkClip(fmt.Sprintf("clip_%02d", i), clipKindText, "正文条目", int64(1790000010000-i*1000))
		it.Text = strings.Repeat("x", bodyLen)
		items = append(items, it)
	}
	clipboardSetup(t, &fakeClipboard{items: items})

	env, _ := clipboardOK(t, map[string]any{})
	if env.Count < 1 {
		t.Fatalf("page budget must never blank the result: %+v", env)
	}
	if env.Count < n && !env.Truncated {
		t.Errorf("dropped tail items must be declared by envelope truncated: count=%d", env.Count)
	}
	sum := 0
	for _, it := range env.Results {
		sum += len(it.raw) // 出机实际字节（含键名与转义）
		switch {
		case it.IdentityOnly:
		case it.TextOmitted || it.Truncated:
		default:
			// 未置任何降级标志即视为"整条正文已下发"，长度必须对得上源文。
			if it.Kind == clipKindText && len(it.Text) < bodyLen {
				t.Errorf("text item %s delivered %d/%d bytes but unflagged", it.ID, len(it.Text), bodyLen)
			}
		}
	}
	if sum > maxClipboardPageBytes {
		t.Errorf("serialized page bytes %d exceeds budget %d", sum, maxClipboardPageBytes)
	}
}

// TestClipboardBudgetCountsRedactionInflation 记账必须按脱敏**后**长度（M3 的由来）：
// RedactPII 的赋值替换带 `$1="******"` 净膨胀、正文里的引号经 JSON 转义再膨胀，
// 按脱敏前长度记账会让整页实际字节冲破预算。
func TestClipboardBudgetCountsRedactionInflation(t *testing.T) {
	const n = 9
	var items []clipboardDiskEntry
	for i := 0; i < n; i++ {
		it := mkClip(fmt.Sprintf("clip_inf%02d", i), clipKindText, "含凭据与引号", int64(1790000010000-i*1000))
		// 每条 60000 字节的 token=… 赋值：脱敏后变 token="******"（等长替换），
		// 但同条里塞 15000 字节的引号串——JSON 转义后翻倍，净增 15000 字节/条。
		it.Text = strings.Repeat(`token=x `, 7500) + strings.Repeat(`"`, 15000)
		items = append(items, it)
	}
	clipboardSetup(t, &fakeClipboard{items: items})

	env, _ := clipboardOK(t, map[string]any{})
	sum := 0
	for _, it := range env.Results {
		sum += len(it.raw)
	}
	if sum > maxClipboardPageBytes {
		t.Fatalf("page bytes %d over budget %d：记账漏了转义/替换的净膨胀", sum, maxClipboardPageBytes)
	}
	if env.Count == 0 {
		t.Fatal("must keep delivering items")
	}
	// 出机正文里的凭据值一律不见（顺带盯住"按脱敏后计账"没有把未脱敏文本挤进页）。
	_, raw := clipboardOK(t, map[string]any{})
	if strings.Contains(raw, "token=x") {
		t.Errorf("assignment secret must be scrubbed in every delivered item")
	}
}

// TestClipboardBuildItemBudgetLadder 单条降级阶梯（直调记账函数，钉三档边界）：
// 额度充足给正文 → 只够元数据则正文整块放弃置 textOmitted → 连元数据都放不下退
// 纯身份行 → 身份行也放不下才判定"不可下发"（调用方据此停页）。
func TestClipboardBuildItemBudgetLadder(t *testing.T) {
	e := mkClip("clip_ladder", clipKindText, "摘要", 1790000001000)
	e.Text = strings.Repeat("y", 5000)

	full, size, ok := clipboardBuildItem(e, 1<<20)
	if !ok || size < 5000 || full["text"] == nil || full["textOmitted"] != nil {
		t.Fatalf("ample room must deliver the body: size=%d ok=%v keys=%v", size, ok, keysOf(full))
	}

	// 额度只够元数据（正文起步价 512 字节都放不下）→ 整块放弃并置 textOmitted。
	metaOnly, _, ok := clipboardBuildItem(e, 400)
	if !ok || metaOnly["textOmitted"] != true || metaOnly["text"] != nil {
		t.Fatalf("room for metadata only must drop the body and say so: ok=%v %+v", ok, metaOnly)
	}

	// 装不下整条但够得上起步价的，发**可发的最大正文并置 truncated**（远好过退身份行）；
	// 只有连 512 字节的残段都塞不进（额度≈元数据大小）才逐级降级。
	bigFrag := mkClip("clip_frag", clipKindText, "摘要", 1790000001000)
	bigFrag.Text = strings.Repeat("w", 60<<10)
	frag, size, ok := clipboardBuildItem(bigFrag, 8000)
	if !ok || frag["identityOnly"] != nil || frag["textOmitted"] != nil {
		t.Fatalf("a big body with decent room must ship as a flagged partial body: ok=%v keys=%v", ok, keysOf(frag))
	}
	if frag["truncated"] != true {
		t.Errorf("partial delivery must set truncated: keys=%v", keysOf(frag))
	}
	fragBody, _ := frag["text"].(string)
	if len(fragBody) < floorMinTextAttach || size > 8000 {
		t.Errorf("partial body must respect the floor and the room: body=%d item=%d", len(fragBody), size)
	}

	// 反向锚（本轮修的功能杀手）：正文虽大于起步价但**整条**放得下剩余额度时，
	// 必须原样发出且不带任何降级标志——按"额度-正文"粗估就把好条目判死是回归。
	fullSmall := mkClip("clip_full", clipKindText, "摘要", 1790000001000)
	fullSmall.Text = strings.Repeat("q", 6700)
	got, size, ok := clipboardBuildItem(fullSmall, 8000)
	if !ok || got["identityOnly"] != nil || got["textOmitted"] != nil || got["truncated"] != nil {
		t.Fatalf("complete body within room must ship unflagged: ok=%v size=%d keys=%v", ok, size, keysOf(got))
	}
	if body, _ := got["text"].(string); len(body) != 6700 {
		t.Errorf("body must be delivered intact, got %d bytes", len(body))
	}

	f := mkClip("clip_wide", clipKindFile, "一堆文件", 1790000001000)
	for i := 0; i < 60; i++ {
		f.Files = append(f.Files, `C:\Some\Deeply\Nested\Folder\With\A\Longer\Path\segment-`+fmt.Sprint(i)+`.txt`)
	}
	wide, size, ok := clipboardBuildItem(f, 200)
	if !ok || wide["identityOnly"] != true {
		t.Fatalf("fat metadata beyond room must fall back to the identity row: ok=%v size=%d keys=%v", ok, size, keysOf(wide))
	}
	if _, gotID := wide["id"]; !gotID || wide["kind"] == nil {
		t.Errorf("identity row must keep id/kind/createdAt: %+v", wide)
	}
	if _, got := wide["files"]; got {
		t.Errorf("identity row must not carry fat fields: %+v", wide)
	}

	if _, _, ok := clipboardBuildItem(f, 40); ok {
		t.Error("room smaller than an identity row must report not-fittable (caller stops the page)")
	}
}

// keysOf 载荷键集合（诊断用，输出按字典序保证断言确定）。
func keysOf(item resultPayload) []string {
	keys := make([]string, 0, len(item))
	for k := range item {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestClipboardFilesCap 单条文件路径数出机上限（超限置 filesTruncated）。
func TestClipboardFilesCap(t *testing.T) {
	f := mkClip("clip_files", clipKindFile, "一堆文件", 1790000001000)
	for i := 0; i < maxClipboardItemFiles+50; i++ {
		f.Files = append(f.Files, fmt.Sprintf(`C:\Temp\file-%03d.txt`, i))
	}
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{f}})

	env, _ := clipboardOK(t, map[string]any{})
	if !env.Results[0].FilesTruncated || len(env.Results[0].Files) != maxClipboardItemFiles {
		t.Fatalf("files must be capped with the flag set: %d items", len(env.Results[0].Files))
	}
}

// TestClipboardOutputRedacted 出机前的 PII 档脱敏（契约 §8 点名字段）：赋值型凭据、
// IPv4、邮箱、供应商前缀密钥四类都不得原文外泄，且未命中文本保持可读。
func TestClipboardOutputRedacted(t *testing.T) {
	conn := mkClip("clip_conn", clipKindText, "生产库连接串", 1790000003000)
	conn.Text = "postgres://postgres@192.168.10.7:5432/app password=S3cr3tDumpMe"
	plain := mkClip("clip_plain", clipKindText, "普通文本", 1790000002000)
	plain.Text = "发给 someone@example.com 的收件地址"
	// 机主手工入库/启发式漏网的裸密钥（defense in depth：脱敏层不依赖 sensitive 标记）。
	leaky := mkClip("clip_leaky", clipKindText, "密钥标题 ghp_abcdefghij1234567890", 1790000001000)
	leaky.Text = "sk-abcdefghij1234567890"
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{conn, plain, leaky}})

	_, raw := clipboardOK(t, map[string]any{})
	for _, banned := range []string{"192.168.10.7", "S3cr3tDumpMe", "someone@example.com", "ghp_abcdefghij", "sk-abcdefghij"} {
		if strings.Contains(raw, banned) {
			t.Errorf("secret/PII %q must be scrubbed before egress: %s", banned, raw)
		}
	}
	for _, want := range []string{"[ipv4]", "[email]", "[redacted-key]", "普通文本", "收件地址"} {
		if !strings.Contains(raw, want) {
			t.Errorf("redaction must keep shape and non-sensitive text (%q missing): %s", want, raw)
		}
	}
}

// ---------- 接线、契约面与确定性 ----------

// TestClipboardUnwiredAndBackendErrors 未装配/后端报错都是指引错误，不 panic、
// 不静默空结果（fail-closed）。
func TestClipboardUnwiredAndBackendErrors(t *testing.T) {
	SetClipboardSource(nil)
	t.Cleanup(func() { SetClipboardSource(nil) })
	if text := clipErr(t, map[string]any{}); !strings.Contains(text, "未装配") {
		t.Errorf("unwired must guide, got: %s", text)
	}

	clipboardSetup(t, &fakeClipboard{err: errors.New("disk on fire")})
	if text := clipErr(t, map[string]any{}); !strings.Contains(text, "读取剪贴板库失败") {
		t.Errorf("backend error must surface, got: %s", text)
	}
}

// TestClipboardContractSurface 契约 §8 冻结面：工具名、参数集合与授权键名。
func TestClipboardContractSurface(t *testing.T) {
	if toolClipboard != "hanxi_clipboard_search" {
		t.Errorf("tool name drifted from contract §8: %s", toolClipboard)
	}
	if clipboardAccessKey != "clipboard" {
		t.Errorf("access key = %s", clipboardAccessKey)
	}
	tool, _ := buildClipboardSearchTool(Deps{})
	params := tool.InputSchema.Properties
	for _, want := range []string{"keyword", "tag", "kind", "limit"} {
		if _, ok := params[want]; !ok {
			t.Errorf("parameter %q missing from tool schema", want)
		}
	}
	if len(params) != 4 {
		t.Errorf("parameter surface must stay exactly keyword/tag/kind/limit, got %v", params)
	}
}

// TestClipboardDescriptionHonesty description 必须如实申报只读、敏感剔除与截断口径。
func TestClipboardDescriptionHonesty(t *testing.T) {
	tool, _ := buildClipboardSearchTool(Deps{})
	for _, phrase := range []string{"只读", "整条不下发", "不计入命中数", "元数据", "脱敏", "truncated",
		"逐字还原", "identityOnly", "textOmitted", "不参与关键词匹配"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description must contain %q: %s", phrase, tool.Description)
		}
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("clipboard search must carry readOnlyHint=true")
	}
	if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Error("clipboard search must carry destructiveHint=false")
	}
	if tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
		t.Error("pure read must carry idempotentHint=true")
	}
}

// TestClipboardRegisterTools registerClipboardTools 直挂形态（契约 §8 冻结名）：
// 工具确实进表，名称与 builder 形态一致。
func TestClipboardRegisterTools(t *testing.T) {
	s := server.NewMCPServer("hanxi-test", "0.0", server.WithToolCapabilities(false))
	registerClipboardTools(s, Deps{})
	if got := s.ListTools(); got[toolClipboard] == nil {
		t.Fatalf("registerClipboardTools must mount %s, got %d tools", toolClipboard, len(got))
	}
	if got := s.ListTools()[toolClipboard].Tool.Name; got != toolClipboard {
		t.Errorf("mounted tool name = %s", got)
	}
}

// TestClipboardOutputDeterministic 同库两次调用字节一致（排序定序 + map 无序漂移回归锚）。
func TestClipboardOutputDeterministic(t *testing.T) {
	a := mkClip("clip_a", clipKindText, "同刻条目 A", 1790000001000)
	b := mkClip("clip_b", clipKindText, "同刻条目 B", 1790000001000)
	clipboardSetup(t, &fakeClipboard{items: []clipboardDiskEntry{b, a}})
	_, first := clipboardOK(t, map[string]any{})
	_, second := clipboardOK(t, map[string]any{})
	if first != second {
		t.Errorf("output must be byte-stable: %s vs %s", first, second)
	}
	env, _ := clipboardOK(t, map[string]any{})
	if got := ids(env); strings.Join(got, ",") != "clip_a,clip_b" {
		t.Fatalf("same-millisecond items must order by id asc: %v", got)
	}
}

// ---------- 装载侧：尺寸闸、解密前预筛、告警聚合、路径脱敏（M2/L1/L3） ----------

// countingDecrypter 记录解密面被真正调用的次数与解出的明文字节——M2 的关键断言
// 靠它成立："按密文预筛"必须让超大条目**一次都不进解密面**，否则只是把内存账
// 从解密后挪到解密前。
type countingDecrypter struct {
	calls int
	plain int
}

func (c *countingDecrypter) decrypt(b64 string) ([]byte, error) {
	c.calls++
	plain, err := fakeDPAPIDecrypt(b64)
	if err == nil {
		c.plain += len(plain)
	}
	return plain, err
}

// writeClipIndex 把逐条原始 JSON 拼成临时 index.json（M2 用例要可控尺寸，
// 不把兆级数据塞进版本库夹具）。
func writeClipIndex(t *testing.T, entries ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clipboard", "index.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"entries":[` + strings.Join(entries, ",") + `]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// clipTextEntryJSON 最小 text 条目（正文按落盘形态存假密文）。
func clipTextEntryJSON(t *testing.T, id, cipher string, created int64) string {
	t.Helper()
	data, err := json.Marshal(clipboardDiskEntry{
		ID: id, Kind: clipKindText, Preview: id + " 摘要", Text: cipher, CreatedAt: created,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func entryByID(items []clipboardDiskEntry) map[string]clipboardDiskEntry {
	out := map[string]clipboardDiskEntry{}
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

// TestClipboardLoadPrefiltersOversizedBeforeDecrypt 累计明文预算下的预筛（M2 核心）：
// 超大条目**不解密**（解密面零调用增量）、密文不进下游、元数据保留并标记；
// 且预算耗尽后的小条目仍能被服务——预算花在解密之前，不是把整库判死。
func TestClipboardLoadPrefiltersOversizedBeforeDecrypt(t *testing.T) {
	small := fakeDPAPIEncrypt("小条目正文")
	big := fakeDPAPIEncrypt(strings.Repeat("z", 1<<20)) // ~1MiB 明文 / ~1.37MiB 密文
	// 顺序刻意把巨条排在最新：它先撞预算，验证"被预筛不影响后续预算内条目"。
	path := writeClipIndex(t,
		clipTextEntryJSON(t, "clip_big", big, 1790000003000),
		clipTextEntryJSON(t, "clip_s1", small, 1790000002000),
		clipTextEntryJSON(t, "clip_s2", small, 1790000001000),
	)

	dec := &countingDecrypter{}
	r := &clipboardDiskReader{path: path, decrypt: dec.decrypt, loadPlainMax: 64}
	items, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("三条都该在（超大条降级而非消失）: %v", idsOf(items))
	}
	// 巨条不进解密面：只有两条小条目被解（若它被解过，plain 会飙到 MiB 级）。
	if dec.calls != 2 || dec.plain > 64 {
		t.Errorf("pre-filter must skip decryption work: calls=%d plainBytes=%d", dec.calls, dec.plain)
	}
	byID := entryByID(items)
	bigEntry := byID["clip_big"]
	if bigEntry.Text != "" || !bigEntry.TextOverBudget {
		t.Errorf("over-budget entry must carry no ciphertext and be marked: text=%q marked=%v",
			bigEntry.Text, bigEntry.TextOverBudget)
	}
	if byID["clip_s1"].Text != "小条目正文" || byID["clip_s1"].TextOverBudget {
		t.Errorf("in-budget entry must be served: %+v", byID["clip_s1"])
	}
	// 工具面口径：该条按"仅元数据 + textOmitted"下发，绝不下发密文或空正文伪装。
	clipboardSetup(t, r)
	env, raw := clipboardOK(t, map[string]any{"limit": 1.0})
	if env.Count != 1 || !env.Results[0].TextOmitted || env.Results[0].Text != "" {
		t.Fatalf("over-budget item must degrade to metadata + textOmitted: %+v", env.Results)
	}
	if strings.Contains(raw, "ZmFrZS1kcGFwaTo") {
		t.Errorf("ciphertext must never reach the wire: %s", raw)
	}
}

// TestClipboardIndexSizeGateRefusesOversizedLibrary 单档尺寸闸（M2 最外层）：超限拒载
// 而非吞档，错误串只报文件名（顺带验 L1）。
func TestClipboardIndexSizeGateRefusesOversizedLibrary(t *testing.T) {
	path := writeClipIndex(t,
		clipTextEntryJSON(t, "clip_a", fakeDPAPIEncrypt(strings.Repeat("a", 2048)), 1790000001000),
		clipTextEntryJSON(t, "clip_b", fakeDPAPIEncrypt(strings.Repeat("b", 2048)), 1790000002000),
	)
	root := filepath.Dir(filepath.Dir(path))
	r := &clipboardDiskReader{path: path, decrypt: fakeDPAPIDecrypt, indexMaxBytes: 512}
	_, err := r.Load()
	if err == nil {
		t.Fatal("oversized index must be refused, not slurped into memory")
	}
	if !strings.Contains(err.Error(), "拒绝装载") || !strings.Contains(err.Error(), "index.json") {
		t.Errorf("size gate must guide with the file name: %v", err)
	}
	if strings.Contains(err.Error(), root) {
		t.Errorf("error must not carry the absolute path: %v", err)
	}
}

// TestClipboardErrorsHideAbsolutePath L1 谱系：os 级错误（*PathError 自带调用时传入的
// 完整路径）与"库位上是目录"两种形态，出参都不得带绝对路径——这条串会直达云端模型
// 上下文，带盘符的用户目录即机主画像。
func TestClipboardErrorsHideAbsolutePath(t *testing.T) {
	// (a) 路径中途是普通文件（Windows 报 ERROR_PATH_NOT_FOUND）：属"库不存在"家族，
	// 按合法真空态处理——不报错、更不创建任何东西（这条同时钉住真空库判定的边界）。
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "clipboard", "index.json")
	items, err := (&clipboardDiskReader{path: path, decrypt: fakeDPAPIDecrypt}).Load()
	if err != nil || len(items) != 0 {
		t.Fatalf("path-under-a-file must read as a void library: items=%v err=%v", items, err)
	}
	if _, serr := os.Stat(filepath.Dir(path)); !os.IsNotExist(serr) {
		t.Error("void-library read must not create directories")
	}

	// (b) 库位上是目录：我的自造文案 + errText 兜底都不得漏目录前缀。
	dirRoot := t.TempDir()
	dirPath := filepath.Join(dirRoot, "clipboard", "index.json")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = (&clipboardDiskReader{path: dirPath, decrypt: fakeDPAPIDecrypt}).Load()
	if err == nil || strings.Contains(err.Error(), dirRoot) {
		t.Fatalf("directory-as-library must fail without leaking the path: %v", err)
	}

	// (c) 单元口径：errText 同时收敛全路径与目录前缀。
	r := &clipboardDiskReader{path: filepath.Join(`E:\机主私密目录\hanxi\data\clipboard`, "index.json")}
	got := r.errText(fmt.Errorf("open %s: 拒绝访问", r.path)).Error()
	if strings.Contains(got, `E:\`) || strings.Contains(got, "机主私密目录") {
		t.Errorf("errText must scrub both the file path and its directory: %s", got)
	}
	if !strings.Contains(got, "index.json") {
		t.Errorf("errText must keep the file name: %s", got)
	}
}

// clipWarnOf 取捕获器里指定 message 的记录（复用包内 captureHandler，它替换 slog 默认器）。
func clipWarnOf(h *captureHandler, msg string) []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slog.Record
	for _, r := range h.records {
		if r.Message == msg {
			out = append(out, r)
		}
	}
	return out
}

// clipAttrInt 读记录里的整型属性（缺失按 0）。
func clipAttrInt(r slog.Record, key string) int64 {
	var v int64
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			switch x := a.Value.Any().(type) {
			case int:
				v = int64(x)
			case int64:
				v = x
			}
			return false
		}
		return true
	})
	return v
}

// TestClipboardSkipWarnAggregated 告警聚合（L3）：一次装载只出**一条** warn，
// 带总数/分原因计数/首条样例；敏感条目按"存在性即敏感"不进计数。
func TestClipboardSkipWarnAggregated(t *testing.T) {
	h := installAuditCapture(t)
	path := writeClipIndex(t,
		`{"id":"clip_badfield","kind":["这一条解码失败"]}`,
		`{"kind":"text","preview":"这一条没有 id"}`,
		clipTextEntryJSON(t, "clip_foreign", "@@not-protected@@", 1790000001000),
		`{"id":"clip_secret","kind":"text","preview":"敏感条目 MUSTNOTAPPEAR",`+
			`"text":"`+fakeDPAPIEncrypt("敏感条目正文 MUSTNOTAPPEAR")+`","sensitive":true}`,
	)
	r := &clipboardDiskReader{path: path, decrypt: fakeDPAPIDecrypt}
	items, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("夹具里该没有一条能活着出来: %v", idsOf(items))
	}
	msg := "mcp clipboard: entries skipped while loading"
	recs := clipWarnOf(h, msg)
	if len(recs) != 1 {
		t.Fatalf("无论坏多少条，一次装载只出一条聚合 warn，got %d", len(recs))
	}
	r0 := recs[0]
	if clipAttrInt(r0, "skipped") != 3 {
		t.Errorf("sensitive item must not be counted: %v", clipAttrInt(r0, "skipped"))
	}
	for _, reason := range []string{"decode", "missing-id", "decrypt"} {
		if clipAttrInt(r0, reason) != 1 {
			t.Errorf("reason %s must be tallied once: %d", reason, clipAttrInt(r0, reason))
		}
	}
	if attrString(r0, "sample") == "" || attrString(r0, "file") != "index.json" {
		t.Errorf("aggregate warn must carry a sample and the file name: %s / %s",
			attrString(r0, "sample"), attrString(r0, "file"))
	}
	// 样例与正文解耦：畸形库的内容不得顺手抄进日志。
	for _, banned := range []string{"这一条解码失败", "敏感条目"} {
		if strings.Contains(attrString(r0, "sample"), banned) {
			t.Errorf("sample leaked library content: %s", attrString(r0, "sample"))
		}
	}
}

// TestClipboardIndexPathLayout 落位布局钉契约 §2（<DataDir>/clipboard/index.json）；
// 真件构造函数不在此调用（会经 settings 建目录，违无头测试零落盘纪律）。
func TestClipboardIndexPathLayout(t *testing.T) {
	got, err := filepath.Rel(filepath.Join("root", "data"), clipboardIndexPath(filepath.Join("root", "data")))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(got) != "clipboard/index.json" {
		t.Errorf("index path = %s", got)
	}
}
