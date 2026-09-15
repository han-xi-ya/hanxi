// .wslconfig 宿主全局配置：网络模式检测与图形化编辑。
//
// 与 /etc/wsl.conf（发行版内、每版一份）相对，.wslconfig 在用户主目录、
// 全局生效（网络模式、内存/CPU 上限等），且要求 `wsl --shutdown` 后重启才读。
// 三步防线与 wsl.conf 同款（语法闸门 → 写前备份 → 原子写 + 读回复核），
// 另加一道本文件特有的闸门：[networking] networkingMode 值必须在
// {nat, bridged, mirrored} 白名单内——写错别的键至多不生效，写错网络模式
// 会让所有发行版集体失联，必须拦在落盘前。
package wsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// HostConfDoc .wslconfig 读取载荷。
type HostConfDoc struct {
	Path        string   `json:"path"`
	Text        string   `json:"text"`
	Missing     bool     `json:"missing"`
	NetworkMode string   `json:"networkMode"` // nat（缺省归一）| bridged | mirrored | unknown
	Warnings    []string `json:"warnings"`
}

// hostWslConfigPath 配置文件路径；包级变量供单测替换。
var hostWslConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".wslconfig")
}

// iniSectionOf 提取节头行的节名（[name]，容许行尾注释）；非节头返回 ""。
func iniSectionOf(line string) string {
	if !strings.HasPrefix(line, "[") {
		return ""
	}
	name := strings.TrimPrefix(line, "[")
	if j := strings.IndexByte(name, ']'); j >= 0 {
		name = name[:j]
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// extractNetworkingMode 取 [networking] networkingMode 值（剔除行尾注释、归一小写）。
// 第二个返回值表示文件里是否显式写了该键。
func extractNetworkingMode(text string) (string, bool) {
	inNet := false
	for line := range strings.SplitSeq(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		if s := iniSectionOf(t); s != "" {
			inNet = s == "networking"
			continue
		}
		if !inNet {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "networkingMode") {
			continue
		}
		v = strings.TrimSpace(v)
		if j := strings.IndexAny(v, "#;"); j >= 0 && (j == 0 || v[j-1] == ' ') {
			v = strings.TrimSpace(v[:j])
		}
		return strings.ToLower(v), true
	}
	return "", false
}

var validNetworkModes = []string{"nat", "bridged", "mirrored"}

// hostNetworkMode 现状归一：文件不在/未写该键 = nat（平台默认）；写了非法值 = unknown。
func hostNetworkMode() (mode string) {
	path := hostWslConfigPath()
	if path == "" {
		return "nat"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "nat"
	}
	v, found := extractNetworkingMode(string(data))
	if !found {
		return "nat"
	}
	if !slices.Contains(validNetworkModes, v) {
		return "unknown"
	}
	return v
}

// GetWslHostConf 读取 .wslconfig（纯宿主文件读，无副作用）。
func (s *WslService) GetWslHostConf() (HostConfDoc, error) {
	doc := HostConfDoc{Path: hostWslConfigPath(), NetworkMode: hostNetworkMode()}
	if doc.Path == "" {
		return HostConfDoc{}, fmt.Errorf("无法定位用户主目录，.wslconfig 不可得")
	}
	data, err := os.ReadFile(doc.Path)
	if os.IsNotExist(err) {
		doc.Missing = true
		doc.NetworkMode = "nat"
		return doc, nil
	}
	if err != nil {
		return HostConfDoc{}, fmt.Errorf("读取 %s 失败: %w", doc.Path, err)
	}
	doc.Text = normalizeLF(string(data))
	if w, verr := validateWslConf(doc.Text); verr == nil {
		doc.Warnings = w
	} else {
		doc.Warnings = append(doc.Warnings, "现文件存在本工具无法解析的行，直接保存会被拒绝——请手工调整: "+verr.Error())
	}
	if v, found := extractNetworkingMode(doc.Text); found && !slices.Contains(validNetworkModes, v) {
		doc.Warnings = append(doc.Warnings, fmt.Sprintf("networkingMode=%q 不是合法值（nat/bridged/mirrored），保存会被拒绝", v))
	}
	return doc, nil
}

// SaveWslHostConf 写回 .wslconfig：语法闸门 + 网络模式白名单 → 备份 → 原子写 → 复核。
func (s *WslService) SaveWslHostConf(text string) (DistroOpResult, error) {
	path := hostWslConfigPath()
	if path == "" {
		return DistroOpResult{}, fmt.Errorf("无法定位用户主目录，.wslconfig 不可写")
	}
	text = normalizeLF(strings.TrimSpace(text))
	if _, err := validateWslConf(text); err != nil {
		return DistroOpResult{}, err
	}
	if v, found := extractNetworkingMode(text); found && !slices.Contains(validNetworkModes, v) {
		return DistroOpResult{}, fmt.Errorf("networkingMode=%q 不合法——仅接受 nat / bridged / mirrored（大小写不敏感）", v)
	}
	bak := path + ".hanxi.bak"
	if _, err := os.Stat(path); err == nil {
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return DistroOpResult{}, fmt.Errorf("备份前读取原文件失败，写入中止: %w", rerr)
		}
		if werr := os.WriteFile(bak, data, 0o600); werr != nil {
			return DistroOpResult{}, fmt.Errorf("备份 %s 失败，写入中止: %w", bak, werr)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wslconfig-*.tmp")
	if err != nil {
		return DistroOpResult{}, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(text + "\n"); err != nil {
		tmp.Close()
		return DistroOpResult{}, fmt.Errorf("写临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return DistroOpResult{}, fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return DistroOpResult{}, fmt.Errorf("落盘改名失败（原文件已备份在 %s）: %w", bak, err)
	}
	back, rerr := os.ReadFile(path)
	if rerr != nil || normalizeLF(strings.TrimSpace(string(back))) != text {
		return DistroOpResult{}, fmt.Errorf("写入后复验不一致——可从 %s 恢复原文件重试", bak)
	}
	mode := hostNetworkMode()
	return DistroOpResult{
		Success: true,
		Message: fmt.Sprintf(".wslconfig 已写入并复验一致（当前网络模式：%s）；这是全局配置，需 wsl --shutdown 后重启才生效——可用「🌑 全停 WSL」按钮执行", mode),
	}, nil
}

// ShutdownWsl 执行 wsl --shutdown：打停全部发行版（数据无损，下次访问自动再启动）。
// 与迁移/克隆/瘦身共用重操作闸，避免"边搬盘边全停"。
func (s *WslService) ShutdownWsl() (DistroOpResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	finish, ok := s.tryBeginHeavyOp()
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()
	if out, err := s.runWsl(ctx, "--shutdown"); err != nil {
		return DistroOpResult{}, fmt.Errorf("wsl --shutdown 失败: %w %s", err, strings.TrimSpace(out))
	}
	return DistroOpResult{Success: true, Message: "已全部停止（运行中的 Linux 会话已终止，数据无损；下次进入自动重启，新 .wslconfig 此刻生效）"}, nil
}
