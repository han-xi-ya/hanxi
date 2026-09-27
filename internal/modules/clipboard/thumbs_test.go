package clipboard

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- 测试素材（确定性构造，不引随机源） ----------

// testGradientImg 平滑双色渐变（可压性好的"截图近似"）。
func testGradientImg(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / max(1, w-1)),
				G: uint8(y * 255 / max(1, h-1)),
				B: uint8((x + y) * 255 / max(1, w+h-2)),
				A: 255,
			})
		}
	}
	return img
}

// testGradientPNG 渐变 PNG 原图字节。
func testGradientPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, testGradientImg(w, h)); err != nil {
		t.Fatalf("构造渐变 PNG: %v", err)
	}
	return buf.Bytes()
}

// decodeThumbJPEG dataURL 前置校验 + base64 回解（StdEncoding 形态钉死）。
func decodeThumbJPEG(t *testing.T, dataURL string) []byte {
	t.Helper()
	if !strings.HasPrefix(dataURL, thumbDataURLPrefix) {
		t.Fatalf("dataURL 前缀错误: %.40q", dataURL)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, thumbDataURLPrefix))
	if err != nil {
		t.Fatalf("dataURL 非合法 StdEncoding base64: %v", err)
	}
	return raw
}

func thumbDims(t *testing.T, dataURL string) (int, int) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(decodeThumbJPEG(t, dataURL)))
	if err != nil {
		t.Fatalf("thumb JPEG 解码失败: %v", err)
	}
	return img.Bounds().Dx(), img.Bounds().Dy()
}

// ---------- 纯函数管线 ----------

// TestThumbPipeline 800×600 渐变 → 64×48 等比 JPEG q60 dataURL，预算内。
// 单条实测体积以 t.Logf 留档（汇报口径来源）。
func TestThumbPipeline(t *testing.T) {
	url, q, err := makeThumb(testGradientPNG(t, 800, 600))
	if err != nil {
		t.Fatalf("makeThumb: %v", err)
	}
	if q != thumbQualityLadder[0] {
		t.Fatalf("平滑图应止步首档 q60，得 q%d", q)
	}
	w, h := thumbDims(t, url)
	if w != 64 || h != 48 {
		t.Fatalf("等比尺寸应 64×48，得 %d×%d", w, h)
	}
	if int64(len(url)) > thumbBudgetBytes {
		t.Fatalf("超单条预算: %d B > %d B", len(url), thumbBudgetBytes)
	}
	t.Logf("thumb 实测：800×600 渐变 → %d B dataURL（q=%d，长边 %d）", len(url), q, thumbMaxEdge)
}

// TestThumbTargetSizes 只缩不放/长边钳 64/方形与非方形取整。
func TestThumbTargetSizes(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{30, 10, 30, 10},     // 小图原样，不放大
		{64, 64, 64, 64},     // 恰至上限
		{65, 65, 64, 64},     // 越界即缩
		{10, 400, 1, 64},     // 极端竖条目标长边 64，短边四舍入至少保 1
		{1920, 1080, 64, 36}, // 常见截图比例
	}
	for _, c := range cases {
		tw, th := thumbTargetSize(c.w, c.h)
		if tw != c.wantW || th != c.wantH {
			t.Errorf("thumbTargetSize(%d,%d) = %d×%d, want %d×%d", c.w, c.h, tw, th, c.wantW, c.wantH)
		}
		url, _, err := makeThumb(testGradientPNG(t, c.w, c.h))
		if err != nil {
			t.Errorf("makeThumb(%d×%d): %v", c.w, c.h, err)
			continue
		}
		if w, h := thumbDims(t, url); w != c.wantW || h != c.wantH {
			t.Errorf("%d×%d 出图 %d×%d，期望 %d×%d", c.w, c.h, w, h, c.wantW, c.wantH)
		}
	}
}

// TestThumbTransparentOverWhite 全透明 PNG → 白底而非黑块（JPEG 无 alpha 的合成口径）。
func TestThumbTransparentOverWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8)) // 全零=全透明
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	url, _, err := makeThumb(buf.Bytes())
	if err != nil {
		t.Fatalf("透明 PNG makeThumb: %v", err)
	}
	j, err := jpeg.Decode(bytes.NewReader(decodeThumbJPEG(t, url)))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := j.At(4, 4).RGBA()
	if r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Fatalf("透明像素应合成近白，得 RGB=0x%04x/0x%04x/0x%04x", r, g, b)
	}
}

// TestThumbBudgetDegrade 预算压低强制走降档通路：q 逐档下探到末档如实保留
// （生产预算 3KB 下平滑图止步 q60 已由 TestThumbPipeline 钉住）。
func TestThumbBudgetDegrade(t *testing.T) {
	url, q, err := encodeThumb(testGradientImg(64, 64), 64) // 64B 预算任何 JPEG 都装不下
	if err != nil {
		t.Fatalf("encodeThumb: %v", err)
	}
	if q != thumbQualityLadder[len(thumbQualityLadder)-1] {
		t.Fatalf("超预算应降到底档 q%d，得 q%d", thumbQualityLadder[len(thumbQualityLadder)-1], q)
	}
	if !strings.HasPrefix(url, thumbDataURLPrefix) {
		t.Fatalf("末档保留的仍须是合法 dataURL: %.40q", url)
	}
	t.Logf("底档实测：%d B（预算 64B，超档如实保留）", len(url))
}

// TestThumbBadInputs 坏输入只报错不 panic：空字节/非 PNG/零尺寸图。
func TestThumbBadInputs(t *testing.T) {
	for _, b := range [][]byte{nil, {}, []byte("PNGFAKE"), []byte("<html>not an image</html>")} {
		url, q, err := makeThumb(b)
		if err == nil {
			t.Fatalf("坏输入 %q 应报错", b)
		}
		if url != "" || q != 0 {
			t.Fatalf("报错时不应出值: url=%.20q q=%d", url, q)
		}
	}
	if _, _, err := encodeThumb(image.NewRGBA(image.Rect(0, 0, 0, 0)), thumbBudgetBytes); err == nil {
		t.Fatal("零尺寸源图应报错")
	}
}

// ---------- store 集成：Add 生成 ----------

// pngBlobFake 真 PNG 字节的 blobIO 假件（本文件独用，不扩展 store_test.go 的
// fakeBlobIO——避免与并行线在同文件打架）。
type pngBlobIO struct {
	mu      sync.Mutex
	data    map[string][]byte
	loads   []string
	loadErr error
}

func newPNGBlobIO() *pngBlobIO {
	return &pngBlobIO{data: map[string][]byte{}}
}

func (f *pngBlobIO) put(rel string, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[rel] = b
}

func (f *pngBlobIO) SaveImage(image.Image) (string, int, int, error) {
	return "", 0, 0, fmt.Errorf("pngBlobIO: SaveImage 不在缩略图测试路径")
}

func (f *pngBlobIO) Load(rel string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, rel)
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if b, ok := f.data[rel]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("pngBlobIO: blob 不存在: %s", rel)
}

func (f *pngBlobIO) Delete(rel string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, rel)
	return nil
}

func (f *pngBlobIO) TotalBytes() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var sum int64
	for _, b := range f.data {
		sum += int64(len(b))
	}
	return sum, nil
}

func (f *pngBlobIO) loadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.loads)
}

func newThumbTestStore(t *testing.T) (*Store, *pngBlobIO, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clipboard")
	fb := newPNGBlobIO()
	st, err := NewStore(dir, fb, nil, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st, fb, dir
}

func imageEntry(id, hash, blobRel string, byteSize int64) Entry {
	return Entry{ID: id, Kind: KindImage, Hash: hash, Blob: blobRelPrefix + blobRel,
		ByteSize: byteSize, CreatedAt: time.Now().UnixMilli()}
}

// TestClipStoreThumbOnAdd KindImage 入库生成 thumb 并随 index.json 落盘、重开
// 透传；非图片/无 blob/已有 thumb/超 8MiB/坏图/缺 blob 各跳过形态都不阻塞 Add。
func TestClipStoreThumbOnAdd(t *testing.T) {
	st, fb, dir := newThumbTestStore(t)
	pngBytes := testGradientPNG(t, 300, 200)
	fb.put("a.png", pngBytes)
	loads0 := fb.loadCount()

	got, _, err := st.Add(imageEntry("img-a", "h-a", "a.png", int64(len(pngBytes))), time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !strings.HasPrefix(got.Thumb, thumbDataURLPrefix) {
		t.Fatalf("Add 未生成 thumb: %.40q", got.Thumb)
	}
	if w, h := thumbDims(t, got.Thumb); w != 64 || h != 42 { // 300×200 → 64×42（等比取整）
		t.Fatalf("thumb 尺寸 %d×%d，期望 64×42", w, h)
	}
	if fb.loadCount() != loads0+1 {
		t.Fatalf("thumb 生成应回读 blob 恰一次: %d→%d", loads0, fb.loadCount())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"thumb": "data:image/jpeg;base64,`) {
		t.Fatal("thumb 未随条目落 index.json")
	}

	// 重开装载透传（lazy 不应再标 pending）
	reopened, err := NewStore(dir, newPNGBlobIO(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.thumbPending {
		t.Fatal("thumb 齐备的库存重开不应标记回填")
	}
	if r, ok := reopened.Get("img-a"); !ok || r.Thumb != got.Thumb {
		t.Fatalf("重开 thumb 丢失: ok=%v", ok)
	}

	// text 条目不碰 blob 回读
	loads1 := fb.loadCount()
	if _, _, err := st.Add(entryText("tx-1", "thumb 测试文本"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if fb.loadCount() != loads1 {
		t.Fatal("非图片条目不应触发 blob 回读")
	}

	// 已有 thumb 幂等不重算；无 blob / 超 8MiB / 坏图 / blob 缺失均留空且 Add 成功
	if _, _, err := st.Add(func() Entry {
		e := imageEntry("img-keep", "h-keep", "a.png", int64(len(pngBytes)))
		e.Thumb = "data:image/jpeg;base64,KEEP"
		return e
	}(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if fb.loadCount() != loads1 {
		t.Fatal("已有 thumb 不应重算（幂等）")
	}
	if k, _ := st.Get("img-keep"); k.Thumb != "data:image/jpeg;base64,KEEP" {
		t.Fatal("预置 thumb 被覆写")
	}
	big := imageEntry("img-big", "h-big", "a.png", 9<<20)
	if b, _, err := st.Add(big, time.Now()); err != nil || b.Thumb != "" {
		t.Fatalf("超 8MiB 应跳过生成且入库照常: %q %v", b.Thumb, err)
	}
	if noBlob, _, err := st.Add(imageEntry("img-noblob", "h-nb", "", 100), time.Now()); err != nil || noBlob.Thumb != "" {
		t.Fatalf("无 blob 图片应直接放行: %v", err)
	}
	fb.put("junk.png", []byte("PNGFAKE"))
	if junk, _, err := st.Add(imageEntry("img-junk", "h-junk", "junk.png", 7), time.Now()); err != nil || junk.Thumb != "" {
		t.Fatalf("坏图 Add 应成功且 thumb 留空（不 panic）: %q %v", junk.Thumb, err)
	}
	if gone, _, err := st.Add(imageEntry("img-gone", "h-gone", "missing.png", 100), time.Now()); err != nil || gone.Thumb != "" {
		t.Fatalf("blob 缺失 Add 应成功且 thumb 留空: %q %v", gone.Thumb, err)
	}
}

// ---------- store 集成：lazy 回填 ----------

// seedStoreDir 直写 index.json 造历史库（thumb 字段晚于存量库的形态）。
func seedStoreDir(t *testing.T, entries ...Entry) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clipboard")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(diskIndex{Version: indexVersion, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestClipStoreThumbLazyBackfill 首轮 List 后台补齐历史缺图条目：List 当轮
// 返回面不带 thumb（不阻塞），随后内存与盘收敛；大源图不入待办；补齐后重开
// 不再标记、再跑回填写 0（幂等）。
func TestClipStoreThumbLazyBackfill(t *testing.T) {
	pngBytes := testGradientPNG(t, 240, 180)
	fb := newPNGBlobIO()
	fb.put("a.png", pngBytes)
	dir := seedStoreDir(t,
		imageEntry("old-1", "h-old-1", "a.png", int64(len(pngBytes))),
		imageEntry("big-1", "h-big-1", "a.png", 9<<20), // 超 8MiB：永远补不出，不入待办
	)
	st, err := NewStore(dir, fb, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !st.thumbPending {
		t.Fatal("装载发现缺图条目应标记回填")
	}

	list := st.List("", string(KindImage), 0)
	if len(list) != 2 {
		t.Fatalf("列表应 2 条: %d", len(list))
	}
	for _, e := range list {
		if e.Thumb != "" {
			t.Fatal("首轮 List 不应阻塞等待后台回填")
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, _ := st.Get("old-1"); got.Thumb != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("后台回填未在时限内落定")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if still, _ := st.Get("big-1"); still.Thumb != "" {
		t.Fatal("超 8MiB 条目不应被回填")
	}
	// 盘面对拍：commitLocked 先改内存后原子写，内存见效≠已落稳，轮询到有为止
	// （jsonstore 原子 rename，读到的必是完整旧档/新档二态）。
	for {
		raw, _ := os.ReadFile(filepath.Join(dir, "index.json"))
		if strings.Contains(string(raw), `"thumb": "data:`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("回填结果未落盘")
		}
		time.Sleep(10 * time.Millisecond)
	}

	reopened, err := NewStore(dir, newPNGBlobIO(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.thumbPending {
		t.Fatal("补齐后重开不应再标记回填")
	}
	if r, _ := reopened.Get("old-1"); r.Thumb == "" {
		t.Fatal("回填未持久化")
	}
	if n := reopened.backfillThumbs(); n != 0 {
		t.Fatalf("幂等：再跑一次应回填 0 条，得 %d", n)
	}
}

// TestClipStoreThumbBackfillBadImage 坏图/缺 blob 的回填只跳过：计数 0、
// 条目原样、不 panic、不反复写盘。
func TestClipStoreThumbBackfillBadImage(t *testing.T) {
	fb := newPNGBlobIO()
	fb.put("junk.png", []byte("PNGFAKE"))
	dir := seedStoreDir(t,
		imageEntry("j-1", "h-j1", "junk.png", 7),
		imageEntry("m-1", "h-m1", "missing.png", 7),
	)
	st, err := NewStore(dir, fb, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !st.thumbPending {
		t.Fatal("缺图条目应标记回填")
	}
	if n := st.backfillThumbs(); n != 0 {
		t.Fatalf("坏图应全部跳过，得回填 %d", n)
	}
	if st.Count() != 2 {
		t.Fatal("坏图跳过不得影响条目数")
	}
	if e, _ := st.Get("j-1"); e.Thumb != "" {
		t.Fatal("坏图 thumb 应维持空")
	}
}
