// Package peicon 纯 Go 解析 Windows PE（.exe/.dll）内嵌图标资源：枚举
// RT_GROUP_ICON(14)/RT_ICON(3)，取**最大真实画幅**一帧输出 PNG 字节。
//
// 缘起（N27 红线图标运行期本机提取通道）：rammap/recordly/vscode 三枚因
// 微软/AGPL 品牌许可红线永不入仓，改由运行期从机主本机已装的官方 exe 就地
// 提取、仅本地使用（不分发不入库，见 docs/THIRD_PARTY_NOTICES.md「运行期
// 本机提取」节）。先例为 scripts/extract_app_icons.ps1 的 PowerShell 通道
// （LoadLibraryEx+EnumResourceNames 证过画幅可得性），运行期必须 Go 实现，
// 本包即其纯标准库对应物（零新增依赖）。
//
// 语义与纪律：
//   - 画幅选择 = 面积最大者；Vista+ 的 PNG 内嵌帧（256² 常见）原样透传字节，
//     DIB 帧（BITMAPINFOHEADER，1/4/8/24/32bpp）自解码转 32bpp PNG；
//     绝不放大也不重编码透传帧；
//   - 候选集 = 全量 RT_ICON 帧（PS 脚本 EnumResourceNames 同谱），组目录
//     不圈定候选——真机实证 RAMMap64.exe 存在"32² 帧不入组"的孤儿画幅，
//     按组取最大会把可得画幅无谓砍半；
//   - 任何解析失败（非 PE、无图标资源、位深不支持、资源越界）都如实返回
//     error，调用方（runtimeicon 服务）记负缓存并让前端回落矢量徽标——
//     宁缺毋滥，绝不伪造"像"的图标；
//   - 只读一个文件，不产生任何子进程/网络 IO；对损坏 PE 的全部切片访问都
//     带边界判定，并以 recover 兜底防越界 panic 炸宿主。
package peicon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

// 资源类型常量（WinUser.h / WINNT.h）。RT_GROUP_ICON(14) 不参与候选圈定，
// 仅在类型目录中如实略过。
const rtIcon = 3

// 上限防御：正常图标资源远小于此（单帧 256² 32bpp ≈ 262KB，PNG 帧 <100KB）。
const (
	maxResourceBytes = 8 << 20 // 单个 RT_ICON 资源最大 8MB
	maxIconDim       = 1024    // 可接受的最大边长
	maxDirEntries    = 4096    // 单层资源目录项数上限
	dirEntrySize     = 8
	dataEntrySize    = 16
)

// pngMagic 是 PNG 签名（Vista+ 图标内嵌帧与 IHDR 画幅直读用）。
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// Icon 提取结果：PNG 编码的最大画幅帧及其真实尺寸与来源帧型。
type Icon struct {
	PNG    []byte // PNG 编码字节（透传或转码，调用方可直接落盘/上送）
	Width  int    // 真实画幅宽（px）
	Height int    // 真实画幅高（px，图标恒正方，如实记录源值）
	Format string // "png"=Vista 内嵌帧透传；"dib"=位图帧转码
}

// Extract 从磁盘 PE 文件提取最大画幅图标（PNG 字节）。
// 文件缺失/无图标资源/格式不支持均返回 error，由调用方降级回落。
func Extract(path string) (*Icon, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("peicon: 读取 %s 失败: %w", path, err)
	}
	return ExtractBytes(data)
}

// ExtractBytes 从内存中的 PE 字节提取图标（测试夹具与调用方自带缓存共用）。
func ExtractBytes(data []byte) (icon *Icon, err error) {
	// 兜底：所有切片访问虽已带边界判定，仍对恶意/损坏输入保持 panic 免疫。
	defer func() {
		if r := recover(); r != nil {
			icon = nil
			err = fmt.Errorf("peicon: 解析越界被兜底拦截: %v", r)
		}
	}()
	p, err := parsePE(data)
	if err != nil {
		return nil, err
	}
	frames, err := p.iconFrames()
	if err != nil {
		return nil, err
	}
	best := pickLargest(frames)
	if best == nil {
		return nil, errors.New("peicon: 未找到可用图标资源（RT_ICON/RT_GROUP_ICON 均缺失或成员无效）")
	}
	return best.toIcon()
}

// ---------- PE 骨架解析 ----------

// peFile 是图标提取所需的最小 PE 视图：节表（RVA→文件偏移映射）与
// 资源目录（IMAGE_DATA_DIRECTORY[2]）指向的 .rsrc 根。
type peFile struct {
	data []byte
	// 节表项：虚拟地址/虚拟尺寸/原始数据指针/原始数据尺寸。
	sections []section
	rsrcRVA  uint32
	rsrcSize uint32
}

type section struct {
	va, vs  uint32
	rawPtr  uint32
	rawSize uint32
}

func parsePE(data []byte) (*peFile, error) {
	if len(data) < 0x40 {
		return nil, errors.New("peicon: 文件过小，非 PE")
	}
	if data[0] != 'M' || data[1] != 'Z' {
		return nil, errors.New("peicon: 缺少 MZ 签名")
	}
	lfanew := int(binary.LittleEndian.Uint32(data[0x3C:0x40]))
	if lfanew <= 0 || lfanew+24 > len(data) {
		return nil, fmt.Errorf("peicon: e_lfanew 越界: %#x", lfanew)
	}
	if !bytes.Equal(data[lfanew:lfanew+4], []byte("PE\x00\x00")) {
		return nil, errors.New("peicon: 缺少 PE 签名")
	}
	coff := lfanew + 4
	if coff+20 > len(data) {
		return nil, errors.New("peicon: COFF 头越界")
	}
	numSections := int(binary.LittleEndian.Uint16(data[coff+2 : coff+4]))
	sizeOpt := int(binary.LittleEndian.Uint16(data[coff+16 : coff+18]))
	optStart := coff + 20
	if sizeOpt < 96 || optStart+sizeOpt > len(data) {
		return nil, errors.New("peicon: Optional Header 越界")
	}
	magic := binary.LittleEndian.Uint16(data[optStart : optStart+2])
	var dataDirOff int
	switch magic {
	case 0x10b: // PE32
		dataDirOff = 96
	case 0x20b: // PE32+
		dataDirOff = 112
	default:
		return nil, fmt.Errorf("peicon: 未知 Optional Header magic %#x（非标准 PE）", magic)
	}
	if dataDirOff+24 > sizeOpt {
		return nil, errors.New("peicon: 数据目录超出 Optional Header")
	}
	// IMAGE_OPTIONAL_HEADER 尾部：NumberOfRvaAndSizes 紧前于目录数组，
	// 目录数组起始于 dataDirOff（PE32=96 / PE32+=112），每项 8 字节；
	// RESOURCE 是目录 #2（0=导出 1=导入）。
	numDir := binary.LittleEndian.Uint32(data[optStart+dataDirOff-4:])
	if numDir < 3 {
		return nil, errors.New("peicon: 无资源数据目录")
	}
	rsrc := data[optStart+dataDirOff+16 : optStart+dataDirOff+24] // IMAGE_DATA_DIRECTORY #2 = RESOURCE
	p := &peFile{
		data:     data,
		rsrcRVA:  binary.LittleEndian.Uint32(rsrc[:4]),
		rsrcSize: binary.LittleEndian.Uint32(rsrc[4:]),
	}
	if p.rsrcRVA == 0 || p.rsrcSize == 0 {
		return nil, errors.New("peicon: 资源目录为空（无嵌入资源）")
	}
	secStart := optStart + sizeOpt
	for i := 0; i < numSections; i++ {
		off := secStart + i*40
		if off+40 > len(data) {
			return nil, errors.New("peicon: 节表越界")
		}
		p.sections = append(p.sections, section{
			va:      binary.LittleEndian.Uint32(data[off+12 : off+16]),
			vs:      binary.LittleEndian.Uint32(data[off+8 : off+12]),
			rawPtr:  binary.LittleEndian.Uint32(data[off+20 : off+24]),
			rawSize: binary.LittleEndian.Uint32(data[off+16 : off+20]),
		})
	}
	return p, nil
}

// rvaToOffset 把模块 RVA 映射为文件偏移；不在任何节内返回 false。
func (p *peFile) rvaToOffset(rva uint32) (int, bool) {
	for _, s := range p.sections {
		if s.vs == 0 {
			continue
		}
		if rva >= s.va && rva < s.va+s.vs {
			delta := rva - s.va
			// 虚拟尺寸可大于原始数据尺寸（对齐填充等），按 rawSize 截断。
			span := s.rawSize
			if s.vs < span {
				span = s.vs
			}
			if delta >= span {
				return 0, false
			}
			off := int(s.rawPtr) + int(delta)
			if off < 0 || off >= len(p.data) {
				return 0, false
			}
			return off, true
		}
	}
	return 0, false
}

// ---------- 资源目录树 ----------

// dirEntry 是拍平后的 IMAGE_RESOURCE_DIRECTORY_ENTRY（仅收按 ID 寻址的项；
// 命名字段带高位置位，图标资源恒用数字 ID，命中名条目即跳过）。
type dirEntry struct {
	id       uint32
	subdir   bool
	childOff int // 相对 .rsrc 根的偏移（子目录或数据条目）
}

// resourceRoot 返回 .rsrc 段字节（绝对偏移映射进整个文件）。
func (p *peFile) resourceRoot() ([]byte, error) {
	off, ok := p.rvaToOffset(p.rsrcRVA)
	if !ok {
		return nil, errors.New("peicon: 资源目录 RVA 不在任何节内")
	}
	end := off + int(p.rsrcSize)
	if end > len(p.data) {
		end = len(p.data)
	}
	return p.data[off:end], nil
}

// readDir 解析 rsrc 内偏移 off 处的一层目录，返回按 ID 寻址的条目。
func readDir(rsrc []byte, off int, depth int) ([]dirEntry, error) {
	if depth > 6 {
		return nil, errors.New("peicon: 资源目录嵌套过深（疑似损坏）")
	}
	if off < 0 || off+16 > len(rsrc) {
		return nil, errors.New("peicon: 资源目录头越界")
	}
	named := int(binary.LittleEndian.Uint16(rsrc[off+12 : off+14]))
	byID := int(binary.LittleEndian.Uint16(rsrc[off+14 : off+16]))
	total := named + byID
	if total > maxDirEntries {
		return nil, fmt.Errorf("peicon: 目录条目数异常: %d", total)
	}
	out := make([]dirEntry, 0, byID)
	base := off + 16
	for i := 0; i < total; i++ {
		eo := base + i*dirEntrySize
		if eo+dirEntrySize > len(rsrc) {
			return nil, errors.New("peicon: 目录项越界")
		}
		name := binary.LittleEndian.Uint32(rsrc[eo : eo+4])
		next := binary.LittleEndian.Uint32(rsrc[eo+4 : eo+8])
		if name&0x80000000 != 0 {
			continue // 命名条目不参与图标寻址，如实忽略
		}
		out = append(out, dirEntry{
			id:       name,
			subdir:   next&0x80000000 != 0,
			childOff: int(next & 0x7FFFFFFF),
		})
	}
	return out, nil
}

// readDataFromPE 解析数据条目（IMAGE_RESOURCE_DATA_ENTRY）并拷贝其负载。
// OffsetToData 标准口径是模块 RVA；个别打包器记 .rsrc 段内相对偏移，
// 两种口径都兑现，绝不猜半吊子。
func (p *peFile) readDataFromPE(rsrc []byte, off int) ([]byte, error) {
	if off < 0 || off+dataEntrySize > len(rsrc) {
		return nil, errors.New("peicon: 数据条目越界")
	}
	rva := binary.LittleEndian.Uint32(rsrc[off : off+4])
	size := int(binary.LittleEndian.Uint32(rsrc[off+4 : off+8]))
	if size <= 0 || size > maxResourceBytes {
		return nil, fmt.Errorf("peicon: 资源尺寸异常: %d", size)
	}
	if fileOff, ok := p.rvaToOffset(rva); ok && fileOff+size <= len(p.data) {
		return append([]byte(nil), p.data[fileOff:fileOff+size]...), nil
	}
	if int(rva)+size <= len(rsrc) {
		return append([]byte(nil), rsrc[int(rva):int(rva)+size]...), nil
	}
	return nil, errors.New("peicon: 数据条目指向的位置无法定位")
}

// iconFrame 是一帧候选：尺寸 + 原始负载（PNG 帧或 DIB）。
type iconFrame struct {
	width, height int
	data          []byte
}

func (f *iconFrame) isPNG() bool {
	return bytes.HasPrefix(f.data, pngMagic)
}

func (f *iconFrame) area() int { return f.width * f.height }

// resolvePayload 把类型层条目归一到底层负载字节：标准 PE 资源树为
// 类型→名字→语言→数据四层，逐级下钻（每层取首个数据向或非跳过的子目录
// 条目）；个别精简打包器少挂语言层、名字条目直指数据条目，同一循环天然兼容。
func (p *peFile) resolvePayload(rsrc []byte, e dirEntry) ([]byte, bool) {
	for depth := 1; depth < 5; depth++ {
		if !e.subdir {
			payload, err := p.readDataFromPE(rsrc, e.childOff)
			return payload, err == nil
		}
		subs, err := readDir(rsrc, e.childOff, depth+1)
		if err != nil {
			return nil, false
		}
		next := dirEntry{}
		found := false
		// 优先直通数据层，其次继续下钻首个子目录。
		for _, s := range subs {
			if !s.subdir {
				next, found = s, true
				break
			}
		}
		if !found {
			for _, s := range subs {
				if s.subdir {
					next, found = s, true
					break
				}
			}
		}
		if !found {
			return nil, false
		}
		e = next
	}
	return nil, false
}

// iconFrames 枚举全部 RT_ICON 帧作为候选（PS 脚本 EnumResourceNames 全量
// 扫描的同谱语义）。刻意不按 RT_GROUP_ICON 目录圈定候选：真机实证 RAMMap64.exe
// 的组目录只登记 16² 4bpp 帧、32² 帧是组外孤儿——组目录优先会把可得画幅
// 无谓砍半；红线消费场景要的是品牌脸面的最大真实画幅，全帧择优才是诚实口径。
func (p *peFile) iconFrames() ([]*iconFrame, error) {
	rsrc, err := p.resourceRoot()
	if err != nil {
		return nil, err
	}
	l1, err := readDir(rsrc, 0, 0)
	if err != nil {
		return nil, err
	}
	var iconDir []dirEntry
	for _, e := range l1 {
		if e.subdir && e.id == rtIcon {
			iconDir = append(iconDir, e)
		}
	}
	if len(iconDir) == 0 {
		return nil, errors.New("peicon: 无图标资源类型目录（RT_ICON）")
	}
	var frames []*iconFrame
	for _, ty := range iconDir {
		l2, err := readDir(rsrc, ty.childOff, 1)
		if err != nil {
			return nil, err
		}
		for _, e := range l2 {
			payload, ok := p.resolvePayload(rsrc, e)
			if !ok {
				continue // 单帧坏不拖全表
			}
			frame := frameFromPayload(payload)
			if frame == nil {
				continue
			}
			frames = append(frames, frame)
		}
	}
	if len(frames) == 0 {
		return nil, errors.New("peicon: 图标资源均无法解析（PNG 帧与 DIB 帧皆无有效画幅）")
	}
	return frames, nil
}

// frameFromPayload 识别帧画幅；PNG 帧读 IHDR，DIB 帧读 BITMAPINFOHEADER。
// 无效负载返回 nil（调用方跳过该帧）。
func frameFromPayload(data []byte) *iconFrame {
	if len(data) < 16 {
		return nil
	}
	if bytes.HasPrefix(data, pngMagic) {
		if len(data) < 24 || !bytes.Equal(data[12:16], []byte("IHDR")) {
			return nil
		}
		w := int(binary.BigEndian.Uint32(data[16:20]))
		h := int(binary.BigEndian.Uint32(data[20:24]))
		if !validDim(w) || !validDim(h) {
			return nil
		}
		return &iconFrame{width: w, height: h, data: data}
	}
	// BITMAPINFOHEADER：biSize=40；biHeight 惯例为色面×2（含 AND 掩码）。
	if int(binary.LittleEndian.Uint32(data[:4])) < 40 {
		return nil
	}
	w := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	hh := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	if !validDim(w) || !validDim(absI32(hh)) {
		return nil
	}
	if hh%2 == 0 && hh/2 == w {
		hh = hh / 2
	} else if hh < 0 {
		hh = -hh // 顶下序（negative height）：单倍高，无掩码
	}
	if !validDim(hh) {
		return nil
	}
	return &iconFrame{width: w, height: hh, data: data}
}

func validDim(v int) bool { return v > 0 && v <= maxIconDim }

func absI32(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// pickLargest 选面积最大帧；同面积时优先 PNG 透传帧（原样出货）。
func pickLargest(frames []*iconFrame) *iconFrame {
	var best *iconFrame
	for _, f := range frames {
		if best == nil {
			best = f
			continue
		}
		if f.area() > best.area() || (f.area() == best.area() && f.isPNG() && !best.isPNG()) {
			best = f
		}
	}
	return best
}

// toIcon 出货：PNG 帧透传字节；DIB 帧解码转码 32bpp PNG。
func (f *iconFrame) toIcon() (*Icon, error) {
	if f.isPNG() {
		return &Icon{PNG: append([]byte(nil), f.data...), Width: f.width, Height: f.height, Format: "png"}, nil
	}
	img, err := decodeDIB(f.data)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("peicon: PNG 编码失败: %w", err)
	}
	return &Icon{PNG: buf.Bytes(), Width: f.width, Height: f.height, Format: "dib"}, nil
}

// ---------- DIB 解码 ----------

// decodeDIB 解 BITMAPINFOHEADER 色面：支持 1/4/8/24/32bpp（BI_RGB 与
// 32bpp BI_BITFIELDS 的 BGRA 布局），AND 掩码按 GDI 语义生效（置位=透明），
// 输出 image.NRGBA。16bpp 等冷门布局如实报错，不猜。
func decodeDIB(data []byte) (image.Image, error) {
	if len(data) < 40 {
		return nil, errors.New("peicon: DIB 头过短")
	}
	hdrSize := int(binary.LittleEndian.Uint32(data[0:4]))
	w := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	h := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	bitCount := int(binary.LittleEndian.Uint16(data[14:16]))
	compression := binary.LittleEndian.Uint32(data[16:20])
	clrUsed := int(binary.LittleEndian.Uint32(data[32:36]))
	if w <= 0 || !validDim(w) {
		return nil, fmt.Errorf("peicon: DIB 宽非法: %d", w)
	}
	topDown := h < 0
	ph := absI32(h)
	if !validDim(ph) {
		return nil, fmt.Errorf("peicon: DIB 高非法: %d", h)
	}
	// ICO 惯例：biHeight 含 AND 掩码（色面高的两倍）；顶下序单倍高无掩码。
	colorHeight := ph
	hasMask := !topDown && ph%2 == 0 && ph/2 == w
	if hasMask {
		colorHeight = ph / 2
	}
	if compression != 0 && !(bitCount == 32 && compression == 3) {
		return nil, fmt.Errorf("peicon: 不支持的 DIB 压缩方式: %d", compression)
	}
	xorStride := ((w*bitCount + 31) & ^31) / 8
	andStride := ((w + 31) & ^31) / 8
	paletteCount := clrUsed
	if paletteCount == 0 && bitCount <= 8 {
		paletteCount = 1 << bitCount
	}
	paletteOff := hdrSize
	if bitCount == 32 && compression == 3 {
		paletteOff += 12 // BI_BITFIELDS 三张掩码表紧随头后
	}
	xorOff := paletteOff
	if bitCount <= 8 {
		xorOff = paletteOff + paletteCount*4
	}
	total := xorOff + colorHeight*xorStride
	if hasMask {
		total += colorHeight * andStride
	}
	if xorOff < hdrSize || total > len(data) {
		return nil, fmt.Errorf("peicon: DIB 负载越界（need %d, have %d）", total, len(data))
	}
	palette := make([]color.RGBA, paletteCount)
	for i := 0; i < paletteCount; i++ {
		o := paletteOff + i*4
		palette[i] = color.RGBA{B: data[o], G: data[o+1], R: data[o+2], A: 255}
	}
	maskStart := xorOff + colorHeight*xorStride
	img := image.NewNRGBA(image.Rect(0, 0, w, colorHeight))
	for y := 0; y < colorHeight; y++ {
		srcRow := y
		if !topDown {
			srcRow = colorHeight - 1 - y // ICO 色面自下而上
		}
		row := data[xorOff+srcRow*xorStride : xorOff+(srcRow+1)*xorStride]
		var maskRow []byte
		if hasMask {
			mr := colorHeight - 1 - y
			maskRow = data[maskStart+mr*andStride : maskStart+(mr+1)*andStride]
		}
		for x := 0; x < w; x++ {
			var px color.RGBA
			switch bitCount {
			case 32:
				o := x * 4
				px = color.RGBA{B: row[o], G: row[o+1], R: row[o+2], A: row[o+3]}
			case 24:
				o := x * 3
				px = color.RGBA{B: row[o], G: row[o+1], R: row[o+2], A: 255}
			case 8, 4, 1:
				var idx int
				switch bitCount {
				case 8:
					idx = int(row[x])
				case 4:
					b := row[x/2]
					if x%2 == 0 {
						idx = int(b >> 4)
					} else {
						idx = int(b & 0x0f)
					}
				case 1:
					b := row[x/8]
					idx = int((b >> (7 - uint(x%8))) & 1)
				}
				if idx < len(palette) {
					px = palette[idx]
				}
			default:
				return nil, fmt.Errorf("peicon: 不支持的 DIB 位深: %d", bitCount)
			}
			// AND 掩码：置位 → 透明（GDI 语义，PS 脚本经 GDI 解码即此规则）。
			if maskRow != nil && maskRow[x/8]&(0x80>>uint(x%8)) != 0 {
				px.A = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: px.R, G: px.G, B: px.B, A: px.A})
		}
	}
	return img, nil
}
