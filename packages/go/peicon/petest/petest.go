// Package petest 构造最小合法 PE 图标夹具（供 peicon 及其消费方测试复用）。
//
// 存在的意义：peicon 的表驱动测试与 internal/app 的 RuntimeIconService 缓存
// 测试都需要"真实可被解析器吃下的 PE 字节"，两份各写一遍必漂移，收口此处。
// 布局：四层资源目录树（类型→名字→语言→数据）+ 各数据条目后紧跟其负载
// （解析器按绝对偏移寻址，对布局零假设），DataEntry.OffsetToData 按标准
// 模块 RVA 口径；外壳是单节 .rsrc 的最小 PE32/PE32+。
package petest

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// 资源类型常量（与 peicon 内部一致，夹具侧独立声明避免导出面污染）。
const (
	IconType      = 3  // RT_ICON
	GroupIconType = 14 // RT_GROUP_ICON
	VersionType   = 16 // RT_VERSION（负例用）
)

const (
	rsrcVA    = uint32(0x2000)
	sectionSz = uint32(0x400) // .rsrc 文件原始偏移
)

// Data 是一个语言层资源项。
type Data struct {
	LangID  uint32
	Payload []byte
}

// Name 是一个名字层资源项。
type Name struct {
	ID    uint32
	Langs []Data
}

// Type 是一个类型层资源项。
type Type struct {
	ID    uint32
	Names []Name
}

// BuildRsrc 深度优先序列化资源树。
func BuildRsrc(types []Type) []byte {
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
		patch(rootEntries+i*8, ty.ID)
		patch(rootEntries+i*8+4, uint32(buf.Len())|0x80000000)
		writeDirHeader(len(ty.Names))
		typeEntries := buf.Len()
		buf.Write(make([]byte, 8*len(ty.Names)))
		for j, nm := range ty.Names {
			patch(typeEntries+j*8, nm.ID)
			patch(typeEntries+j*8+4, uint32(buf.Len())|0x80000000)
			writeDirHeader(len(nm.Langs))
			nameEntries := buf.Len()
			buf.Write(make([]byte, 8*len(nm.Langs)))
			for k, d := range nm.Langs {
				patch(nameEntries+k*8, d.LangID)
				patch(nameEntries+k*8+4, uint32(buf.Len())|0x80000000)
				// 语言层：单条目目录，条目直指数据条目（无高位标记）。
				writeDirHeader(1)
				langEntry := buf.Len()
				buf.Write(make([]byte, 8))
				dataEntry := buf.Len()
				buf.Write(make([]byte, 16))
				payloadOff := buf.Len()
				buf.Write(d.Payload)
				patch(langEntry, d.LangID)
				patch(langEntry+4, uint32(dataEntry))
				patch(dataEntry, rsrcVA+uint32(payloadOff))
				patch(dataEntry+4, uint32(len(d.Payload)))
			}
		}
	}
	return buf.Bytes()
}

// BuildPE 把 .rsrc 段包进一个最小 PE；pe32Plus 选择 Optional Header 布局。
func BuildPE(rsrc []byte, pe32Plus bool) []byte {
	file := make([]byte, int(sectionSz)+len(rsrc))
	file[0], file[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(file[0x3C:], 0x40)
	copy(file[0x40:], []byte("PE\x00\x00"))
	binary.LittleEndian.PutUint16(file[0x46:], 1) // NumberOfSections
	sizeOpt := 224
	if pe32Plus {
		sizeOpt = 240
	}
	binary.LittleEndian.PutUint16(file[0x54:], uint16(sizeOpt))
	opt := 0x58
	magic := uint16(0x10b)
	dataDirOff := 96
	if pe32Plus {
		magic = 0x20b
		dataDirOff = 112
	}
	binary.LittleEndian.PutUint16(file[opt:], magic)
	binary.LittleEndian.PutUint32(file[opt+dataDirOff-4:], 16) // 目录数
	binary.LittleEndian.PutUint32(file[opt+dataDirOff+16:], rsrcVA)
	binary.LittleEndian.PutUint32(file[opt+dataDirOff+20:], uint32(len(rsrc)))
	sec := opt + sizeOpt
	copy(file[sec:sec+8], []byte(".rsrc   "))
	binary.LittleEndian.PutUint32(file[sec+8:], uint32(len(rsrc)))  // VirtualSize
	binary.LittleEndian.PutUint32(file[sec+12:], rsrcVA)            // VirtualAddress
	binary.LittleEndian.PutUint32(file[sec+16:], uint32(len(rsrc))) // SizeOfRawData
	binary.LittleEndian.PutUint32(file[sec+20:], sectionSz)         // PointerToRawData
	copy(file[int(sectionSz):], rsrc)
	return file
}

// PNG 生成 size×size 纯色 PNG 帧（Vista+ 内嵌图标同型）。
func PNG(size int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// DIB32 生成 32bpp BITMAPINFOHEADER 图标帧：色面自下而上、AND 掩码
// 自下而上（置位=透明）；biHeight 按惯例取色面两倍；左上角像素透明，
// 其余为 c。
func DIB32(size int, c color.NRGBA) []byte {
	stride := size * 4
	maskStride := ((size + 31) & ^31) / 8
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(size))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(size*2))
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	xor := make([]byte, stride*size)
	for i := 0; i < size*size; i++ {
		xor[i*4+0] = c.B
		xor[i*4+1] = c.G
		xor[i*4+2] = c.R
		xor[i*4+3] = c.A
	}
	mask := make([]byte, maskStride*size)
	mask[len(mask)-maskStride] = 0x80
	var buf bytes.Buffer
	buf.Write(hdr)
	buf.Write(xor)
	buf.Write(mask)
	return buf.Bytes()
}

// GroupMember 是 GRPICONDIR 的一条成员目录项。
type GroupMember struct {
	W, H, Bit, Bytes, ID uint16
}

// Group 生成 GRPICONDIR 字节（仅用于"组不圈定候选"的负例钉桩）。
func Group(members ...GroupMember) []byte {
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint16(buf[2:], 1)
	binary.LittleEndian.PutUint16(buf[4:], uint16(len(members)))
	for _, m := range members {
		e := make([]byte, 14)
		e[0] = byte(m.W) // 256 在协议里记 0，夹具只用小尺寸
		e[1] = byte(m.H)
		binary.LittleEndian.PutUint16(e[4:], 1)
		binary.LittleEndian.PutUint16(e[6:], m.Bit)
		binary.LittleEndian.PutUint16(e[8:], m.Bytes)
		binary.LittleEndian.PutUint16(e[12:], m.ID)
		buf = append(buf, e...)
	}
	return buf
}
