package snip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// DecodeDIBToPNG 把剪贴板 CF_DIB 字节流（BITMAPINFOHEADER + 像素，可能含调色板）
// 解码并转 PNG。覆盖 BI_RGB 的 1/4/8/24/32 位与负高度 top-down 行序；
// BI_BITFIELDS 仅接受 32 位标准 BGRA 掩码，其余组合明确报「格式不支持」。
func DecodeDIBToPNG(dib []byte) ([]byte, error) {
	if len(dib) < 40 {
		return nil, fmt.Errorf("DIB 头过短(%d B)", len(dib))
	}
	headerSize := binary.LittleEndian.Uint32(dib[0:4])
	if headerSize != 40 { // BITMAPCOREHEADER(12) 等远古格式现实中不会来自截屏工具
		return nil, fmt.Errorf("不支持的 DIB 头类型 size=%d（仅支持 BITMAPINFOHEADER）", headerSize)
	}
	width := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(dib[8:12]))) // 负值 = top-down
	bottomUp := height > 0
	if width <= 0 || height == 0 {
		return nil, fmt.Errorf("DIB 尺寸非法 %dx%d", width, height)
	}
	if height < 0 {
		height = -height
	}
	bitCount := binary.LittleEndian.Uint16(dib[14:16])
	compression := binary.LittleEndian.Uint32(dib[16:20])
	// BITMAPINFOHEADER 布局：28..32 biClrUsed，32..36 biClrImportant
	clrUsed := int(binary.LittleEndian.Uint32(dib[28:32]))
	pixelOff := 40
	var palette []color.RGBA
	switch bitCount {
	case 1, 4, 8:
		if compression != 0 {
			return nil, fmt.Errorf("不支持的调色板 DIB 压缩方式 %d", compression)
		}
		n := clrUsed
		if n == 0 {
			n = 1 << bitCount
		}
		need := pixelOff + n*4
		if len(dib) < need {
			return nil, fmt.Errorf("DIB 调色板不完整（需 %d B 实得 %d B）", need, len(dib))
		}
		palette = make([]color.RGBA, n)
		for i := 0; i < n; i++ {
			b, g, r := dib[pixelOff+i*4], dib[pixelOff+i*4+1], dib[pixelOff+i*4+2]
			palette[i] = color.RGBA{R: r, G: g, B: b, A: 255}
		}
		pixelOff += n * 4
	case 24:
		if compression != 0 {
			return nil, fmt.Errorf("24 位 DIB 不支持压缩方式 %d", compression)
		}
	case 32:
		// BI_RGB 下第 4 字节在剪贴板 DIB 里常为 0，按不透明处理；
		// BI_BITFIELDS 校验标准 BGRA 掩码（0x00FF0000/0xFF00/0xFF）。
		if compression != 0 {
			if compression != 3 || len(dib) < pixelOff+12 ||
				binary.LittleEndian.Uint32(dib[40:44]) != 0x00FF0000 ||
				binary.LittleEndian.Uint32(dib[44:48]) != 0x0000FF00 ||
				binary.LittleEndian.Uint32(dib[48:52]) != 0x000000FF {
				return nil, fmt.Errorf("32 位 DIB 不支持压缩/掩码组合(compression=%d)", compression)
			}
			pixelOff += 12
		}
	default:
		return nil, fmt.Errorf("不支持的 DIB 位深 %d（截屏请经 Hanxi 文字识别页反馈）", bitCount)
	}

	stride := ((width*int(bitCount) + 31) / 32) * 4 // 行 4 字节对齐
	total := stride * height
	if len(dib) < pixelOff+total {
		return nil, fmt.Errorf("DIB 像素数据不完整（需 %d B 实得 %d B）", pixelOff+total, len(dib))
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for row := 0; row < height; row++ {
		src := pixelOff + row*stride
		dst := row
		if bottomUp {
			dst = height - 1 - row
		}
		base := img.PixOffset(0, dst)
		switch bitCount {
		case 32:
			for x := 0; x < width; x++ {
				o, p := base+x*4, src+x*4
				img.Pix[o+0], img.Pix[o+1], img.Pix[o+2] = dib[p+2], dib[p+1], dib[p+0]
				if a := dib[p+3]; a != 0 {
					img.Pix[o+3] = a
				} else {
					img.Pix[o+3] = 255 // 剪贴板 DIB 的 alpha 字节未填时按不透明
				}
			}
		case 24:
			for x := 0; x < width; x++ {
				o, p := base+x*4, src+x*3
				img.Pix[o+0], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = dib[p+2], dib[p+1], dib[p+0], 255
			}
		case 8:
			for x := 0; x < width; x++ {
				c := palette[dib[src+x]]
				img.SetRGBA(x, dst, c)
			}
		case 4:
			for x := 0; x < width; x++ {
				b := dib[src+x/2]
				if x%2 == 0 {
					b >>= 4
				}
				img.SetRGBA(x, dst, palette[b&0x0F])
			}
		case 1:
			for x := 0; x < width; x++ {
				b := dib[src+x/8]
				if (b>>(7-uint(x%8)))&1 == 0 {
					b = 0
				} else {
					b = 1
				}
				img.SetRGBA(x, dst, palette[b])
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("PNG 编码失败: %w", err)
	}
	return buf.Bytes(), nil
}
