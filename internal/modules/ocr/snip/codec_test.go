package snip

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"
	"time"
)

// mkDIB 组装 BITMAPINFOHEADER 头部（含可选位域掩码与调色板）+ 像素。
func mkDIB(width, height int32, bitCount uint16, compression uint32, palette [][]byte, pixels []byte) []byte {
	var b bytes.Buffer
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:4], 40)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(width))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(height))
	binary.LittleEndian.PutUint16(hdr[12:14], 1)
	binary.LittleEndian.PutUint16(hdr[14:16], bitCount)
	binary.LittleEndian.PutUint32(hdr[16:20], compression)
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(len(palette)))
	b.Write(hdr)
	for _, m := range palette { // 前 3 项当位域掩码用（BI_BITFIELDS）
		p := make([]byte, 4)
		copy(p, m)
		binary.LittleEndian.PutUint32(p, binary.LittleEndian.Uint32(m))
		b.Write(p)
	}
	b.Write(pixels)
	return b.Bytes()
}

func mustDecode(t *testing.T, dib []byte) image.Image {
	t.Helper()
	raw, err := DecodeDIBToPNG(dib)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("PNG 回读失败: %v", err)
	}
	return img
}

func TestDecodeDIB24BitBottomUp(t *testing.T) {
	// 2x2 24bit：行 8B 对齐（2px*3B=6B pad 到 8）；bottom-up 首行是底行。
	// DIB 字节序为 B,G,R。
	pixels := []byte{
		255, 0, 0, 255, 0, 0, 0, 0, // 底行：两像素 蓝（B=255）
		0, 0, 255, 0, 0, 255, 0, 0, // 顶行：两像素 红（R=255）
	}
	dib := mkDIB(2, 2, 24, 0, nil, pixels)
	img := mustDecode(t, dib)
	r, g, bb, a := img.At(0, 0).RGBA()
	// 顶行红应映射到 y=0
	if r>>8 != 255 || bb>>8 != 0 || a>>8 != 255 {
		t.Fatalf("y=0 应为红, 得 %d,%d,%d,%d", r>>8, g>>8, bb>>8, a>>8)
	}
	r, _, bb, _ = img.At(1, 1).RGBA()
	if r>>8 != 0 || bb>>8 != 255 {
		t.Fatalf("y=1 应为蓝, 得 %d,%d", r>>8, bb>>8)
	}
}

func TestDecodeDIB32BitTopDownAlphaFallback(t *testing.T) {
	// 高度为负 = top-down；alpha 全 0 需按不透明处理（剪贴板 DIB 常见）
	// DIB 字节序 B,G,R,A：期望 RGBA(7,8,9,255)
	pixels := make([]byte, 2*2*4)
	for i := 0; i < 8; i += 4 {
		pixels[i+0] = 9 // B
		pixels[i+1] = 8 // G
		pixels[i+2] = 7 // R
	}
	for i := 8; i < 16; i += 4 {
		pixels[i+0] = 109 // B
		pixels[i+1] = 108 // G
		pixels[i+2] = 107 // R
	}
	dib := mkDIB(2, -2, 32, 0, nil, pixels)
	img := mustDecode(t, dib)
	r, g, b, a := img.At(0, 0).RGBA()
	if r>>8 != 7 || g>>8 != 8 || b>>8 != 9 || a>>8 != 255 {
		t.Fatalf("top-down 首像素异常: %d,%d,%d,%d", r>>8, g>>8, b>>8, a>>8)
	}
	r, _, _, _ = img.At(1, 1).RGBA()
	if r>>8 != 107 {
		t.Fatalf("top-down 末像素异常: %d", r>>8)
	}
}

func TestDecodeDIB8BitPalette(t *testing.T) {
	pal := make([][]byte, 2)
	// BGRA 调色板项：索引0 黑、索引1 绿
	pal[0] = []byte{0, 0, 0, 0}
	pal[1] = []byte{0, 255, 0, 0}
	// 2px 8bit 行 = 2B pad 到 4；bottom-up：像素首行是底行(索引1)，第二行是顶行(索引0)
	pixels := []byte{1, 1, 0, 0, 0, 0, 0, 0}
	dib := mkDIB(2, 2, 8, 0, pal, pixels)
	img := mustDecode(t, dib)
	_, g, _, _ := img.At(0, 1).RGBA()
	if g>>8 != 255 {
		t.Fatalf("底行应为绿, g=%d", g>>8)
	}
	_, g, _, _ = img.At(0, 0).RGBA()
	if g != 0 {
		t.Fatalf("顶行应为黑, g=%d", g)
	}
}

func TestDecodeDIBRejects(t *testing.T) {
	cases := map[string][]byte{
		"短头":   {1, 2, 3},
		"OS2头": func() []byte { b := make([]byte, 40); binary.LittleEndian.PutUint32(b, 12); return b }(),
		"位深16": func() []byte {
			b := make([]byte, 40)
			binary.LittleEndian.PutUint32(b, 40)
			binary.LittleEndian.PutUint32(b[4:], 2)
			binary.LittleEndian.PutUint32(b[8:], 2)
			binary.LittleEndian.PutUint16(b[14:], 16)
			return b
		}(),
		"像素不足": func() []byte {
			b := make([]byte, 40)
			binary.LittleEndian.PutUint32(b, 40)
			binary.LittleEndian.PutUint32(b[4:], 8)
			binary.LittleEndian.PutUint32(b[8:], 8)
			binary.LittleEndian.PutUint16(b[14:], 24)
			return b
		}(),
		"BITFIELDS非标准掩码": mkDIB(1, -1, 32, 3, [][]byte{{1, 0, 0, 0}, {2, 0, 0, 0}, {3, 0, 0, 0}}, make([]byte, 4)),
	}
	for name, dib := range cases {
		if _, err := DecodeDIBToPNG(dib); err == nil {
			t.Fatalf("%s 应被拒", name)
		}
	}
}

func TestWaitForStateMachine(t *testing.T) {
	tick := 100 * time.Millisecond
	// 第 3 次命中
	calls := 0
	now := time.Unix(0, 0)
	deadline := now.Add(1 * time.Second)
	grab := func() ([]byte, bool, error) {
		calls++
		return []byte("x"), calls == 3, nil
	}
	sleep := func(d time.Duration) { now = now.Add(d) }
	data, found, err := waitFor(grab, deadline, tick, nowFn(&now), sleep)
	if err != nil || !found || string(data) != "x" {
		t.Fatalf("应在第 3 次命中: %v %v", found, err)
	}
	if calls != 3 {
		t.Fatalf("调用次数 %d", calls)
	}
	// 超时未命中且无错
	calls = 0
	data, found, err = waitFor(func() ([]byte, bool, error) { calls++; return nil, false, nil }, now.Add(250*time.Millisecond), tick, nowFn(&now), sleep)
	if err != nil || found || data != nil {
		t.Fatalf("超时应未命中无错: %v %v", found, err)
	}
	// 错误即止
	calls = 0
	boom := &stubErr{}
	_, found, err = waitFor(boom.grab, now.Add(time.Second), tick, nowFn(&now), sleep)
	if err == nil || found {
		t.Fatal("grab 错误应透传")
	}
}

type stubErr struct{}

func (*stubErr) grab() ([]byte, bool, error) { return nil, false, errStub }

var errStub = &myErr{}

type myErr struct{}

func (*myErr) Error() string { return "stub" }

func nowFn(p *time.Time) func() time.Time {
	return func() time.Time { return *p }
}
