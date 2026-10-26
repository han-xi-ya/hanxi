package peicon

// A1 审查批次：32bpp 帧的 clrUsed 遗留字段不得驱动建表。
//
// 外部可观察事实（不是实现镜像）：一枚合法 32bpp 图标——位深 >8 本无色表，
// 但真实打包器常把调色板项数照填进 clrUsed——必须能完整提取，且解出的像素
// 就是 BGRA 色面声明的颜色（AND 掩码覆盖处透明）。旧实现按 clrUsed=256
// 索引越界，ExtractBytes 只余 recover 兜底的"解析越界"误拒。

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"testing"

	"hanxi/packages/go/peicon/petest"
)

func TestExtractDIB32WithGarbageClrUsed(t *testing.T) {
	payload := petest.DIB32(8, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	// BITMAPINFOHEADER 偏移 32 是 biClrUsed：32bpp 无调色板，此处塞历史遗留值。
	binary.LittleEndian.PutUint32(payload[32:36], 256)

	rsrc := petest.BuildRsrc([]petest.Type{{
		ID: petest.IconType,
		Names: []petest.Name{{
			ID:    1,
			Langs: []petest.Data{{LangID: 1033, Payload: payload}},
		}},
	}})
	pe := petest.BuildPE(rsrc, false)

	icon, err := ExtractBytes(pe)
	if err != nil {
		t.Fatalf("32bpp 图标不得因遗留字段脏值被误拒: %v", err)
	}
	if icon.Width != 8 || icon.Height != 8 || icon.Format != "dib" {
		t.Fatalf("帧信息 = %dx%d/%s, want 8x8/dib", icon.Width, icon.Height, icon.Format)
	}

	img, derr := png.Decode(bytes.NewReader(icon.PNG))
	if derr != nil {
		t.Fatalf("出货 PNG 不可解码: %v", derr)
	}
	if img.Bounds().Dx() != 8 || img.Bounds().Dy() != 8 {
		t.Fatalf("画幅 = %v, want 8x8", img.Bounds())
	}
	r, g, b, a := img.At(3, 4).RGBA()
	if got := [4]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}; got != [4]uint8{10, 20, 30, 255} {
		t.Fatalf("普通像素 RGBA = %v, want [10 20 30 255]", got)
	}
	if _, _, _, a0 := img.At(0, 0).RGBA(); a0 != 0 {
		t.Fatal("AND 掩码覆盖的左上像素必须透明")
	}
}

// TestExtractDIB4BbpHonorsClrUsed 反向锁死：位深 ≤8 时 clrUsed 仍是有效表长
// 口径（截短到 4 色则表后只读 4 项，越界索引如实报错而非静默读色面）。
func TestExtractDIB4bppHonorsClrUsed(t *testing.T) {
	// 手工 4bpp 帧：hdr(40)+16 项色表+2x2 色面。DIB 行按 32 位对齐：
	// 2px×4bpp=8bit → 每行补到 4 字节；AND 掩码同理 2 行各 4 字节。
	size := 2
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(size))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(size*2)) // 色面高×2（含 AND 掩码惯例）
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 4)
	binary.LittleEndian.PutUint32(hdr[32:], 16) // 合法表长
	pal := make([]byte, 16*4)
	// 表项 1 = 红色（RGBQUAD：B G R 预留）
	pal[4], pal[5], pal[6], pal[7] = 0, 0, 200, 255
	// 两个像素都是索引 1（高/低 nibble 均 1），行内其余为对齐填充
	xor := []byte{0x11, 0, 0, 0, 0x11, 0, 0, 0}
	mask := make([]byte, 8) // 2 行 × 4 字节，全 0 = 不遮蔽
	payload := append(append(append(hdr, pal...), xor...), mask...)

	rsrc := petest.BuildRsrc([]petest.Type{{
		ID:    petest.IconType,
		Names: []petest.Name{{ID: 1, Langs: []petest.Data{{LangID: 1033, Payload: payload}}}},
	}})
	icon, err := ExtractBytes(petest.BuildPE(rsrc, false))
	if err != nil {
		t.Fatalf("4bpp 常规帧提取失败: %v", err)
	}
	img, derr := png.Decode(bytes.NewReader(icon.PNG))
	if derr != nil {
		t.Fatalf("出货 PNG 不可解码: %v", derr)
	}
	r, g, b, _ := img.At(1, 1).RGBA()
	if uint8(r>>8) != 200 || uint8(g>>8) != 0 || uint8(b>>8) != 0 {
		t.Fatalf("表项 1 未生效: RGBA=%d,%d,%d", r>>8, g>>8, b>>8)
	}
}
