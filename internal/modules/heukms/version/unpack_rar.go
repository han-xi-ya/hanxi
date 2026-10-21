// unpack_rar.go 是 HEU 的 RAR5 解包件（模块自有，刻意不进 packages/go/artifact）：
//
//   - 内核 artifact 目前只有 zip 通道（UnpackZip），把 rardecode 塞进共享包会让
//     "可拆走的托管脚手架"从此带上第三方归档依赖，而全仓只有本模块需要 rar——
//     按"重复家族保持独立、通用化要等第二个消费者"的既有纪律留在模块内；
//   - 上游 rar **未加密**（实测空密码即解，包内更新日志那句"解压密码 HEU168"是
//     作者信息栏文字），因此不接密码参数——真遇到加密包会让 rardecode 在读阶段
//     如实报错，不做静默降级。
//
// 三道防线（zip 家族的 ZipSlip/炸弹/CRC 等价物，逐条有实证依据）：
//  1. 条目名清洗与平铺假设（#108 实测上游 5 个条目全在包根，无任何路径分隔）；
//  2. 单条目与总量大小上限（压缩炸弹防护）；
//  3. **CRC 校验时机**：实测 `rardecode` 把文件 CRC 结算在**读满之后**才报
//     `bad file checksum`，所以必须先写 `.partial` 临时件、读到结束且 err==nil
//     才 rename 就位——否则半截错数据会留在中转目录里被 Commit 搬进版本目录。
package version

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nwaples/rardecode"
)

const (
	// rarMaxEntryBytes 单条目解出上限（上游主程序实测 4.7MB，留足十倍余量）。
	rarMaxEntryBytes int64 = 64 << 20
	// rarMaxTotalBytes 整包解出总量上限（HEU 的 rar 里只有 5 个平铺条目）。
	rarMaxTotalBytes int64 = 128 << 20
)

// rarEntryInfo 解包计划消费的最小事实面（从 rardecode.FileHeader 抽出，
// 让"名字与数量审阅"成为可脱离真实 rar 包单测的纯函数——本仓无纯 Go 的
// RAR **写**入库，无法在 CI 造 rar 夹具，故把安全判据抽成纯逻辑来锁）。
type rarEntryInfo struct {
	Name  string // 包内原始条目名（rar 以 '/' 作分隔）
	Size  int64  // 声明解出字节数
	IsDir bool
}

// planRarUnpack 审阅全部条目，返回主程序条目名（包内原名，逐字用于二次定位）。
//
// 判据与 findHostedExe 一脉相承：宁拒不错——名字可疑、exe 非唯一、体积越界
// 都直接拒收整个包，绝不"挑一个看起来像的"。
func planRarUnpack(entries []rarEntryInfo) (string, error) {
	if len(entries) == 0 {
		return "", errors.New("rar 包内没有任何条目")
	}

	var total int64
	var exes []string
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		base, err := sanitizeRarName(e.Name)
		if err != nil {
			return "", err
		}
		if e.Size > rarMaxEntryBytes {
			return "", fmt.Errorf("rar 条目 %s 声明解出 %d 字节，超过单条目上限 %d（疑似压缩炸弹，拒收整包）", base, e.Size, rarMaxEntryBytes)
		}
		total += e.Size
		if strings.EqualFold(path.Ext(base), ".exe") {
			exes = append(exes, e.Name)
		}
	}
	if total > rarMaxTotalBytes {
		return "", fmt.Errorf("rar 包声明解出总量 %d 字节，超过上限 %d（疑似压缩炸弹，拒收整包）", total, rarMaxTotalBytes)
	}
	switch len(exes) {
	case 0:
		return "", errors.New("rar 包内没有 *.exe 主程序条目")
	case 1:
		return exes[0], nil
	default:
		names := make([]string, 0, len(exes))
		for _, x := range exes {
			names = append(names, path.Base(strings.ReplaceAll(x, "\\", "/")))
		}
		sort.Strings(names)
		return "", fmt.Errorf("rar 包内有 %d 枚可执行文件（%s），无法判定主程序，拒收整包", len(exes), strings.Join(names, ", "))
	}
}

// sanitizeRarName 审阅条目名并取基名。
//
// 上游实测为平铺包（无目录条目），因此这里按最保守口径：**任何带路径分隔的
// 条目名一律拒收**——不解子目录、不"归一化后落平铺"，因为一旦上游改了打包
// 形态，静默展平会让布局自检对不上真实结构。同时拒绝穿越片段与 Windows
// 保留设备名（与内核 artifact 的解包闸门同一口径）。
func sanitizeRarName(raw string) (string, error) {
	norm := strings.ReplaceAll(raw, "\\", "/")
	if strings.Contains(norm, "..") {
		return "", fmt.Errorf("rar 条目名含穿越片段，拒收: %q", raw)
	}
	if strings.ContainsAny(norm, ":") {
		return "", fmt.Errorf("rar 条目名含盘符或非法字符，拒收: %q", raw)
	}
	if strings.Contains(norm, "/") {
		return "", fmt.Errorf("rar 条目名带目录层级（本模块只接受平铺包），拒收: %q", raw)
	}
	norm = strings.TrimSpace(norm)
	if norm == "" || norm == "." {
		return "", fmt.Errorf("rar 条目名为空，拒收: %q", raw)
	}
	if strings.HasSuffix(norm, ".") || strings.HasSuffix(norm, ". ") {
		return "", fmt.Errorf("rar 条目名以点结尾（Windows 目录名限制），拒收: %q", raw)
	}
	if isReservedDeviceName(norm) {
		return "", fmt.Errorf("rar 条目名是 Windows 保留设备名，拒收: %q", raw)
	}
	return norm, nil
}

// isReservedDeviceName 与内核 artifact.sanitizeRelPath 同口径的保留名判定
// （CON/PRN/AUX/NUL/COM1-9/LPT1-9，不分大小写、带扩展名同样命中）。
func isReservedDeviceName(name string) bool {
	stem := strings.ToLower(strings.SplitN(name, ".", 2)[0])
	switch stem {
	case "con", "prn", "aux", "nul":
		return true
	}
	for _, prefix := range []string{"com", "lpt"} {
		if len(stem) == len(prefix)+1 && strings.HasPrefix(stem, prefix) {
			if d := stem[len(prefix)]; d >= '1' && d <= '9' {
				return true
			}
		}
	}
	return false
}

// ctxReader 把 ctx 取消注入流式解压（rardecode 只接 io.Reader，不感知上下文）。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// unpackRar 从 rar 包解出主程序到 dstDir（平铺，保留上游原文件名），返回落地路径。
//
// 两遍遍历：pass1 审阅条目名并产出计划（顺带读满全部条目触发 CRC 结算——坏包在
// 这一遍就暴露）；pass2 只把目标条目写盘，其余丢弃。主程序先写 `.partial`，
// 读满且无错误、字节数与声明一致才 rename 就位（防"错数据已落盘"的半件）。
func unpackRar(ctx context.Context, rarPath, dstDir string) (string, error) {
	entries, err := listRarEntries(ctx, rarPath)
	if err != nil {
		return "", err
	}
	target, err := planRarUnpack(entries)
	if err != nil {
		return "", err
	}
	base, err := sanitizeRarName(target)
	if err != nil {
		return "", err
	}

	f, err := os.Open(rarPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// 独立 context 派生一份取消信号：解包中途取消同样落到下面的错误路径。
	// rardecode.Reader 无 Close（只有 OpenReader 返回的 ReadCloser 有）——
	// 底层 *os.File 由上面的 defer 收口，这里不重复关闭。
	rc, err := rardecode.NewReader(&ctxReader{ctx: ctx, r: f}, "")
	if err != nil {
		return "", fmt.Errorf("打开 rar 包失败: %w", err)
	}

	var declared int64
	matched := false
	for {
		h, err := rc.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("遍历 rar 包失败: %w", err)
		}
		if h.Name != target {
			if _, cerr := io.Copy(io.Discard, &ctxReader{ctx: ctx, r: rc}); cerr != nil {
				return "", fmt.Errorf("跳过 rar 条目 %q 失败: %w", h.Name, cerr)
			}
			continue
		}
		matched = true
		declared = h.UnPackedSize
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return "", err
		}
		final := filepath.Join(dstDir, base)
		tmp := final + ".partial"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		// 上限多读 1 字节：超限立刻可见，而不是悄悄截断。
		n, copyErr := io.Copy(out, io.LimitReader(&ctxReader{ctx: ctx, r: rc}, rarMaxEntryBytes+1))
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("解压 rar 条目 %q 失败: %w", base, copyErr)
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("写入主程序失败: %w", closeErr)
		}
		if n > rarMaxEntryBytes {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("rar 条目 %q 实际解出超过上限 %d 字节，拒收", base, rarMaxEntryBytes)
		}
		if declared > 0 && n != declared {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("rar 条目 %q 解出 %d 字节与包内声明 %d 字节不符，拒收", base, n, declared)
		}
		if err := os.Rename(tmp, final); err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
		break
	}
	if !matched {
		return "", fmt.Errorf("rar 包内定位不到计划选定的主程序条目 %q（包结构与审阅结果不符）", target)
	}
	return filepath.Join(dstDir, base), nil
}

// listRarEntries pass1：审阅条目名（并读满每个条目，使 rardecode 的文件 CRC
// 校验在本遍即完成——坏包根本走不到解盘那一步）。
func listRarEntries(ctx context.Context, rarPath string) ([]rarEntryInfo, error) {
	f, err := os.Open(rarPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rc, err := rardecode.NewReader(&ctxReader{ctx: ctx, r: f}, "")
	if err != nil {
		return nil, fmt.Errorf("打开 rar 包失败: %w", err)
	}

	var out []rarEntryInfo
	for {
		h, err := rc.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("遍历 rar 包失败: %w", err)
		}
		out = append(out, rarEntryInfo{Name: h.Name, Size: h.UnPackedSize, IsDir: h.IsDir})
		if _, cerr := io.Copy(io.Discard, &ctxReader{ctx: ctx, r: rc}); cerr != nil {
			return nil, fmt.Errorf("校验 rar 条目 %q 失败: %w", h.Name, cerr)
		}
	}
}
