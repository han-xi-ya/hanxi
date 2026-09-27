package clipboard

// R-G2 缩略图管线（契约 §12 v1.7.2）：原图 PNG 字节 → 64px 等比 JPEG(q60) →
// dataURL，随 Entry.Thumb 内嵌 index.json（无独立文件，blob 删除/淘汰无需额外
// 清理）。纯函数零落盘，调用方（store）负责回读 blob 与写回。
//
// 实现取舍（如实声明）：
//   - 只用标准库，禁新增 golang/x/image——缩放手写盒式面积平均（box filter）。
//     长边 ≤64 的目标下，面积平均比最近邻明显少锯齿（下采样信息不丢样），
//     代价是整源逐像素扫描（4K 截图约 10⁷ 次 image.At，单次几十毫秒量级；
//     入库路径每张一次、回填路径后台串行，可接受，不预建金字塔）。
//     放大一律不做（小图原样出图）。
//   - 内存：目标画布 O(64²)；源图解码内存与既有 ingestImage/blob 全解码链路
//     同谱，无新增暴露；解压炸弹另经 DecodeConfig 像素数闸拦截（见 makeThumb）。
//   - 透明像素：JPEG 无 alpha，盒式平均阶段按预乘值聚合后合成到白底
//     （截图必为不透明，透明 PNG 视觉=白，不黑边不失真）。

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // 钉住 PNG 解码器注册（同包 blobs.go 亦显式 import，此处不赌彼处的存在）
)

// thumbDataURLPrefix 前端 img src 直挂的 dataURL 头（契约 R-G2 格式钉死）。
const thumbDataURLPrefix = "data:image/jpeg;base64,"

const (
	thumbMaxEdge      = 64             // 等比缩放长边（契约 R-G2）
	thumbBudgetBytes  = int64(3 << 10) // 单条 dataURL 预算（契约预估 ≤3KB，超则降质量）
	thumbSourceMaxLen = int64(8) << 20 // 原图超 8MiB 跳过生成（钳制与 text 尺寸闸同谱）
	thumbMaxPixels    = int64(1) << 26 // 解码前像素数熔断（~67M px），防 PNG 解压炸弹
)

// thumbQualityLadder 质量阶梯：首档 60 为契约钉死，超预算逐档下探；
// 走到末档仍超预算则如实保留末档结果（缩略图缺失不如缩略图略糊，
// 但绝不为省字节放弃出图——前端布局按定长 64px 槽位设计，糊优于无）。
var thumbQualityLadder = []int{60, 50, 40, 30}

// makeThumb 原图 PNG 字节 → (dataURL, 实际使用质量, error)。
// 一切失败（非 PNG/零尺寸/像素数熔断/编码错）只上浮 error，调用方跳过生成。
func makeThumb(pngBytes []byte) (string, int, error) {
	if len(pngBytes) == 0 {
		return "", 0, errors.New("clipboard/thumbs: 原图字节为空")
	}
	// DecodeConfig 只读 PNG 头部不解码像素，先过尺寸闸再进全量解码。
	cfg, _, err := image.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil {
		return "", 0, fmt.Errorf("clipboard/thumbs: 原图头部解析失败: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return "", 0, fmt.Errorf("clipboard/thumbs: 原图尺寸非法 %d×%d", cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > thumbMaxPixels {
		return "", 0, fmt.Errorf("clipboard/thumbs: 原图像素数超限 %d×%d，跳过缩略图", cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return "", 0, fmt.Errorf("clipboard/thumbs: 原图解码失败: %w", err)
	}
	return encodeThumb(src, thumbBudgetBytes)
}

// encodeThumb 缩放 + JPEG 编码 + dataURL 装配，budget 注入形参供测试压低
// 验证降档通路（生产恒为 thumbBudgetBytes）。
func encodeThumb(src image.Image, budget int64) (string, int, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return "", 0, errors.New("clipboard/thumbs: 源图尺寸为零")
	}
	tw, th := thumbTargetSize(w, h)
	canvas := downscaleBox(src, tw, th)

	dataURL := ""
	used := 0
	for i, q := range thumbQualityLadder {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: q}); err != nil {
			return "", q, fmt.Errorf("clipboard/thumbs: JPEG 编码失败: %w", err)
		}
		dataURL = thumbDataURLPrefix + base64.StdEncoding.EncodeToString(buf.Bytes())
		used = q
		if int64(len(dataURL)) <= budget || i == len(thumbQualityLadder)-1 {
			break // 达标即收；末档仍超则如实保留（取舍见阶梯注释）
		}
	}
	return dataURL, used, nil
}

// thumbTargetSize 等比目标尺寸：长边钳到 64（只缩不放，小图原样）。
func thumbTargetSize(w, h int) (int, int) {
	if w <= thumbMaxEdge && h <= thumbMaxEdge {
		return w, h
	}
	if w >= h {
		return thumbMaxEdge, max(1, thumbMaxEdge*h/w)
	}
	return max(1, thumbMaxEdge*w/h), thumbMaxEdge
}

// downscaleBox 盒式面积平均缩放到 dw×dh（dw≤源宽、dh≤源高，由 thumbTargetSize
// 保证；等尺寸调用即"1:1 拍平到白底不透明 RGBA"）。alpha 全程按预乘值聚合，
// 出图时 over 白底：out = premul + (255-alpha)，一次除法都不要。
func downscaleBox(src image.Image, dw, dh int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		y0 := y * sh / dh
		y1 := (y + 1) * sh / dh
		for x := 0; x < dw; x++ {
			x0 := x * sw / dw
			x1 := (x + 1) * sw / dw
			var sr, sg, sb2, sa, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, bl, a := src.At(sb.Min.X+sx, sb.Min.Y+sy).RGBA() // 16bit 预乘
					sr += uint64(r)
					sg += uint64(g)
					sb2 += uint64(bl)
					sa += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}
			ar, ag, ab, aa := sr/n, sg/n, sb2/n, sa/n
			p := func(sum uint64) uint8 {
				v := sum + (65535 - aa) // 预乘值 over 白（白=65535，系数 1-a）
				r := (v + 128) >> 8     // 16→8bit 四舍入；满值 65535 会进到 256
				if r > 255 {
					r = 255 // 钳回防 uint8 回绕成 0（纯白被画成纯黑即是此坑）
				}
				return uint8(r)
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = p(ar), p(ag), p(ab), 255
		}
	}
	return dst
}
