package peicon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- 最小 PE 资源夹具 ----------
//
// 布局：四层资源目录树（类型→名字→语言→数据）+ 各数据条目后紧跟其负载
// （解析器按绝对偏移寻址，对布局零假设），DataEntry.OffsetToData 按标准
// 模块 RVA 口径。外壳是单节 .rsrc 的最小 PE32/PE32+，parsePE 所需字段
// 全部如实填充。

const (
	fixtureRsrcVA    = uint32(0x2000)
	fixtureSectionSz = uint32(0x400) // .rsrc 文件原始偏移（对齐字段本解析器不校验）
)

type resData struct {
	langID  uint32
	payload []byte
}

type resName struct {
	id    uint32
	langs []resData
}

type resType struct {
	id    uint32
	names []resName
}

// buildRsrc 深度优先序列化：写一层目录头 → 预留条目表 → 递归子层 → 回写条目。
func buildRsrc(types []resType) []byte {
	var buf bytes.Buffer
	patch := func(off int, v uint32) { binary.LittleEndian.PutUint32(buf.Bytes()[off:], v) }
	writeDirHeader := func(count int) {
		binary.Write(&buf, binary.LittleEndian, uint32(0)) // Characteristics
		binary.Write(&buf, binary.LittleEndian, uint32(0)) // TimeDateStamp
		binary.Write(&buf, binary.LittleEndian, uint16(0)) // 版本
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, uint16(0))     // NumberOfNamedEntries
		binary.Write(&buf, binary.LittleEndian, uint16(count)) // NumberOfIdEntries
	}
	writeDirHeader(len(types))
	rootEntries := buf.Len()
	buf.Write(make([]byte, 8*len(types)))
	for i, ty := range types {
		patch(rootEntries+i*8, ty.id)
		patch(rootEntries+i*8+4, uint32(buf.Len())|0x80000000)
		writeDirHeader(len(ty.names))
		typeEntries := buf.Len()
		buf.Write(make([]byte, 8*len(ty.names)))
		for j, nm := range ty.names {
			patch(typeEntries+j*8, nm.id)
			patch(typeEntries+j*8+4, uint32(buf.Len())|0x80000000)
			writeDirHeader(len(nm.langs))
			nameEntries := buf.Len()
			buf.Write(make([]byte, 8*len(nm.langs)))
			for k, d := range nm.langs {
				patch(nameEntries+k*8, d.langID)
				patch(nameEntries+k*8+4, uint32(buf.Len())|0x80000000)
				// 语言层：单条目目录，条目直指数据条目（无高位标记）。
				writeDirHeader(1)
				langEntry := buf.Len()
				buf.Write(make([]byte, 8))
				// 数据条目：占位后回填 RVA/Size，负载紧跟。
				dataEntry := buf.Len()
				buf.Write(make([]byte, 16))
				payloadOff := buf.Len()
				buf.Write(d.payload)
				patch(langEntry, d.langID)
				patch(langEntry+4, uint32(dataEntry))
				patch(dataEntry, fixtureRsrcVA+uint32(payloadOff))
				patch(dataEntry+4, uint32(len(d.payload)))
			}
		}
	}
	return buf.Bytes()
}

// buildPE 把 .rsrc 段包进一个最小 PE；pe32plus 选择 Optional Header 布局。
func buildPE(t *testing.T, rsrc []byte, pe32plus bool) []byte {
	t.Helper()
	file := make([]byte, int(fixtureSectionSz)+len(rsrc))
	file[0], file[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(file[0x3C:], 0x40)
	copy(file[0x40:], []byte("PE\x00\x00"))
	binary.LittleEndian.PutUint16(file[0x46:], 1) // NumberOfSections
	sizeOpt := 224
	if pe32plus {
		sizeOpt = 240
	}
	binary.LittleEndian.PutUint16(file[0x54:], uint16(sizeOpt))
	opt := 0x58
	magic := uint16(0x10b)
	dataDirOff := 96
	if pe32plus {
		magic = 0x20b
		dataDirOff = 112
	}
	binary.LittleEndian.PutUint16(file[opt:], magic)
	binary.LittleEndian.PutUint32(file[opt+dataDirOff-4:], 16)             // 目录数
	binary.LittleEndian.PutUint32(file[opt+dataDirOff+16:], fixtureRsrcVA) // 目录 #2 = RESOURCE
	binary.LittleEndian.PutUint32(file[opt+dataDirOff+20:], uint32(len(rsrc)))
	sec := opt + sizeOpt
	copy(file[sec:sec+8], []byte(".rsrc   "))
	binary.LittleEndian.PutUint32(file[sec+8:], uint32(len(rsrc)))  // VirtualSize
	binary.LittleEndian.PutUint32(file[sec+12:], fixtureRsrcVA)     // VirtualAddress
	binary.LittleEndian.PutUint32(file[sec+16:], uint32(len(rsrc))) // SizeOfRawData
	binary.LittleEndian.PutUint32(file[sec+20:], fixtureSectionSz)  // PointerToRawData
	copy(file[int(fixtureSectionSz):], rsrc)
	return file
}

// rtGroupIconFixture 是 RT_GROUP_ICON 类型号：夹具故意在树里挂组目录，
// 钉死"组目录不圈定候选"的行为（真机 RAMMap64.exe 即组外孤儿帧更大）。
const rtGroupIconFixture = 14

func pngFixture(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 10, G: 200, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("构造 PNG 夹具失败: %v", err)
	}
	return buf.Bytes()
}

// dibFixture32 构造 32bpp BITMAPINFOHEADER 图标帧：色面自下而上、
// AND 掩码自下而上（置位=透明）；biHeight 按惯例取色面两倍。
func dibFixture32(size int) []byte {
	stride := size * 4
	maskStride := ((size + 31) & ^31) / 8
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(size))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(size*2))
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	xor := make([]byte, stride*size)
	for i := range xor[:size*size] {
		xor[i*4+0] = 0x30 // B
		xor[i*4+1] = 0x20 // G
		xor[i*4+2] = 0xC8 // R
		xor[i*4+3] = 0xFF // A
	}
	mask := make([]byte, maskStride*size)
	mask[len(mask)-maskStride] = 0x80 // 自下而上最后一条掩码行首位置位 = 视觉左上角透明
	var buf bytes.Buffer
	buf.Write(hdr)
	buf.Write(xor)
	buf.Write(mask)
	return buf.Bytes()
}

type grpMemberSpec struct{ w, h, bit, bytes, id uint16 }

func grpFixture(members ...grpMemberSpec) []byte {
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint16(buf[2:], 1)
	binary.LittleEndian.PutUint16(buf[4:], uint16(len(members)))
	for _, m := range members {
		e := make([]byte, 14)
		e[0] = byte(m.w) // 256 在协议里记 0，夹具只用小尺寸
		e[1] = byte(m.h)
		binary.LittleEndian.PutUint16(e[4:], 1)
		binary.LittleEndian.PutUint16(e[6:], m.bit)
		binary.LittleEndian.PutUint16(e[8:], m.bytes)
		binary.LittleEndian.PutUint16(e[12:], m.id)
		buf = append(buf, e...)
	}
	return buf
}

// ---------- 表驱动测试 ----------

func TestExtractFrames(t *testing.T) {
	t.Run("组不圈定候选且PNG帧透传最大画幅优先", func(t *testing.T) {
		bigPNG := pngFixture(t, 32)
		smallDIB := dibFixture32(4)
		rsrc := buildRsrc([]resType{
			{id: rtIcon, names: []resName{
				{id: 1, langs: []resData{{payload: smallDIB}}},
				{id: 2, langs: []resData{{payload: bigPNG}}},
			}},
			// 组目录故意只登记小帧：解析器不得被组圈定，孤儿 32² PNG 仍须胜出。
			{id: rtGroupIconFixture, names: []resName{
				{id: 101, langs: []resData{{payload: grpFixture(
					grpMemberSpec{w: 4, h: 4, bit: 32, bytes: uint16(len(smallDIB)), id: 1},
				)}}},
			}},
		})
		icon, err := ExtractBytes(buildPE(t, rsrc, false))
		if err != nil {
			t.Fatalf("提取失败: %v", err)
		}
		if icon.Format != "png" || icon.Width != 32 || icon.Height != 32 {
			t.Fatalf("画幅/帧型不符: %+v", icon)
		}
		if !bytes.Equal(icon.PNG, bigPNG) {
			t.Fatal("PNG 帧必须原样透传，不得重编码")
		}
	})

	t.Run("DIB帧转码PNG且掩码生效", func(t *testing.T) {
		dib := dibFixture32(8)
		rsrc := buildRsrc([]resType{
			{id: rtIcon, names: []resName{{id: 1, langs: []resData{{payload: dib}}}}},
		})
		icon, err := ExtractBytes(buildPE(t, rsrc, false))
		if err != nil {
			t.Fatalf("提取失败: %v", err)
		}
		if icon.Format != "dib" || icon.Width != 8 || icon.Height != 8 {
			t.Fatalf("画幅/帧型不符: %+v", icon)
		}
		img, err := png.Decode(bytes.NewReader(icon.PNG))
		if err != nil {
			t.Fatalf("转码产物非有效 PNG: %v", err)
		}
		if b := img.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
			t.Fatalf("解码尺寸不符: %v", b)
		}
		nrgba, ok := img.(*image.NRGBA)
		if !ok {
			t.Fatalf("解码像素型不符: %T", img)
		}
		// 左上角被 AND 掩码置透明；其余不透明且 BGRA 映射正确。
		if a := nrgba.NRGBAAt(0, 0).A; a != 0 {
			t.Fatalf("掩码位应透明, got A=%d", a)
		}
		c := nrgba.NRGBAAt(7, 0)
		if c.R != 0xC8 || c.G != 0x20 || c.B != 0x30 || c.A != 0xFF {
			t.Fatalf("像素通道不符: %+v", c)
		}
	})

	t.Run("无组时散帧兜底", func(t *testing.T) {
		dib := dibFixture32(6)
		rsrc := buildRsrc([]resType{
			{id: rtIcon, names: []resName{{id: 1, langs: []resData{{payload: dib}}}}},
		})
		icon, err := ExtractBytes(buildPE(t, rsrc, true)) // 顺带覆盖 PE32+ 目录偏移
		if err != nil {
			t.Fatalf("提取失败: %v", err)
		}
		if icon.Width != 6 {
			t.Fatalf("散帧兜底画幅不符: %+v", icon)
		}
	})

	t.Run("无图标资源如实报错", func(t *testing.T) {
		rsrc := buildRsrc([]resType{
			{id: 16, names: []resName{{id: 1, langs: []resData{{payload: []byte("versioninfo")}}}}},
		})
		_, err := ExtractBytes(buildPE(t, rsrc, false))
		if err == nil || !strings.Contains(err.Error(), "无图标资源") {
			t.Fatalf("期望无图标资源错误, got %v", err)
		}
	})

	t.Run("坏PE四路", func(t *testing.T) {
		rsrcMissing := buildPE(t, buildRsrc(nil), false)
		binary.LittleEndian.PutUint32(rsrcMissing[0xB8+16:], 0) // 清空资源目录 RVA
		cases := map[string][]byte{
			"过短":     []byte("MZ"),
			"无MZ":    bytes.Repeat([]byte{0}, 0x100),
			"PE签名坏":  func() []byte { b := buildPE(t, []byte{0}, false); b[0x40] = 'X'; return b }(),
			"rsrc缺失": rsrcMissing,
		}
		for name, data := range cases {
			t.Run(name, func(t *testing.T) {
				if _, err := ExtractBytes(data); err == nil {
					t.Fatal("期望错误，实际静默成功")
				}
			})
		}
	})
}

// TestExtractRealRAMMap 用机主本机已装的官方 RAMMap 真件验证画幅可得性。
// 未安装环境（版本目录不在位）如实 skip——提取通道不可用是合法常态。
func TestExtractRealRAMMap(t *testing.T) {
	matches, err := filepath.Glob(filepath.FromSlash("../../../bin/hanxidata/versions/rammap_*/RAMMap64.exe"))
	if err != nil {
		t.Fatalf("glob 失败: %v", err)
	}
	var hit string
	for _, m := range matches {
		if _, err := os.Stat(m); err == nil {
			hit = m
			break
		}
	}
	if hit == "" {
		t.Skip("本机未安装托管 RAMMap（versions/rammap_*/RAMMap64.exe 不在位），跳过真件集成")
	}
	icon, err := Extract(hit)
	if err != nil {
		t.Fatalf("真件提取失败: %v", err)
	}
	t.Logf("RAMMap64.exe 图标真实画幅 %dx%d（%s 帧，PNG %d 字节）", icon.Width, icon.Height, icon.Format, len(icon.PNG))
	if icon.Width < 16 {
		t.Errorf("真件画幅异常: %dx%d", icon.Width, icon.Height)
	}
	if !bytes.HasPrefix(icon.PNG, pngMagic) {
		t.Error("出货字节必须以 PNG 签名开头")
	}
}
