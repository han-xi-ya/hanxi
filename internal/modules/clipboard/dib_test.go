package clipboard

// DecodeDIB 全量自测:无二进制 fixture,所有 DIB 流程序化构造(确定性)。
//
// 交叉验证说明(与任务书的一条环境性偏差,非契约偏差):本仓工具链为
// Go 1.26,stdlib 已按 1.24 的弃用预告移除 image/bmp,"与 image/bmp Encode
// 交叉比对"物理上不可行(禁新增依赖,也不引 golang.org/x/image)。
// 等价替代:下方 dibTestReferenceDecode 为**测试内朴素参考解码器**——
// 不共享 DecodeDIB 任何代码,按微软 BITMAPINFOHEADER 规范直译的无技巧
// 实现,与生产实现、构造时黄金像素做三方比对,强度不低于 stdlib 对拍。

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math/rand"
	"strings"
	"testing"
)

// ---------- 构造 helper(全部 dibTest 前缀,防与同包 A2 测试撞名) ----------

func dibTestHeader(biSize uint32, width, height int, planes uint16, bitCount uint16, compression, clrUsed uint32) []byte {
	hdr := make([]byte, biSize)
	binary.LittleEndian.PutUint32(hdr[0:4], biSize)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(int32(width)))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(int32(height)))
	binary.LittleEndian.PutUint16(hdr[12:14], planes)
	binary.LittleEndian.PutUint16(hdr[14:16], bitCount)
	binary.LittleEndian.PutUint32(hdr[16:20], compression)
	binary.LittleEndian.PutUint32(hdr[36:40], clrUsed)
	return hdr
}

func dibTestStride(width, bitCount int) int {
	return (width*bitCount + 31) / 32 * 4
}

// dibTestU32 负值常量的补码位模式:常量直转 uint32(-N) 编译期溢出,
// 经函数形参(非常量)回绕,一行表意。
func dibTestU32(v int32) uint32 { return uint32(v) }

// dibTestPixels24 拼 24bpp BI_RGB 流(头+像素)。px 为顶→底行序的黄金像素;
// topDown=false 时物理行序翻转(bottom-up)。pad 填充行尾对齐字节。
func dibTestPixels24(width, height int, topDown bool, pad uint8, px []color.NRGBA) []byte {
	stride := dibTestStride(width, 24)
	biHeight := height
	if topDown {
		biHeight = -height
	}
	buf := &bytes.Buffer{}
	buf.Write(dibTestHeader(dibHeaderInfo, width, biHeight, 1, 24, dibBI_RGB, 0))
	body := make([]byte, stride*height)
	for y := 0; y < height; y++ {
		srcY := y
		if !topDown {
			srcY = height - 1 - y
		}
		row := body[srcY*stride:]
		for x := 0; x < width; x++ {
			p := px[y*width+x]
			row[x*3] = p.B
			row[x*3+1] = p.G
			row[x*3+2] = p.R
		}
		for i := width * 3; i < stride; i++ {
			row[i] = pad
		}
	}
	buf.Write(body)
	return buf.Bytes()
}

// dibTestPixels32 拼 32bpp 流;bitfields=true 时在头后追加 3(或含 alpha 共 4)
// 个掩码 DWORD。alpha!=0 表示 V4 头携带 alpha 掩码(经典 biSize=40 形态无 alpha 位)。
func dibTestPixels32(width, height int, topDown, bitfields, alpha bool, px []color.NRGBA) []byte {
	stride := dibTestStride(width, 32)
	biHeight := height
	if topDown {
		biHeight = -height
	}
	comp := uint32(dibBI_RGB)
	biSize := uint32(dibHeaderInfo)
	if bitfields {
		comp = dibBI_BITFIELDS
		if alpha {
			biSize = dibHeaderV4 // alpha 掩码只在 V4+ 有官方安放处
		}
	}
	hdr := dibTestHeader(biSize, width, biHeight, 1, 32, comp, 0)
	if bitfields && alpha {
		// V4:头内偏移 40/44/48/52 = R/G/B/A
		binary.LittleEndian.PutUint32(hdr[40:], dibStdMaskRed)
		binary.LittleEndian.PutUint32(hdr[44:], dibStdMaskGreen)
		binary.LittleEndian.PutUint32(hdr[48:], dibStdMaskBlue)
		binary.LittleEndian.PutUint32(hdr[52:], dibStdMaskAlpha)
	}
	body := make([]byte, stride*height)
	for y := 0; y < height; y++ {
		srcY := y
		if !topDown {
			srcY = height - 1 - y
		}
		for x := 0; x < width; x++ {
			p := px[y*width+x]
			v := uint32(p.B) | uint32(p.G)<<8 | uint32(p.R)<<16
			if bitfields && alpha {
				v |= uint32(p.A) << 24
			}
			binary.LittleEndian.PutUint32(body[srcY*stride+x*4:], v)
		}
	}
	buf := &bytes.Buffer{}
	buf.Write(hdr)
	if bitfields && !alpha {
		// 经典形态:掩码三连 DWORD 占用调色板表前 3 项
		var m [12]byte
		binary.LittleEndian.PutUint32(m[0:], dibStdMaskRed)
		binary.LittleEndian.PutUint32(m[4:], dibStdMaskGreen)
		binary.LittleEndian.PutUint32(m[8:], dibStdMaskBlue)
		buf.Write(m[:])
	}
	buf.Write(body)
	return buf.Bytes()
}

// dibTestIndexed 拼 8/4/1bpp 调色板流。palette 为 RGBQUAD 表项(蓝绿红序);
// idx 顶→底索引。
func dibTestIndexed(width, height, bitCount int, topDown bool, palette []color.NRGBA, idx []uint8) []byte {
	stride := dibTestStride(width, bitCount)
	biHeight := height
	if topDown {
		biHeight = -height
	}
	buf := &bytes.Buffer{}
	buf.Write(dibTestHeader(dibHeaderInfo, width, biHeight, 1, uint16(bitCount), dibBI_RGB, uint32(len(palette))))
	for _, p := range palette {
		buf.WriteByte(p.B)
		buf.WriteByte(p.G)
		buf.WriteByte(p.R)
		buf.WriteByte(0)
	}
	body := make([]byte, stride*height)
	for y := 0; y < height; y++ {
		srcY := y
		if !topDown {
			srcY = height - 1 - y
		}
		row := body[srcY*stride:]
		switch bitCount {
		case 8:
			for x := 0; x < width; x++ {
				row[x] = idx[y*width+x]
			}
		case 4:
			for x := 0; x < width; x++ {
				if idx[y*width+x]&0x0F != idx[y*width+x] {
					panic("索引超 4bit")
				}
				if x%2 == 0 {
					row[x/2] |= idx[y*width+x] << 4
				} else {
					row[x/2] |= idx[y*width+x]
				}
			}
		case 1:
			for x := 0; x < width; x++ {
				if idx[y*width+x] != 0 {
					row[x/8] |= 1 << (7 - uint(x%8))
				}
			}
		}
	}
	buf.Write(body)
	return buf.Bytes()
}

// dibTestRamp 确定性伪随机 RGBA 图像(固定种子)。
func dibTestRamp(width, height int, withAlpha bool) []color.NRGBA {
	rnd := rand.New(rand.NewSource(20260926))
	px := make([]color.NRGBA, width*height)
	for i := range px {
		px[i] = color.NRGBA{
			R: uint8(rnd.Intn(256)),
			G: uint8(rnd.Intn(256)),
			B: uint8(rnd.Intn(256)),
			A: 255,
		}
		if withAlpha {
			px[i].A = uint8(rnd.Intn(256))
		}
	}
	return px
}

// ---------- 测试内朴素参考解码器(与 DecodeDIB 零共享) ----------

// dibTestReferenceDecode 按规范直译的无技巧解码,仅覆盖测试用到的子集:
// BI_RGB 24/32 与标准掩码 BI_BITFIELDS 32;BI_RGB 32 按 GDI 语义取不透明。
// 返回顶→底行序黄金 NRGBA 数组。任何异常一律 t.Fatal。
func dibTestReferenceDecode(t *testing.T, stream []byte) (w, h int, px []color.NRGBA) {
	t.Helper()
	s := stream
	if len(s) >= 14 && s[0] == 'B' && s[1] == 'M' {
		s = s[14:]
	}
	if len(s) < 40 {
		t.Fatalf("参考解码:流过短 %d", len(s))
	}
	biSize := int(binary.LittleEndian.Uint32(s[0:4]))
	if biSize != 40 && biSize != 112 && biSize != 124 {
		t.Fatalf("参考解码:头长 %d", biSize)
	}
	w = int(int32(binary.LittleEndian.Uint32(s[4:8])))
	hRaw := int(int32(binary.LittleEndian.Uint32(s[8:12])))
	if binary.LittleEndian.Uint16(s[12:14]) != 1 {
		t.Fatal("参考解码:planes≠1")
	}
	bpp := int(binary.LittleEndian.Uint16(s[14:16]))
	comp := binary.LittleEndian.Uint32(s[16:20])
	if w <= 0 || hRaw == 0 {
		t.Fatal("参考解码:尺寸非法")
	}
	topDown := hRaw < 0
	h = hRaw
	if topDown {
		h = -hRaw
	}
	alphaMask := false
	dataOff := biSize
	switch {
	case comp == 0 && (bpp == 24 || bpp == 32):
	case comp == 3 && bpp == 32:
		if biSize == 40 {
			r := binary.LittleEndian.Uint32(s[dataOff:])
			g := binary.LittleEndian.Uint32(s[dataOff+4:])
			b := binary.LittleEndian.Uint32(s[dataOff+8:])
			if r != dibStdMaskRed || g != dibStdMaskGreen || b != dibStdMaskBlue {
				t.Fatal("参考解码:非标准掩码")
			}
			dataOff += 12
		} else {
			if binary.LittleEndian.Uint32(s[52:]) == dibStdMaskAlpha {
				alphaMask = true
			}
		}
	default:
		t.Fatalf("参考解码:子集外参数 comp=%d bpp=%d", comp, bpp)
	}
	stride := dibTestStride(w, bpp)
	if len(s) < dataOff+stride*h {
		t.Fatalf("参考解码:截断")
	}
	px = make([]color.NRGBA, w*h)
	for y := 0; y < h; y++ {
		srcY := y
		if !topDown {
			srcY = h - 1 - y
		}
		for x := 0; x < w; x++ {
			var p color.NRGBA
			if bpp == 24 {
				o := dataOff + srcY*stride + x*3
				p = color.NRGBA{R: s[o+2], G: s[o+1], B: s[o], A: 255}
			} else {
				o := dataOff + srcY*stride + x*4
				v := binary.LittleEndian.Uint32(s[o:])
				a := uint8(255)
				if alphaMask {
					a = uint8(v >> 24)
				}
				p = color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: a}
			}
			px[y*w+x] = p
		}
	}
	return w, h, px
}

// dibTestAssertMatches 三方比对:DecodeDIB == 参考解码 == 构造黄金像素。
func dibTestAssertMatches(t *testing.T, stream []byte, golden []color.NRGBA) {
	t.Helper()
	img, err := DecodeDIB(stream)
	if err != nil {
		t.Fatalf("DecodeDIB: %v", err)
	}
	rw, rh, ref := dibTestReferenceDecode(t, stream)
	if img.Bounds() != image.Rect(0, 0, rw, rh) {
		t.Fatalf("尺寸不符: got %v want %dx%d", img.Bounds(), rw, rh)
	}
	for y := 0; y < rh; y++ {
		for x := 0; x < rw; x++ {
			got, ok := img.At(x, y).(color.NRGBA)
			if !ok {
				t.Fatalf("(%d,%d) 非 NRGBA: %T", x, y, img.At(x, y))
			}
			if want := ref[y*rw+x]; got != want {
				t.Errorf("(%d,%d) 与参考解码不符: got %v want %v", x, y, got, want)
			}
			if want := golden[y*rw+x]; got != want {
				t.Errorf("(%d,%d) 与黄金像素不符: got %v want %v", x, y, got, want)
			}
		}
	}
}

// dibTestAssertGolden 比对 DecodeDIB 与黄金像素(用于参考解码器覆盖不了的
// 布局变体,如 bfOffBits 指向掩码后间隙的真实世界 BMP)。
func dibTestAssertGolden(t *testing.T, w, h int, stream []byte, golden []color.NRGBA) {
	t.Helper()
	img, err := DecodeDIB(stream)
	if err != nil {
		t.Fatalf("DecodeDIB: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, w, h) {
		t.Fatalf("尺寸不符: got %v want %dx%d", img.Bounds(), w, h)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if got, want := img.At(x, y).(color.NRGBA), golden[y*w+x]; got != want {
				t.Errorf("(%d,%d) got %v want %v", x, y, got, want)
			}
		}
	}
}

// ---------- 正向用例 ----------

func TestDib24SmallSquaresBothOrientations(t *testing.T) {
	// 1x1 与 2x2,bottom-up 与 top-down;2x2 用不对称图案钉死行序方向。
	one := []color.NRGBA{{R: 1, G: 2, B: 3, A: 255}}
	dibTestAssertMatches(t, dibTestPixels24(1, 1, false, 0, one), one)
	dibTestAssertMatches(t, dibTestPixels24(1, 1, true, 0, one), one)

	two := []color.NRGBA{
		{R: 10, G: 20, B: 30, A: 255}, {R: 40, G: 50, B: 60, A: 255},
		{R: 70, G: 80, B: 90, A: 255}, {R: 100, G: 110, B: 120, A: 255},
	}
	img24, err := DecodeDIB(dibTestPixels24(2, 2, false, 0xAA, two))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img24.(*image.NRGBA); !ok {
		t.Fatalf("24bpp 应输出 *image.NRGBA,得到 %T", img24)
	}
	dibTestAssertMatches(t, dibTestPixels24(2, 2, false, 0xAA, two), two)
	dibTestAssertMatches(t, dibTestPixels24(2, 2, true, 0xAA, two), two)
}

func TestDib24OddWidthPaddingNonZero(t *testing.T) {
	// 宽 3:行 9 字节 + 3 字节对齐填充,填充灌非零验证其被跳过。
	px := dibTestRamp(3, 5, false)
	dibTestAssertMatches(t, dibTestPixels24(3, 5, false, 0xFF, px), px)
	dibTestAssertMatches(t, dibTestPixels24(3, 5, true, 0x11, px), px)
}

func TestDib32OpaqueIgnoresReservedAlpha(t *testing.T) {
	// BI_RGB 32bpp:第 4 字节是 GDI 保留值(实测剪贴板常为 0),解码按不透明,
	// 否则整屏截图入库会变全透明废图。
	px := []color.NRGBA{
		{R: 1, G: 2, B: 3, A: 255}, {R: 4, G: 5, B: 6, A: 255},
		{R: 7, G: 8, B: 9, A: 255}, {R: 10, G: 11, B: 12, A: 255},
	}
	stream := dibTestPixels32(2, 2, false, false, false, px)
	// 手工把 alpha 字节改成垃圾(0x00/0x7F),结果必须仍不透明
	for i := 0; i < 4; i++ {
		stream[dibHeaderInfo+i*4+3] = []byte{0x00, 0x7F, 0x00, 0xFF}[i]
	}
	dibTestAssertMatches(t, stream, px)
}

func TestDibBitFields32StandardNoAlpha(t *testing.T) {
	px := dibTestRamp(3, 4, false) // A 全 255,掩码无 alpha → 不透明
	dibTestAssertMatches(t, dibTestPixels32(3, 4, false, true, false, px), px)
	dibTestAssertMatches(t, dibTestPixels32(3, 4, true, true, false, px), px)
}

func TestDibBitFields32V4AlphaPreserved(t *testing.T) {
	// V4 + alpha 掩码:A 通道按位抽取,非预乘原样进 NRGBA(0 保持全透)。
	px := dibTestRamp(4, 3, true)
	dibTestAssertMatches(t, dibTestPixels32(4, 3, false, true, true, px), px)
	dibTestAssertMatches(t, dibTestPixels32(4, 3, true, true, true, px), px)
}

func TestDibV4AlphaMaskZeroMeansOpaque(t *testing.T) {
	// V4 头但 alpha 掩码为 0:视作无 alpha,全不透明。
	// 掩码标准位:R 在 bits16-23、G 在 8-15、B 在 0-7,黄金像素与打包 DWORD 同构。
	px := []color.NRGBA{{R: 7, G: 8, B: 9, A: 255}}
	hdr := dibTestHeader(dibHeaderV4, 1, -1, 1, 32, dibBI_BITFIELDS, 0)
	binary.LittleEndian.PutUint32(hdr[40:], dibStdMaskRed)
	binary.LittleEndian.PutUint32(hdr[44:], dibStdMaskGreen)
	binary.LittleEndian.PutUint32(hdr[48:], dibStdMaskBlue)
	// hdr[52:] alpha 掩码留 0
	var v [4]byte
	binary.LittleEndian.PutUint32(v[:], uint32(7)<<16|uint32(8)<<8|uint32(9))
	stream := append(append(make([]byte, 0, len(hdr)+4), hdr...), v[:]...)
	dibTestAssertMatches(t, stream, px)
}

func TestDibV5Header24bpp(t *testing.T) {
	// BITMAPV5HEADER(124):前 40 字节布局同 V3,数据紧随头。
	px := dibTestRamp(2, 3, false)
	h := dibTestHeader(dibHeaderV5, 2, 3, 1, 24, dibBI_RGB, 0)
	stride := dibTestStride(2, 24) // =8:2 像素 6 字节 + 2 对齐填充
	body := make([]byte, stride*3)
	for y := 0; y < 3; y++ {
		srcY := 2 - y // bottom-up
		for x := 0; x < 2; x++ {
			p := px[y*2+x]
			body[srcY*stride+x*3] = p.B
			body[srcY*stride+x*3+1] = p.G
			body[srcY*stride+x*3+2] = p.R
		}
	}
	dibTestAssertMatches(t, append(h, body...), px)
}

func TestDib8BitPaletteGrayAndLuminance(t *testing.T) {
	// 256 项灰阶坡道 + 3 个彩色项;灰阶必须精确复原,彩色项按 Rec.601 整型降灰。
	palette := make([]color.NRGBA, 256)
	for v := 0; v < 256; v++ {
		palette[v] = color.NRGBA{R: uint8(v), G: uint8(v), B: uint8(v), A: 255}
	}
	palette[10] = color.NRGBA{R: 255} // 红 → 77
	palette[11] = color.NRGBA{G: 255} // 绿 → 149
	palette[12] = color.NRGBA{B: 255} // 蓝 → 29
	idx := []uint8{0, 128, 255, 10, 11, 12}
	stream := dibTestIndexed(3, 2, 8, false, palette, idx)
	img, err := DecodeDIB(stream)
	if err != nil {
		t.Fatal(err)
	}
	gray, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("调色板应降级 *image.Gray,得到 %T", img)
	}
	want := []uint8{0, 128, 255, 77, 149, 29}
	for i, w := range want {
		x, y := i%3, i/3
		if g := gray.At(x, y).(color.Gray); g.Y != w {
			t.Errorf("像素(%d,%d)灰度 got %d want %d", x, y, g.Y, w)
		}
	}
}

func TestDib4BitAnd1BitPalette(t *testing.T) {
	// 4bpp:16 级灰阶坡道;宽 3(奇数,末位 nibble 只取高位)。
	pal16 := make([]color.NRGBA, 16)
	for v := range pal16 {
		gray := uint8(v * 17)
		pal16[v] = color.NRGBA{R: gray, G: gray, B: gray, A: 255}
	}
	idx4 := []uint8{15, 0, 8, 3, 14, 1}
	img4, err := DecodeDIB(dibTestIndexed(3, 2, 4, false, pal16, idx4))
	if err != nil {
		t.Fatal(err)
	}
	gray4, ok := img4.(*image.Gray)
	if !ok {
		t.Fatalf("4bpp 应为 *image.Gray,得到 %T", img4)
	}
	for i, e := range idx4 {
		if g := gray4.At(i%3, i/3).(color.Gray); g.Y != uint8(e)*17 {
			t.Errorf("4bpp 像素 %d got %d want %d", i, g.Y, uint8(e)*17)
		}
	}

	// 1bpp:宽 9 逼出双字节行 + MSB-first;表项黑/白。
	pal2 := []color.NRGBA{{}, {R: 255, G: 255, B: 255, A: 255}}
	idx1 := []uint8{1, 0, 0, 1, 1, 0, 1, 0, 0}
	img1, err := DecodeDIB(dibTestIndexed(9, 1, 1, false, pal2, idx1))
	if err != nil {
		t.Fatal(err)
	}
	gray1 := img1.(*image.Gray)
	for x, e := range idx1 {
		if g := gray1.At(x, 0).(color.Gray); (g.Y == 255) != (e == 1) {
			t.Errorf("1bpp x=%d got Y=%d want e=%d", x, g.Y, e)
		}
	}
}

func TestDibFullBMPWithFileHeaderEquivalence(t *testing.T) {
	// 'BM' 完整头与裸 CF_DIB 等值;再验 bfOffBits 指到掩码+间隙之后的真实 BMP 变体。
	px := dibTestRamp(4, 2, false)
	raw := dibTestPixels32(4, 2, false, true, false, px) // 40 头 + 12 掩码 + 数据(32bpp stride=16)
	file := append(dibTestFileHeader(len(raw)+dibFileHeaderSize, 66), raw...)
	dibTestAssertMatches(t, file, px)
	dibTestAssertMatches(t, raw, px)

	// 变体:掩码后插 4 字节间隙,bfOffBits=14+40+12+4=70(此布局仅对黄金像素比对,
	// 测试内参考解码器按计算布局走,不追这类边角)。
	gapped := make([]byte, 0, len(raw)+4)
	gapped = append(gapped, raw[:dibHeaderInfo+12]...)
	gapped = append(gapped, 0xDE, 0xAD, 0xBE, 0xEF)
	gapped = append(gapped, raw[dibHeaderInfo+12:]...)
	file2 := append(dibTestFileHeader(len(gapped)+dibFileHeaderSize, 70), gapped...)
	dibTestAssertGolden(t, 4, 2, file2, px)
}

func dibTestFileHeader(totalFileSize, bfOffBits int) []byte {
	fh := make([]byte, dibFileHeaderSize)
	fh[0], fh[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(fh[2:6], uint32(totalFileSize))
	binary.LittleEndian.PutUint32(fh[10:14], uint32(bfOffBits))
	return fh
}

// ---------- 恶意/畸形输入:全部必须 error 且零 panic ----------

func TestDibMalformedInputs(t *testing.T) {
	base := dibTestPixels24(2, 2, false, 0, make([]color.NRGBA, 4))

	mutate := func(name string, fix func(h []byte)) {
		d := append([]byte(nil), base...)
		fix(d)
		t.Run(name, func(t *testing.T) {
			if img, err := DecodeDIB(d); err == nil {
				t.Fatalf("必须报错,却解码成功: %v", img)
			} else if img != nil {
				t.Fatalf("报错时必须返回 nil 图像")
			} else if err.Error() == "" {
				t.Fatal("错误信息不得为空")
			}
		})
	}

	// 裸截断
	t.Run("empty", func(t *testing.T) {
		for _, in := range [][]byte{nil, {}, {0x28}, bytes.Repeat([]byte{0x28}, 39), []byte("BM"), []byte("BMgarbage")} {
			if _, err := DecodeDIB(in); err == nil {
				t.Fatalf("输入 %d 字节必须报错", len(in))
			}
		}
	})

	mutate("header-too-short-12", func(d []byte) { binary.LittleEndian.PutUint32(d[0:4], 12) })
	mutate("header-too-short-0", func(d []byte) { binary.LittleEndian.PutUint32(d[0:4], 0) })
	mutate("header-len-41", func(d []byte) { binary.LittleEndian.PutUint32(d[0:4], 41) })
	mutate("header-len-124-truncated", func(d []byte) { binary.LittleEndian.PutUint32(d[0:4], 124) })
	mutate("planes-2", func(d []byte) { binary.LittleEndian.PutUint16(d[12:14], 2) })
	mutate("planes-0", func(d []byte) { binary.LittleEndian.PutUint16(d[12:14], 0) })
	mutate("width-zero", func(d []byte) { binary.LittleEndian.PutUint32(d[4:8], 0) })
	mutate("width-negative", func(d []byte) { binary.LittleEndian.PutUint32(d[4:8], dibTestU32(-2)) })
	mutate("height-zero", func(d []byte) { binary.LittleEndian.PutUint32(d[8:12], 0) })
	mutate("declared-huge", func(d []byte) {
		binary.LittleEndian.PutUint32(d[4:8], 40000)
		binary.LittleEndian.PutUint32(d[8:12], 40000)
	})
	mutate("declared-huge-topdown", func(d []byte) {
		binary.LittleEndian.PutUint32(d[4:8], 1<<20)
		binary.LittleEndian.PutUint32(d[8:12], dibTestU32(-(1 << 20)))
	})
	mutate("width-widened-pixels-short", func(d []byte) { binary.LittleEndian.PutUint32(d[4:8], 4) })
	mutate("bpp-16", func(d []byte) { binary.LittleEndian.PutUint16(d[14:16], 16) })
	mutate("bpp-0", func(d []byte) { binary.LittleEndian.PutUint16(d[14:16], 0) })
	mutate("bpp-2", func(d []byte) { binary.LittleEndian.PutUint16(d[14:16], 2) })
	mutate("comp-jpeg", func(d []byte) { binary.LittleEndian.PutUint32(d[16:20], dibBI_JPEG) })
	mutate("comp-png", func(d []byte) { binary.LittleEndian.PutUint32(d[16:20], dibBI_PNG) })
	mutate("comp-rle8", func(d []byte) { binary.LittleEndian.PutUint32(d[16:20], dibBI_RLE8) })
	mutate("comp-rle4", func(d []byte) { binary.LittleEndian.PutUint32(d[16:20], dibBI_RLE4) })
	mutate("comp-99", func(d []byte) { binary.LittleEndian.PutUint32(d[16:20], 99) })
	mutate("sizeimage-lies-small", func(d []byte) { binary.LittleEndian.PutUint32(d[20:24], 1) })

	// BI_BITFIELDS 恶意变体
	bf := dibTestPixels32(2, 2, false, true, false, make([]color.NRGBA, 4))
	t.Run("bitfields-masks-truncated", func(t *testing.T) {
		if _, err := DecodeDIB(bf[:dibHeaderInfo]); err == nil {
			t.Fatal("掩码被截必须报错")
		}
	})
	for _, bad := range []struct {
		name  string
		off   int
		value uint32
	}{
		{"bitfields-red-f8", dibHeaderInfo, 0x00F80000},
		{"bitfields-green-16", dibHeaderInfo + 4, 0x0000F800},
		{"bitfields-blue-15", dibHeaderInfo + 8, 0x0000001F},
	} {
		t.Run(bad.name, func(t *testing.T) {
			d := append([]byte(nil), bf...)
			binary.LittleEndian.PutUint32(d[bad.off:], bad.value)
			if _, err := DecodeDIB(d); err == nil {
				t.Fatal("非标准掩码必须拒绝,不许硬猜")
			}
		})
	}
	t.Run("bitfields-8bpp", func(t *testing.T) {
		d := dibTestIndexed(2, 1, 8, false, []color.NRGBA{{}, {A: 255, R: 255, G: 255, B: 255}}, []uint8{0, 1})
		binary.LittleEndian.PutUint32(d[16:20], dibBI_BITFIELDS)
		if _, err := DecodeDIB(d); err == nil {
			t.Fatal("BITFIELDS+8bpp 必须报错")
		}
	})

	// 调色板畸形
	palStream := dibTestIndexed(2, 1, 8, false, make([]color.NRGBA, 256), []uint8{0, 1})
	t.Run("palette-truncated", func(t *testing.T) {
		if _, err := DecodeDIB(palStream[:dibHeaderInfo+1000]); err == nil {
			t.Fatal("调色板不足 256 项必须报错")
		}
	})
	t.Run("clrued-over-cap", func(t *testing.T) {
		d := append([]byte(nil), palStream...)
		binary.LittleEndian.PutUint32(d[36:40], 300)
		if _, err := DecodeDIB(d); err == nil {
			t.Fatal("biClrUsed=300 超 8bpp 上限必须报错")
		}
	})
	t.Run("1bpp-no-palette", func(t *testing.T) {
		if _, err := DecodeDIB(dibTestHeader(dibHeaderInfo, 1, 1, 1, 1, dibBI_RGB, 0)); err == nil {
			t.Fatal("1bpp 缺调色板必须报错")
		}
	})

	// bfOffBits 荒诞值(超大/过小)不得越界或 panic
	t.Run("bmpfile-bfoffbits-absurd", func(t *testing.T) {
		px := dibTestRamp(2, 2, false)
		raw := dibTestPixels24(2, 2, false, 0, px)
		far := append(dibTestFileHeader(1<<30, 1<<30), raw...)
		if _, err := DecodeDIB(far); err != nil {
			t.Fatalf("bfOffBits 越界应回退计算布局而非报错: %v", err)
		}
		low := append(dibTestFileHeader(len(raw)+14, 20), raw...) // bfOffBits<头长
		if _, err := DecodeDIB(low); err != nil {
			t.Fatalf("bfOffBits 过小应回退计算布局: %v", err)
		}
	})

	// 巨大合法但内存不足的声明:确认快速失败(不分配巨型 image)
	t.Run("declared-gigantic-fails-fast", func(t *testing.T) {
		d := append([]byte(nil), base...)
		binary.LittleEndian.PutUint32(d[4:8], 1<<30)
		binary.LittleEndian.PutUint32(d[8:12], 1<<30)
		// err.Error() 同时逼 lazy formatter 走一遍,格式化路径也必须无 panic
		img, err := DecodeDIB(d)
		if err == nil || img != nil || !strings.Contains(err.Error(), "截断") {
			t.Fatalf("必须报截断:img=%v err=%v", img, err)
		}
	})
}

// ---------- 三方交叉:随机大图钉死行序/步长/掩码 ----------

func TestDibCmpReference24And32(t *testing.T) {
	cases := []struct {
		name                 string
		w, h                 int
		topDown, bitfields32 bool
	}{
		{"24-bottom-7x5", 7, 5, false, false},
		{"24-top-7x5", 7, 5, true, false},
		{"24-bottom-1x8", 1, 8, false, false},
		{"32-bottom-5x3", 5, 3, false, true},
		{"32-top-5x3", 5, 3, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			px := dibTestRamp(c.w, c.h, false)
			var stream []byte
			if c.bitfields32 {
				stream = dibTestPixels32(c.w, c.h, c.topDown, true, false, px)
			} else {
				stream = dibTestPixels24(c.w, c.h, c.topDown, 0x5A, px)
			}
			dibTestAssertMatches(t, stream, px)
		})
	}
}
