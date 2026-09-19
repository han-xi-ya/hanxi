package artifact

// ---------- 安全解包：恶意 zip 的十八层闸门 ----------
//
// 防护清单（PLAN §10.4，逐条对应 ocr/litemonitor 已验证经验）：
//   - ZipSlip：反斜杠归一 + 组件级清洗 + Clean 前后双查，绝对/盘符/UNC/.. 全拒；
//   - 链接语义：symlink/设备/管道/socket 条目一律拒收；junction/reparse 无法
//     经由本包的"普通文件写入 + MkdirAll"落盘路径产生，天然免疫；
//   - Windows 文件名纪律：保留设备名（CON/PRN/AUX/NUL/COM1-9/LPT1-9）、
//     尾点/尾空格、非法字符与控制字符全拒；
//   - 大小写去重：a.txt 与 A.txt 并存即冲突（含文件/目录互相占位）；
//   - zip 炸弹：条目数、声明单文件/总展开、压缩比（≥1MiB 才判，免误杀小文件）
//     静态预检 + cappedWriter 实写字节双预算兜底；
//   - 完整性：archive/zip 逐条目强制 CRC32（读满不提前返回），可选逐文件 SHA-256；
//   - 白名单：allowFiles 非 nil 时，包内文件必须全在清单内、清单点名文件必须
//     全在包内（缺失同样拒收——清单是完整契约，不是可选提示）；
//   - 独占落位：O_CREATE|O_EXCL 不覆盖；目标目录必须不存在或为空目录。

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 压缩比闸门只作用于达到该展开规模以上的条目：小文件压缩比噪声大
// （几十字节的文本很容易 >200:1），真正危险的炸弹必然体量可观。
const ratioCheckFloorBytes = int64(1 << 20)

// Limits 是解包防护预算。零值/负值字段回退 DefaultLimits 对应项。
type Limits struct {
	MaxEntries    int     // 条目数上限
	MaxFileBytes  int64   // 单文件展开上限
	MaxTotalBytes int64   // 总展开上限
	MaxRatio      float64 // 压缩比上限（展开/压缩）
}

// DefaultLimits 通用托管组件的默认预算：条目 5000 / 单文件 1GiB / 总 4GiB / 压缩比 200。
var DefaultLimits = Limits{
	MaxEntries:    5000,
	MaxFileBytes:  1 << 30,
	MaxTotalBytes: 4 << 30,
	MaxRatio:      200,
}

// normalize 把非正字段回退为默认值（0 视为"未设"而非"不许"）。
func (l Limits) normalize() Limits {
	if l.MaxEntries <= 0 {
		l.MaxEntries = DefaultLimits.MaxEntries
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = DefaultLimits.MaxFileBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = DefaultLimits.MaxTotalBytes
	}
	if l.MaxRatio <= 0 {
		l.MaxRatio = DefaultLimits.MaxRatio
	}
	return l
}

// UnpackZip 把 zipPath 安全解到 targetDir（必须不存在[自动新建] 或 已存在的空普通目录）。
// lim 的零值字段回退 DefaultLimits；allowFiles 为可选清单（rel→sha256，值空=不校验）：
// nil 表示不做白名单限制，非 nil（含空 map）表示包内文件必须全在清单、
// 点名文件必须齐备，且逐文件按摘要复核。任一条目违规即报错中止，
// 已写出的残件由调用方连同 staging 目录整体清理（本包只写入 targetDir 内）。
func UnpackZip(zipPath, targetDir string, lim Limits, allowFiles map[string]string) error {
	lim = lim.normalize()

	// ---- 目标目录闸门 ----
	if st, err := os.Lstat(targetDir); err == nil {
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("解包目标不是普通目录（拒绝链接/文件占位）：%s", targetDir)
		}
		ents, err := os.ReadDir(targetDir)
		if err != nil {
			return fmt.Errorf("检查目标目录失败: %w", err)
		}
		if len(ents) > 0 {
			return fmt.Errorf("解包目标目录必须为空（独占落位不覆盖已有文件）：%s", targetDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查目标目录失败: %w", err)
	} else if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("创建解包目标目录失败: %w", err)
	}

	// ---- 清单归一化（manifest 路径不可信其字面，同过清洗闸） ----
	restricted := allowFiles != nil
	var manifest map[string]string // lowerRel → 期望 sha256（小写，空=不校验）
	if restricted {
		manifest = make(map[string]string, len(allowFiles))
		for rel, sha := range allowFiles {
			clean, err := sanitizeRelPath(rel)
			if err != nil {
				return fmt.Errorf("清单含非法路径 %q: %w", rel, err)
			}
			key := strings.ToLower(clean)
			if _, dup := manifest[key]; dup {
				return fmt.Errorf("清单路径大小写冲突：%q 与既有项重复", rel)
			}
			sha = strings.ToLower(strings.TrimSpace(sha))
			if sha != "" && !isSHA256Hex(sha) {
				return fmt.Errorf("清单文件 %s 的摘要不是合法 sha256：%q", rel, allowFiles[rel])
			}
			manifest[key] = sha
		}
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("无法打开 zip（文件可能损坏）: %w", err)
	}
	defer zr.Close()

	if len(zr.File) == 0 {
		return errors.New("zip 不含任何条目，拒收")
	}
	if len(zr.File) > lim.MaxEntries {
		return fmt.Errorf("zip 条目数 %d 超出上限 %d，拒收", len(zr.File), lim.MaxEntries)
	}

	// ---- 第一遍：静态校验（路径清洗/类型/预算/白名单/去重） ----
	type pending struct {
		f     *zip.File
		clean string
	}
	var dirs []string
	var files []pending
	occupied := map[string]string{} // lowerRel → "dir"/"file"（大小写去重 + 占位冲突检测）
	var declaredTotal int64
	provided := map[string]bool{} // 包内实际文件集（清单完整性复核用）

	for _, f := range zr.File {
		clean, err := sanitizeRelPath(f.Name)
		if err != nil {
			return fmt.Errorf("zip 含非法路径条目 %q: %w", f.Name, err)
		}
		key := strings.ToLower(clean)
		// 目录判定双通道:mode 位之外,ZIP 惯例的目录条目还以原始名尾斜杠标识
		// (`Plugins\x86\` 这类包 common;实测 QuickLook 官方包部分条目 mode 位
		// 不带目录标志,单靠 IsDir() 会落成 0 字节文件并连坐"祖先被文件占位"整包拒收)。
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") || strings.HasSuffix(f.Name, "\\") {
			if owner, dup := occupied[key]; dup && owner == "file" {
				return fmt.Errorf("zip 条目 %q 与同名文件冲突（目录/文件互相占位），拒收", f.Name)
			}
			occupied[key] = "dir"
			dirs = append(dirs, clean)
			continue
		}

		// 链接与特殊文件：symlink 是 ZipSlip 的换皮通道；设备/管道/socket 无落盘语义
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("zip 含符号链接条目 %q，拒收", f.Name)
		}
		if mode&os.ModeType != 0 {
			return fmt.Errorf("zip 含不支持的特殊文件条目 %q，拒收", f.Name)
		}

		// 预算（声明值预检；实写另有 cappedWriter 兜底）
		if f.UncompressedSize64 > uint64(lim.MaxFileBytes) {
			return fmt.Errorf("条目 %q 声明展开 %d 字节，超单文件上限 %d 字节，拒收",
				f.Name, f.UncompressedSize64, lim.MaxFileBytes)
		}
		if f.UncompressedSize64 > uint64(lim.MaxTotalBytes-declaredTotal) {
			return fmt.Errorf("zip 声明展开总大小超过上限 %d 字节，拒收", lim.MaxTotalBytes)
		}
		declaredTotal += int64(f.UncompressedSize64)
		if int64(f.UncompressedSize64) >= ratioCheckFloorBytes {
			if f.CompressedSize64 == 0 {
				return fmt.Errorf("条目 %q 压缩尺寸为 0 但展开非空（声明自相矛盾），拒收", f.Name)
			}
			if r := float64(f.UncompressedSize64) / float64(f.CompressedSize64); r > lim.MaxRatio {
				return fmt.Errorf("条目 %q 压缩比 %.0f:1 超过上限 %.0f:1（zip 炸弹防护），拒收",
					f.Name, r, lim.MaxRatio)
			}
		}

		// 白名单
		if restricted {
			if _, listed := manifest[key]; !listed {
				return fmt.Errorf("zip 含清单外文件 %q，拒收", clean)
			}
		}

		// 去重与占位冲突：自身与所有祖先目录
		if owner, dup := occupied[key]; dup {
			if owner == "file" {
				return fmt.Errorf("zip 含重复/大小写冲突条目 %q，拒收", f.Name)
			}
			return fmt.Errorf("zip 条目 %q 与同名目录冲突，拒收", f.Name)
		}
		for anc := path.Dir(clean); anc != "."; anc = path.Dir(anc) {
			if occupied[strings.ToLower(anc)] == "file" {
				return fmt.Errorf("zip 条目 %q 的祖先路径被文件占位，落位冲突，拒收", f.Name)
			}
		}
		occupied[key] = "file"
		provided[key] = true
		files = append(files, pending{f: f, clean: clean})
	}
	if restricted {
		for wantRel := range manifest {
			if !provided[wantRel] {
				return fmt.Errorf("清单点名的文件 %q 在包内缺失，拒收", wantRel)
			}
		}
	}

	// ---- 第二遍：落盘（O_EXCL 独占 + 双预算实写 + 读满触发 CRC + 逐文件 SHA 复核） ----
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(targetDir, filepath.FromSlash(d)), 0755); err != nil {
			return fmt.Errorf("创建目录条目 %q 失败: %w", d, err)
		}
	}
	var writtenTotal int64
	for _, it := range files {
		full := filepath.Join(targetDir, filepath.FromSlash(it.clean))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return fmt.Errorf("创建 %q 父目录失败: %w", it.clean, err)
		}
		out, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("独占创建 %q 失败（拒绝覆盖既有文件）: %w", it.clean, err)
		}
		rc, err := it.f.Open()
		if err != nil {
			out.Close()
			return fmt.Errorf("打开 zip 条目 %q 失败: %w", it.f.Name, err)
		}
		h := sha256.New()
		cw := &cappedWriter{w: io.MultiWriter(out, h), maxFile: lim.MaxFileBytes, total: &writtenTotal, maxTotal: lim.MaxTotalBytes}
		// 必须读满：提前返回会跳过 archive/zip 内建 CRC32 校验
		_, copyErr := io.Copy(cw, rc)
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("解压条目 %q 失败: %w", it.f.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("关闭 %q 失败: %w", it.clean, closeErr)
		}
		if restricted {
			if wantSHA := manifest[strings.ToLower(it.clean)]; wantSHA != "" {
				got := hex.EncodeToString(h.Sum(nil))
				if got != wantSHA {
					return fmt.Errorf("逐文件 SHA256 校验失败：%s（期望 %s，实际 %s）", it.clean, wantSHA, got)
				}
			}
		}
	}
	return nil
}

// sanitizeRelPath 清洗 zip 条目/清单路径为目标目录下安全的 slash 相对形式。
// 清洗前后双查：反斜杠归一为分隔后仍出现绝对/盘符/UNC/空组件/.. 一律拒绝。
func sanitizeRelPath(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("路径为空")
	}
	if strings.ContainsRune(raw, 0) {
		return "", errors.New("路径含 NUL 字符")
	}
	name := strings.ReplaceAll(raw, "\\", "/")
	switch {
	case strings.HasPrefix(name, "//"):
		return "", errors.New("UNC 路径被拒")
	case len(name) >= 2 && name[1] == ':' && (name[0] >= 'A' && name[0] <= 'Z' || name[0] >= 'a' && name[0] <= 'z'):
		return "", errors.New("盘符绝对路径被拒")
	case strings.HasPrefix(name, "/"):
		return "", errors.New("绝对路径被拒")
	}
	name = strings.TrimSuffix(name, "/") // 目录条目的尾斜杠不参与组件检查
	if name == "" {
		return "", errors.New("路径为空")
	}
	for _, comp := range strings.Split(name, "/") {
		if err := checkPathComponent(comp); err != nil {
			return "", err
		}
	}
	clean := path.Clean(name)
	// 双查：清洗归一之后仍不得逃逸（防御 "a/../../etc" 之类先藏后跳的写法）
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("清洗后逃逸目标目录（%q → %q）", raw, clean)
	}
	return clean, nil
}

// checkPathComponent 组件级 Windows 文件名纪律闸。
func checkPathComponent(comp string) error {
	switch {
	case comp == "":
		return errors.New("含空路径组件（连续分隔符或首尾斜杠）")
	case comp == ".":
		return errors.New("含 . 组件（路径写法异常）")
	case comp == "..":
		return errors.New("含 .. 逃逸组件")
	case strings.HasSuffix(comp, "."):
		return fmt.Errorf("组件 %q 以点结尾（Windows 目录名限制）", comp)
	case strings.HasSuffix(comp, " "):
		return fmt.Errorf("组件 %q 以空格结尾（Windows 目录名限制）", comp)
	case strings.ContainsAny(comp, `:*?"<>|`):
		return fmt.Errorf("组件 %q 含 Windows 非法字符", comp)
	case isReservedDeviceName(comp):
		return fmt.Errorf("组件 %q 是 Windows 保留设备名", comp)
	}
	for _, r := range comp {
		if r < 0x20 {
			return fmt.Errorf("组件 %q 含控制字符", comp)
		}
	}
	return nil
}

// isReservedDeviceName 判定 Windows 保留设备名（含带扩展名形态，如 CON.txt）。
func isReservedDeviceName(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		prefix := strings.ToUpper(base[:3])
		digit := base[3]
		if (prefix == "COM" || prefix == "LPT") && digit >= '1' && digit <= '9' {
			return true
		}
	}
	return false
}

// cappedWriter 解压预算写入器：单文件与累计总字节双上限，越限即报错中断。
type cappedWriter struct {
	w        io.Writer
	maxFile  int64
	total    *int64
	maxTotal int64
	written  int64
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	n := int64(len(p))
	if c.written+n > c.maxFile {
		return 0, fmt.Errorf("条目展开超过单文件上限 %d 字节（zip 炸弹防护）", c.maxFile)
	}
	if *c.total+n > c.maxTotal {
		return 0, fmt.Errorf("zip 展开总大小超过上限 %d 字节（zip 炸弹防护）", c.maxTotal)
	}
	written, err := c.w.Write(p)
	c.written += int64(written)
	*c.total += int64(written)
	return written, err
}
