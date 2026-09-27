package clipboard

// A6 存储底座(二):Windows CF_DIB 解码。契约见
// docs/plans/2026-09-26-clipboard-contract.md §10。
//
// CF_DIB 是**裸 BITMAPINFOHEADER + 位图数据**,没有 BMP 文件的 14 字节
// BITMAPFILEHEADER;本函数同时兼容以 'BM' 魔数开头的完整 BMP(剥头再解),
// 并按头长识别 BITMAPV4HEADER(112)/BITMAPV5HEADER(124)。
//
// 支持矩阵(实现取舍全部落此,与 dib_test 逐条对钉):
//
//	接受:BI_RGB   32bpp(BGRA 字节序;第 4 字节按 GDI 语义为保留值,忽略并置不透明)
//	      BI_RGB   24bpp(BGR→RGB,4 字节行对齐)
//	      BI_RGB    8/4/1bpp 调色板 → 降级 *image.Gray(见 dibLuminance 精度声明)
//	      BI_BITFIELDS 32bpp,掩码必须为标准 R=0x00FF0000/G=0x0000FF00/B=0x000000FF,
//	                   alpha 掩码 ∈ {0(不透明), 0xFF000000(按位抽 alpha)};非标准掩码拒
//	      biHeight<0 top-down / >0 bottom-up 自适配;biPlanes 必须为 1
//	拒绝(error,不猜不硬解):头长 ∉ {40,112,124}(含 BITMAPCOREHEADER 12)、
//	      BI_JPEG/BI_PNG(压缩负载归上游监听线的渲染通道,见下)、BI_RLE4/RLE8、
//	      16bpp(含 BITFIELDS 5-5-5/5-6-5)、BITFIELDS 非 32bpp、planes≠1、
//	      宽/高非法、声明尺寸×bpp 超出实际字节、biSizeImage 虚报、调色板截断
//
// BI_JPEG/BI_PNG 说明:Windows 某些应用(Office 系)会以 DIB 容器装 JPEG/PNG
// 负载,剪贴板监听线(A2)对这类条目另走渲染/直存通道,DecodeDIB 只做解码器
// 职责边界内的裸位图,遇到即 fail-loud,绝不上浮猜测。
//
// 输出类型:*image.NRGBA(24/32bpp;顶层带真 alpha——BI_BITFIELDS 带 alpha
// 掩码时为剪贴板源的非预乘值)或 *image.Gray(≤8bpp 调色板降级)。

import (
	"encoding/binary"
	"fmt"
	"image"
)

const (
	dibFileHeaderSize = 14 // BITMAPFILEHEADER('BM' 前缀)

	dibHeaderInfo = 40  // BITMAPINFOHEADER
	dibHeaderV4   = 112 // BITMAPV4HEADER
	dibHeaderV5   = 124 // BITMAPV5HEADER

	dibBI_RGB       = 0
	dibBI_RLE8      = 1
	dibBI_RLE4      = 2
	dibBI_BITFIELDS = 3
	dibBI_JPEG      = 4
	dibBI_PNG       = 5
)

// BITFIELDS 标准掩码(32bpp 小端像素 DWORD = B | G<<8 | R<<16 | A<<24)。
const (
	dibStdMaskRed   = 0x00FF0000
	dibStdMaskGreen = 0x0000FF00
	dibStdMaskBlue  = 0x000000FF
	dibStdMaskAlpha = 0xFF000000
)

// DecodeDIB CF_DIB(或完整 BMP)字节流 → image.Image。
// 越界输入 fail-loud:先做尺寸×步长的总量校验,再进解码循环,
// 循环内所有切片索引都被预检覆盖,绝不读脏内存。
func DecodeDIB(dib []byte) (image.Image, error) {
	body := dib
	bfOffBits := -1 // 完整 BMP 才有;裸 CF_DIB 无从声明数据偏移

	// 'BM' 前缀识别:裸 BITMAPINFOHEADER 首 DWORD 恒为 40/112/124(0x28/0x70/0x7C),
	// 前两字节不可能是 'B','M',误判无可能。
	if len(body) >= dibFileHeaderSize+4 && body[0] == 'B' && body[1] == 'M' {
		bfOffBits = int(binary.LittleEndian.Uint32(body[10:14]))
		body = body[dibFileHeaderSize:]
	}

	if len(body) < dibHeaderInfo {
		return nil, fmt.Errorf("clipboard/dib: 数据仅 %d 字节,不足一个 BITMAPINFOHEADER(%d 字节)", len(body), dibHeaderInfo)
	}
	biSize := int(binary.LittleEndian.Uint32(body[0:4]))
	switch biSize {
	case dibHeaderInfo, dibHeaderV4, dibHeaderV5:
	default:
		return nil, fmt.Errorf("clipboard/dib: 不支持的头长 %d(仅接受 40/112/124,BITMAPCOREHEADER 等同旧格式不实现)", biSize)
	}
	if len(body) < biSize {
		return nil, fmt.Errorf("clipboard/dib: 截断:头声明 %d 字节,实际流只有 %d", biSize, len(body))
	}

	width := int(int32(binary.LittleEndian.Uint32(body[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(body[8:12])))
	planes := binary.LittleEndian.Uint16(body[12:14])
	bitCount := int(binary.LittleEndian.Uint16(body[14:16]))
	compression := binary.LittleEndian.Uint32(body[16:20])
	sizeImage := int64(binary.LittleEndian.Uint32(body[20:24]))
	clrUsed := int(binary.LittleEndian.Uint32(body[36:40]))

	switch {
	case width <= 0:
		return nil, fmt.Errorf("clipboard/dib: 非法宽度 %d(必须 >0)", width)
	case height == 0:
		return nil, fmt.Errorf("clipboard/dib: 非法高度 0")
	case planes != 1:
		return nil, fmt.Errorf("clipboard/dib: biPlanes=%d,位图必须为 1", planes)
	}
	topDown := height < 0
	h := height
	if topDown {
		h = -height
	}

	var hasAlpha bool
	switch compression {
	case dibBI_RGB:
	case dibBI_BITFIELDS:
		if bitCount != 32 {
			return nil, fmt.Errorf("clipboard/dib: BI_BITFIELDS 仅实现 32bpp(收到 %dbpp)", bitCount)
		}
		var red, green, blue, alpha uint32
		if biSize == dibHeaderInfo {
			// 经典形态:3 个 DWORD 掩码紧随头后,占用调色板表前 3 项
			if len(body) < biSize+12 {
				return nil, fmt.Errorf("clipboard/dib: 截断:BI_BITFIELDS 掩码缺位")
			}
			red = binary.LittleEndian.Uint32(body[biSize:])
			green = binary.LittleEndian.Uint32(body[biSize+4:])
			blue = binary.LittleEndian.Uint32(body[biSize+8:])
		} else {
			// V4/V5:掩码在头内固定偏移 40/44/48/52(R,G,B,A 序)
			red = binary.LittleEndian.Uint32(body[40:])
			green = binary.LittleEndian.Uint32(body[44:])
			blue = binary.LittleEndian.Uint32(body[48:])
			alpha = binary.LittleEndian.Uint32(body[52:])
		}
		if red != dibStdMaskRed || green != dibStdMaskGreen || blue != dibStdMaskBlue ||
			(alpha != 0 && alpha != dibStdMaskAlpha) {
			return nil, fmt.Errorf("clipboard/dib: BI_BITFIELDS 非标准掩码组合 R=%#010X G=%#010X B=%#010X A=%#010X,拒绝猜测",
				red, green, blue, alpha)
		}
		hasAlpha = alpha == dibStdMaskAlpha
	case dibBI_JPEG, dibBI_PNG:
		return nil, fmt.Errorf("clipboard/dib: BI_JPEG/BI_PNG 压缩负载不归解码器(上游监听线另走渲染通道)")
	case dibBI_RLE8, dibBI_RLE4:
		return nil, fmt.Errorf("clipboard/dib: BI_RLE4/RLE8 行程压缩未实现(剪贴板实践中罕见)")
	default:
		return nil, fmt.Errorf("clipboard/dib: 未知压缩类型 %d", compression)
	}

	switch bitCount {
	case 1, 4, 8, 24, 32:
	default:
		return nil, fmt.Errorf("clipboard/dib: 不支持的位深 %d(16bpp 未实现)", bitCount)
	}

	// ---- 数据起点:头 (+掩码) (+调色板);完整 BMP 的 bfOffBits 在合理区间时优先
	dataOff := biSize
	if compression == dibBI_BITFIELDS && biSize == dibHeaderInfo {
		dataOff += 12
	}
	var lut [256]uint8
	if bitCount <= 8 {
		n := clrUsed
		if n == 0 {
			n = 1 << bitCount
		}
		if n > 1<<bitCount {
			return nil, fmt.Errorf("clipboard/dib: biClrUsed=%d 超出 %dbpp 索引上限", clrUsed, bitCount)
		}
		if len(body) < dataOff+n*4 {
			return nil, fmt.Errorf("clipboard/dib: 截断:调色板需 %d 字节,流只剩 %d", n*4, len(body)-dataOff)
		}
		// 未声明到的 LUT 槽位保持 0:biClrUsed 少于全表时,高位索引属非法数据,
		// 映射为黑而非越界或连坐整图(与"单条坏数据不毁库"的读侧纪律同谱)。
		for i := 0; i < n; i++ {
			p := body[dataOff+i*4:]
			lut[i] = dibLuminance(p[2], p[1], p[0]) // 表项 BGRA
		}
		dataOff += n * 4
	}
	if bfOff := bfOffBits - dibFileHeaderSize; bfOffBits >= 0 && bfOff >= dataOff && bfOff <= len(body) {
		dataOff = bfOff // 真实 BMP 常在此埋掩码/间隙,以文件头声明为准
	}

	// ---- 越界总闸:声明尺寸 × bpp 必须装得进流(先于一切分配与解码)
	stride := (int64(width)*int64(bitCount) + 31) / 32 * 4
	remaining := int64(len(body)) - int64(dataOff)
	if remaining < 0 {
		return nil, fmt.Errorf("clipboard/dib: 数据偏移 %d 越出流长 %d", dataOff, len(body))
	}
	// 判定用除法而非乘法:声明尺寸极大时 stride*h 可溢出 int64,除法比较恒安全
	if int64(h) > remaining/stride {
		return nil, fmt.Errorf("clipboard/dib: 截断:%dx%d@%dbpp 步长 %d,流内只剩 %d 字节,连一行都装不满全部行",
			width, h, bitCount, stride, remaining)
	}
	if sizeImage > 0 && sizeImage < stride*int64(h) {
		return nil, fmt.Errorf("clipboard/dib: biSizeImage=%d 与声明尺寸所需 %d 矛盾", sizeImage, stride*int64(h))
	}

	// ---- 解码
	if bitCount <= 8 {
		out := image.NewGray(image.Rect(0, 0, width, h))
		for y := 0; y < h; y++ {
			srcY := y
			if !topDown {
				srcY = h - 1 - y
			}
			row := body[int64(dataOff)+int64(srcY)*stride:]
			dst := out.Pix[y*out.Stride:]
			switch bitCount {
			case 8:
				for x := 0; x < width; x++ {
					dst[x] = lut[row[x]]
				}
			case 4:
				for x := 0; x < width; x += 2 {
					v := row[x/2]
					dst[x] = lut[v>>4]
					if x+1 < width {
						dst[x+1] = lut[v&0x0F]
					}
				}
			case 1:
				for x := 0; x < width; x++ {
					bit := (row[x/8] >> (7 - uint(x%8))) & 1
					dst[x] = lut[bit]
				}
			}
		}
		return out, nil
	}

	out := image.NewNRGBA(image.Rect(0, 0, width, h))
	for y := 0; y < h; y++ {
		srcY := y
		if !topDown {
			srcY = h - 1 - y
		}
		row := body[int64(dataOff)+int64(srcY)*stride:]
		dst := out.Pix[y*out.Stride:]
		if bitCount == 32 {
			for x := 0; x < width; x++ {
				v := binary.LittleEndian.Uint32(row[x*4:])
				p := dst[x*4:]
				p[0] = byte(v >> 16) // 标准掩码:R 在 bits16-23
				p[1] = byte(v >> 8)  // G 在 bits8-15
				p[2] = byte(v)       // B 在 bits0-7
				if hasAlpha {
					p[3] = byte(v >> 24) // A 在 bits24-31(剪贴板源非预乘)
				} else {
					p[3] = 0xFF // BI_RGB 32bpp 第 4 字节为 GDI 保留值,一律不透明
				}
			}
		} else { // 24bpp: BGR 行内序,4 字节行对齐已在 stride 处理
			for x := 0; x < width; x++ {
				p := dst[x*4:]
				p[0] = row[x*3+2]
				p[1] = row[x*3+1]
				p[2] = row[x*3]
				p[3] = 0xFF
			}
		}
	}
	return out, nil
}

// dibLuminance 调色板降灰:Rec.601 整型近似 Y=(77R+150G+29B+128)>>8。
// 精度取舍(契约明示允许):色度全丢,亮暗保真;灰阶调色板(r=g=b)时结果
// 精确等于原值((系数和=256,舍入项 128 恰落回 v))。剪贴板截图/图章类
// DIB 几乎全是 BI_RGB,此路径仅兜住老式 256 色以下产物,换取零依赖解码。
func dibLuminance(r, g, b uint8) uint8 {
	return uint8((77*uint32(r) + 150*uint32(g) + 29*uint32(b) + 128) >> 8)
}
