package wsl

// 安装落位偏好（「➕ 添加实例」页的安装基目录）后端持久化。
// 前身为前端 localStorage（key wsl.distroInstallDir），三处粘滞病根：
//   - 一旦存进空串，默认值 D:\wsl 永远回不来，商店安装静默落系统默认（C 盘）；
//   - 手滑填过 C 路径即被永久记住，克隆/迁移预填双双跟去 C；
//   - localStorage 跟着 WebView2 用户数据目录走，dev 构建 / 安装包 / 便携包
//     各自为政，"记忆时好时坏"。
// 迁到 DataDir 落 JSON，与端口转发规则账本（wsl-portproxy.json）同款原子写形态；
// 前端 localStorage 退役，读失败一律降级为"未设置"——偏好读不出来不该拦页面。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallDirPref 安装基目录偏好的返回形态：Set=false 表示从未设置过
// （前端按内置默认 D:\wsl 预填）；Set=true 且 Dir="" 是用户显式选择的
// "系统默认位置"（装到哪由 WSL 说了算，通常在 C 盘）——三态区分开，
// "没记得"与"故意留空"不再共用一个空串。
type InstallDirPref struct {
	Set bool   `json:"set"`
	Dir string `json:"dir"`
}

// GetDistroInstallDir 读取落位偏好。路径不可用、文件缺失或损坏都回退
// "未设置"：偏好读不出来不该影响添加实例页的任何功能。
func (s *WslService) GetDistroInstallDir() InstallDirPref {
	if s.installPrefPath == "" {
		return InstallDirPref{}
	}
	data, err := os.ReadFile(s.installPrefPath)
	if err != nil {
		return InstallDirPref{}
	}
	var store struct {
		Dir string `json:"dir"`
	}
	if json.Unmarshal(data, &store) != nil {
		return InstallDirPref{}
	}
	return InstallDirPref{Set: true, Dir: store.Dir}
}

// SetDistroInstallDir 写入落位偏好：空串=显式"系统默认位置"；非空须是
// 绝对路径形态（moveTarget 同款校验口径）——写偏好时就把明显坏掉的拼写拦下，
// 好过让它粘在记忆里反复坑后续操作。长度封顶防呆（路径实用上限远低于此）。
func (s *WslService) SetDistroInstallDir(dir string) error {
	if s.installPrefPath == "" {
		return fmt.Errorf("数据存储路径不可用，无法记住安装目录")
	}
	dir = strings.TrimSpace(dir)
	if len(dir) > 260 {
		return fmt.Errorf("安装基目录过长（%d 字符），请换更短的目录", len(dir))
	}
	if dir != "" && (!filepath.IsAbs(dir) || !validMovePath(dir)) {
		return fmt.Errorf("安装基目录须为绝对路径（如 D:\\wsl）: %s", dir)
	}
	data, err := json.MarshalIndent(struct {
		Dir string `json:"dir"`
	}{dir}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.installPrefPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.installPrefPath), ".wsl-install-pref-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.installPrefPath)
}
