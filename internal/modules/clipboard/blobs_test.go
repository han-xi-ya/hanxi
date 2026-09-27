package clipboard

// BlobStore 全量自测:幂等、白名单硬闸、原子性(并发)、TotalBytes 账、Delete 幂等。
// 全部程序化构造,零 fixture。测试名统一 blobTest 前缀防同包撞名。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// blobTestImage 确定性伪随机图,固定种子;不同 (w,h,seed) 内容必然不同。
func blobTestImage(w, h int, seed int64) *image.NRGBA {
	rnd := rand.New(rand.NewSource(seed))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		if i%4 == 3 {
			img.Pix[i] = 255
		} else {
			img.Pix[i] = uint8(rnd.Intn(256))
		}
	}
	return img
}

// blobTestPNG 测试侧独立编码(与实现同输入同输出,作为期望字节基准)。
func blobTestPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("基准 PNG 编码: %v", err)
	}
	return buf.Bytes()
}

func blobTestName(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + ".png"
}

var blobHexNameRe = regexp.MustCompile(`^[0-9a-f]{64}\.png$`)

func TestBlobLazyDirAndRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	b := NewBlobStore(dir)

	// 懒建:任何只读操作前目录不存在,且不报错不建目录
	if total, err := b.TotalBytes(); err != nil || total != 0 {
		t.Fatalf("空库应为 (0,nil),got (%d,%v)", total, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("NewBlobStore 不应建目录,stat=%v", err)
	}

	img := blobTestImage(7, 5, 1)
	rel, w, h, err := b.SaveImage(img)
	if err != nil {
		t.Fatal(err)
	}
	if w != 7 || h != 5 {
		t.Fatalf("宽高 got %dx%d want 7x5", w, h)
	}
	if !blobHexNameRe.MatchString(rel) || strings.ContainsAny(rel, `/\`) {
		t.Fatalf("rel 应为纯文件名,got %q", rel)
	}
	if rel != blobTestName(blobTestPNG(t, img)) {
		t.Fatal("命名不是 sha256(PNG 字节)")
	}

	data, err := b.Load(rel)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, blobTestPNG(t, img)) {
		t.Fatal("Load 字节与基准 PNG 不符")
	}
	// 回读解码自证:PNG 完整可用且尺寸一致
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 7 || cfg.Height != 5 {
		t.Fatalf("PNG 回读: %v %+v", err, cfg)
	}

	if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
		t.Fatalf("Save 后文件应在盘上: %v", err)
	}
}

func TestBlobSubRectDims(t *testing.T) {
	// 非零 Min 子图:宽高取 Dx/Dy,PNG 编码子区域。
	base := blobTestImage(8, 8, 2)
	sub := base.SubImage(image.Rect(1, 1, 4, 6)).(*image.NRGBA)
	b := NewBlobStore(filepath.Join(t.TempDir(), "blobs"))
	rel, w, h, err := b.SaveImage(sub)
	if err != nil {
		t.Fatal(err)
	}
	if w != 3 || h != 5 {
		t.Fatalf("子图宽高 got %dx%d want 3x5", w, h)
	}
	data, err := b.Load(rel)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, blobTestPNG(t, sub)) {
		t.Fatal("子图 PNG 基准不符")
	}
}

func TestBlobIdempotentNoRewrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	b := NewBlobStore(dir)
	img := blobTestImage(3, 3, 3)

	rel1, _, _, err := b.SaveImage(img)
	if err != nil {
		t.Fatal(err)
	}
	// 往同名文件灌哨兵字节:幂等承诺"不重写",再 Save 必须原样保留哨兵
	sentinel := []byte("SENTINEL-NOT-A-PNG")
	path := filepath.Join(dir, rel1)
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	rel2, _, _, err := b.SaveImage(img)
	if err != nil {
		t.Fatalf("同 hash 再 Save 不得报错: %v", err)
	}
	if rel1 != rel2 {
		t.Fatalf("同图 rel 必须一致: %q vs %q", rel1, rel2)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatal("幂等承诺被违背:文件被重写")
	}
}

func TestBlobDistinctContentDistinctNames(t *testing.T) {
	b := NewBlobStore(filepath.Join(t.TempDir(), "blobs"))
	relA, _, _, err := b.SaveImage(blobTestImage(4, 4, 10))
	if err != nil {
		t.Fatal(err)
	}
	relB, _, _, err := b.SaveImage(blobTestImage(4, 4, 11))
	if err != nil {
		t.Fatal(err)
	}
	if relA == relB {
		t.Fatal("内容不同却同 hash")
	}
	entries, err := os.ReadDir(b.dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("应恰好 2 个 blob: %d %v", len(entries), err)
	}
}

// blobBadRels 白名单攻击样例集:全部必须"在触盘前"被拒。
func blobBadRels() []string {
	hex64 := strings.Repeat("a", 64)
	return []string{
		"",
		"..",
		"../x.png",
		"..\\..\\evil.png",
		"a/..",
		"./" + hex64 + ".png",
		hex64 + ".png/../../secret",
		"/",
		"\\",
		"C:\\windows\\evil.png",
		"/tmp/" + hex64 + ".png",
		"\\\\nas\\share\\x.png",
		strings.Repeat("A", 64) + ".png", // 大写 hex
		hex64[:63] + ".png",              // 63 位
		hex64 + "0.png",                  // 65 位
		hex64 + ".PNG",                   // 大写扩展名
		hex64 + ".jpg",
		hex64,
		hex64 + ".png\n",
		" " + hex64 + ".png",
		"blobs/" + hex64 + ".png",
		hex64 + ".png\x00",
		"blob-" + hex64 + ".png",
		"g" + hex64[1:] + ".png", // 非 hex 字符 g
		strings.Repeat("0", 64) + ".png.tmp",
	}
}

func TestBlobRelWhitelistHardGate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	b := NewBlobStore(dir)
	bad := blobBadRels()
	for _, rel := range bad {
		t.Run("Load/"+rel, func(t *testing.T) {
			if data, err := b.Load(rel); err == nil {
				t.Fatalf("必须拒绝 %q,却返回 %d 字节", rel, len(data))
			}
		})
	}
	for _, rel := range bad {
		t.Run("Delete/"+rel, func(t *testing.T) {
			if err := b.Delete(rel); err == nil {
				t.Fatalf("必须拒绝删除 %q", rel)
			}
		})
	}
	// 闸必须在任何目录存在之前就生效:上面全部未落盘、未建目录
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("非法 rel 不得触发建目录")
	}
	// 合法名必须放行(同一条 hex 形态)
	if _, _, _, err := b.SaveImage(blobTestImage(2, 2, 5)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || !blobHexNameRe.MatchString(entries[0].Name()) {
		t.Fatalf("合法 Save 后应恰 1 个合规文件: %v %+v", err, entries)
	}
}

func TestBlobDeleteIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	b := NewBlobStore(dir)

	// 不存在目录删合法名:幂等 nil(闸先行,目录都不许建)
	if err := b.Delete(strings.Repeat("f", 64) + ".png"); err != nil {
		t.Fatalf("删除不存在 blob 应幂等: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("Delete 不应建目录")
	}

	rel, _, _, err := b.SaveImage(blobTestImage(2, 2, 6))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Load(rel); err == nil {
		t.Fatal("删除后 Load 必须失败")
	}
	if err := b.Delete(rel); err != nil {
		t.Fatalf("二次删除应幂等: %v", err)
	}
}

func TestBlobTotalBytesAccounting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	b := NewBlobStore(dir)

	var want int64
	for _, img := range []*image.NRGBA{blobTestImage(5, 5, 20), blobTestImage(9, 2, 21)} {
		rel, _, _, err := b.SaveImage(img)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		want += info.Size()
	}
	total, err := b.TotalBytes()
	if err != nil || total != want {
		t.Fatalf("账目不符: got %d want %d err %v", total, want, err)
	}

	// 脏目录:子目录、非 PNG 残留临时名、合法名前缀名——全部忽略不报错
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	junk := map[string]int{
		"nested/should-not-count.bin":    4096,
		".blob-stray.tmp":                4096,
		"notes.txt":                      4096,
		strings.Repeat("0", 64) + ".png": 0, // 先写一个"合法名空文件"证它被计入(0 字节),再删
	}
	for name, size := range junk {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.WriteFile(p, bytes.Repeat([]byte{7}, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	total, err = b.TotalBytes()
	if err != nil || total != want {
		t.Fatalf("脏条目应被忽略: got %d want %d err %v", total, want, err)
	}
	// 合法名 0 字节文件与脏名混布不影响计数(0 加不加都一样),换非零再验
	zeroName := strings.Repeat("0", 64) + ".png"
	if err := os.WriteFile(filepath.Join(dir, zeroName), []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	total, err = b.TotalBytes()
	if err != nil || total != want+2 {
		t.Fatalf("合法名文件必须入账: got %d want %d err %v", total, want+2, err)
	}
	if err := b.Delete(zeroName); err != nil {
		t.Fatal(err)
	}
	total, _ = b.TotalBytes()
	if total != want {
		t.Fatalf("Delete 后应回落: got %d want %d", total, want)
	}
}

func TestBlobConcurrentSameImageNoDupNoLoss(t *testing.T) {
	// 50 goroutine 同图并发:全部成功、同一 rel、目录恰好 1 个文件、字节无损。
	dir := filepath.Join(t.TempDir(), "blobs")
	b := NewBlobStore(dir)
	img := blobTestImage(16, 16, 30)
	wantPNG := blobTestPNG(t, img)
	wantRel := blobTestName(wantPNG)

	const n = 50
	var wg sync.WaitGroup
	start := make(chan struct{}) // 齐发门闸:一次 close 放行全部,最大化撞车窗口
	rels := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			rels[i], _, _, errs[i] = b.SaveImage(img)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("并发 Save %d 报错: %v", i, errs[i])
		}
		if rels[i] != wantRel {
			t.Fatalf("并发 Save %d rel=%q want %q", i, rels[i], wantRel)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("50 并发同图应恰 1 文件,得 %d: %v", len(entries), names)
	}
	data, err := b.Load(wantRel)
	if err != nil || !bytes.Equal(data, wantPNG) {
		t.Fatalf("并发后 Load 字节受损: %v", err)
	}
}

func TestBlobConcurrentDistinctMixed(t *testing.T) {
	// 8 张不同图并发各存 4 次(32 Save):不同内容不同 rel,互不踩;总账精确。
	dir := filepath.Join(t.TempDir(), "blobs")
	b := NewBlobStore(dir)
	const imgs, reps = 8, 4

	wantRel := make([]string, imgs)
	wantSize := int64(0)
	for i := 0; i < imgs; i++ {
		data := blobTestPNG(t, blobTestImage(8, 8, int64(100+i)))
		wantRel[i] = blobTestName(data)
		wantSize += int64(len(data))
	}

	var wg sync.WaitGroup
	rels := make([]string, imgs*reps)
	errs := make([]error, imgs*reps)
	for i := 0; i < imgs*reps; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			rel, _, _, err := b.SaveImage(blobTestImage(8, 8, int64(100+slot%imgs)))
			rels[slot], errs[slot] = rel, err
		}(i)
	}
	wg.Wait()

	for slot, err := range errs {
		if err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
		if rels[slot] != wantRel[slot%imgs] {
			t.Fatalf("slot %d rel 漂移: %q", slot, rels[slot])
		}
	}
	if _, err := b.TotalBytes(); err != nil {
		t.Fatal(err)
	}
	// PNG 大小对账要用盘上字节:TotalBytes 数盘上文件大小,与内存字节等值
	total, err := b.TotalBytes()
	if err != nil || total != wantSize {
		t.Fatalf("并发后总账: got %d want %d err %v", total, wantSize, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != imgs {
		t.Fatalf("应恰好 %d 个 blob,得 %d", imgs, len(entries))
	}
}

func TestBlobNilImage(t *testing.T) {
	b := NewBlobStore(t.TempDir())
	if _, _, _, err := b.SaveImage(nil); err == nil {
		t.Fatal("nil 图必须报错")
	}
}

func TestBlobColorModelIndependence(t *testing.T) {
	// 非 NRGBA 源(*image.RGBA、*image.Gray)也能入库:image/png 全能,
	// rel 仍按 PNG 字节寻址。钉住"API 只收 image.Image"的解耦承诺。
	b := NewBlobStore(filepath.Join(t.TempDir(), "blobs"))
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{1, 2, 3, 255})
	rel, w, h, err := b.SaveImage(src)
	if err != nil || w != 2 || h != 2 {
		t.Fatalf("RGBA 入库: %v %dx%d", err, w, h)
	}
	if _, err := b.Load(rel); err != nil {
		t.Fatal(err)
	}
	g := image.NewGray(image.Rect(0, 0, 2, 2))
	g.SetGray(0, 0, color.Gray{Y: 128})
	rel2, _, _, err := b.SaveImage(g)
	if err != nil || rel2 == "" {
		t.Fatalf("Gray 入库: %v", err)
	}
}
