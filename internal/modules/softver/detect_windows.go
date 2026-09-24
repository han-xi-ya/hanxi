//go:build windows

package softver

// Windows 本机探测（真机实证 2026-09-18，微信 4.1.15.9）：
//   - 注册表口径：HKLM 64/32 位视图 + HKCU 三根 Uninstall；4.0 子键名 "Weixin"
//     （DisplayName"微信"），3.x 子键名 "WeChat"——按子键名与 DisplayName 双判；
//   - InstallLocation 部分安装器写入带引号（rustdesk 同谱先例），统一剥引号；
//     本机 4.0 的 InstallLocation 在 WOW6432Node 根命中；
//   - PE 口径：安装目录主程序 Weixin.exe（4.0）/ WeChat.exe 与 WeChatWin.dll
//     （3.x 真实版本长期记在 DLL 上）读 FileVersion/ProductVersion；
//   - 数据目录两代：4.0 "xwechat_files" / 3.x "WeChat Files"，可自定义盘符——
//     默认 Documents（经 KnownFolder 注册表还原真实重定向路径）、盘符根候选、
//     以及 4.0 组件落盘记录（%APPDATA%\Tencent\xwechat\config 内嵌绝对路径）
//     三路收候选，含账号/all_users 子目录者判"在用"。
//   - 企业微信（WeCom/WXWork）是另一产品，显式排除。

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/versioninfo"
)

func attachPlatformDefaults(s *SoftverService) {
	s.probeLocal = probeLocal
	s.downloadsDir = downloadsDir
}

const uninstallBase = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
const uninstallBase32 = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`

// validVersionRe 参与对比的版本值门槛：至少两段数字（"4.1.15.9"/"3.9.12"）。
var validVersionRe = regexp.MustCompile(`^[0-9][0-9]*(?:\.[0-9]+)+$`)

// configDataDirRe 从 xwechat config 文件里捞数据目录绝对路径（贪婪吃到 xwechat_files 截止）。
var configDataDirRe = regexp.MustCompile(`[A-Za-z]:[^"\x00\r\n\t]*xwechat_files`)

// weixinConfigDir 4.0 客户端配置目录（组件安装记录内嵌数据目录绝对路径）。
func weixinConfigDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Tencent", "xwechat", "config")
}

// probeLocal 一次完整本机探测：Uninstall 双口径安装记录 + 安装/数据目录槽位。
func probeLocal() (localData, error) {
	var data localData
	installs, notes := scanUninstallKeys()
	for i := range installs {
		enrichInstall(&installs[i])
	}
	data.Installs = installs
	data.Dirs = collectDirs(installs)
	data.Notes = append(data.Notes, notes...)
	return data, nil
}

// wechatInstallMatch 判定 Uninstall 子键是否微信本体（排除企业微信）。
func wechatInstallMatch(childName, displayName string) bool {
	lname := strings.ToLower(strings.TrimSpace(childName))
	ldisp := strings.TrimSpace(displayName)
	lowDisp := strings.ToLower(ldisp)
	switch {
	case strings.Contains(ldisp, "企业微信") || strings.Contains(lowDisp, "wecom") || lname == "wxwork":
		return false // 另一产品（F5 卡片边界）
	case lname == "wechat" || lname == "weixin":
		return true
	case lowDisp == "微信" || lowDisp == "wechat" || lowDisp == "weixin":
		return true
	}
	return false
}

// scanUninstallKeys 遍历三根 Uninstall，产出安装记录（跨根去重）。
func scanUninstallKeys() ([]LocalInstall, []string) {
	type root struct {
		hive registry.Key
		path string
		view string
	}
	roots := []root{
		{registry.LOCAL_MACHINE, uninstallBase, "HKLM"},
		{registry.LOCAL_MACHINE, uninstallBase32, "HKLM/WOW6432Node"},
		{registry.CURRENT_USER, uninstallBase, "HKCU"},
	}
	var installs []LocalInstall
	seen := map[string]bool{}
	var notes []string
	for _, r := range roots {
		key, err := registry.OpenKey(r.hive, r.path, registry.READ)
		if err != nil {
			notes = append(notes, fmt.Sprintf("注册表 %s 根不可读: %v", r.view, err))
			continue
		}
		names, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			notes = append(notes, fmt.Sprintf("注册表 %s 枚举子键失败: %v", r.view, err))
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(r.hive, r.path+`\`+name, registry.READ)
			if err != nil {
				continue
			}
			display, _, _ := sub.GetStringValue("DisplayName")
			if !wechatInstallMatch(name, display) {
				sub.Close()
				continue
			}
			dispVer, _, _ := sub.GetStringValue("DisplayVersion")
			loc, _, _ := sub.GetStringValue("InstallLocation")
			pub, _, _ := sub.GetStringValue("Publisher")
			estKB, _, _ := sub.GetIntegerValue("EstimatedSize")
			sub.Close()

			loc = strings.Trim(strings.TrimSpace(loc), `"`)
			dedupe := strings.ToLower(name + "|" + dispVer + "|" + loc)
			if seen[dedupe] {
				continue
			}
			seen[dedupe] = true

			inst := LocalInstall{
				ID:              strings.ToLower(strings.TrimSpace(name)),
				Generation:      generationOf(name, display),
				DisplayName:     strings.TrimSpace(display),
				Publisher:       strings.TrimSpace(pub),
				RegistryKey:     viewHiveName(r.hive) + `\` + r.path + `\` + name,
				DisplayVersion:  strings.TrimSpace(dispVer),
				InstallDir:      loc,
				EstimatedSizeKB: int64(estKB),
			}
			if inst.ID == "" {
				inst.ID = "wechat"
			}
			installs = append(installs, inst)
		}
	}
	fixDuplicateIDs(installs)
	if len(installs) == 0 {
		notes = append(notes, "注册表 Uninstall 未命中微信/Weixin 本体（可能未安装，或安装形态不写卸载登记）")
	}
	return installs, notes
}

func generationOf(childName, displayName string) string {
	switch {
	case strings.EqualFold(strings.TrimSpace(childName), "weixin"):
		return "4.x (Weixin)"
	case strings.EqualFold(strings.TrimSpace(childName), "wechat"):
		return "3.x (WeChat)"
	default:
		_ = displayName // DisplayName 命中但子键名不典型：代际如实未知，不猜
		return "未知代际"
	}
}

func viewHiveName(hive registry.Key) string {
	if hive == registry.CURRENT_USER {
		return "HKEY_CURRENT_USER"
	}
	return "HKEY_LOCAL_MACHINE"
}

// fixDuplicateIDs 同产品多根登记出细微差异时加序号消歧，保证 ID 唯一稳定。
func fixDuplicateIDs(installs []LocalInstall) {
	count := map[string]int{}
	for i := range installs {
		count[installs[i].ID]++
	}
	used := map[string]bool{}
	for i := range installs {
		id := installs[i].ID
		if count[id] > 1 || used[id] {
			n := 2
			for used[fmt.Sprintf("%s-%d", id, n)] {
				n++
			}
			id = fmt.Sprintf("%s-%d", id, n)
			installs[i].ID = id
		}
		used[installs[i].ID] = true
	}
}

// installExeCandidates 主程序 PE 文件候选（相对安装目录；含 3.x 把真实版本
// 记在 WeChatWin.dll 上的历史口径，卡片点名的 WeChatWin.exe 一并防御性尝试）。
var installExeCandidates = []string{"Weixin.exe", "WeChat.exe", "WeChatWin.exe", "WeChatWin.dll"}

// enrichInstall 补安装目录回落、PE 第二口径与主版本判定。
func enrichInstall(in *LocalInstall) {
	if in.InstallDir != "" && dirExists(in.InstallDir) {
		in.InstallDirOrigin = "registry"
	} else if alt, origin := fallbackInstallDir(in.ID); alt != "" {
		in.InstallDir = alt
		in.InstallDirOrigin = origin
	}
	// 注册表有值但目录不存在时保留原值如实呈现（InstallDirOrigin 空 → 前端标"缺失"）。
	in.InstallDirExists = in.InstallDir != "" && dirExists(in.InstallDir)

	// 口径一：注册表 DisplayVersion（安装器口径）。
	if in.DisplayVersion != "" {
		in.Sources = append(in.Sources, VersionSource{
			Kind: "registry", Label: "注册表 Uninstall", Field: "DisplayVersion",
			Value: in.DisplayVersion, Detail: in.RegistryKey,
		})
	}
	// 口径二：PE 文件版本资源（安装内容口径）。
	if in.InstallDirExists {
		for _, cand := range installExeCandidates {
			p := filepath.Join(in.InstallDir, cand)
			if !fileExists(p) {
				continue
			}
			for _, field := range []string{"FileVersion", "ProductVersion"} {
				v, err := versioninfo.StringValue(p, field)
				v = strings.TrimSpace(v)
				if err != nil || v == "" {
					continue
				}
				in.Sources = append(in.Sources, VersionSource{
					Kind: "pe", Label: "PE 文件版本信息", Field: field,
					Value: v, Detail: p,
				})
			}
		}
		if !anySourceKind(in.Sources, "pe") {
			in.Notes = append(in.Notes, "安装目录内未读到 PE 版本资源（候选 "+strings.Join(installExeCandidates, " / ")+"）")
		}
	}

	// 主版本：所有有效读数里取最高（段数更全的构建号自然胜出）；平手注册表口径优先。
	best, bestIdx := "", -1
	for i, src := range in.Sources {
		if !validVersionRe.MatchString(src.Value) {
			continue
		}
		if best == "" || versioncmp.Compare(src.Value, best) > 0 {
			best, bestIdx = src.Value, i
		}
	}
	if bestIdx >= 0 {
		in.Sources[bestIdx].Primary = true
		in.BestVersion = best
	} else if len(in.Sources) > 0 {
		in.BestVersion = in.Sources[0].Value
		in.Sources[0].Primary = true
		in.Notes = append(in.Notes, "版本读数非规范数字段，仅作展示不参与对比")
	}
}

func anySourceKind(srcs []VersionSource, kind string) bool {
	for _, s := range srcs {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

// fallbackInstallDir 注册表 InstallLocation 缺失/失效时的回落探测。
// 返回路径与证据来源（hkcu/default），双双落空返回 ("", "")。
func fallbackInstallDir(id string) (string, string) {
	// 1) HKCU\Software\Tencent\{Weixin|WeChat} 的 InstallPath（本机 4.0 实证有值）。
	want4 := id == "weixin" || strings.HasPrefix(id, "weixin-")
	altKeys := []string{`Software\Tencent\Weixin`, `Software\Tencent\WeChat`}
	if !want4 {
		altKeys = []string{`Software\Tencent\WeChat`, `Software\Tencent\Weixin`}
	}
	for _, k := range altKeys {
		key, err := registry.OpenKey(registry.CURRENT_USER, k, registry.READ)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue("InstallPath")
		key.Close()
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if err == nil && v != "" && dirExists(v) && (fileExists(filepath.Join(v, "Weixin.exe")) || fileExists(filepath.Join(v, "WeChat.exe"))) {
			return v, "hkcu"
		}
	}
	// 2) 常见默认落位（Program Files 环境目录逐机可能重定向，取系统环境变量口径）。
	var cands []string
	if want4 {
		cands = []string{
			filepath.Join(programFiles(), "Tencent", "Weixin"),
			filepath.Join(programFiles(), "Weixin"),
			filepath.Join(programFiles32(), "Tencent", "Weixin"),
		}
	} else {
		cands = []string{
			filepath.Join(programFiles32(), "Tencent", "WeChat"),
			filepath.Join(programFiles(), "Tencent", "WeChat"),
		}
	}
	for _, c := range cands {
		if dirExists(c) && (fileExists(filepath.Join(c, "Weixin.exe")) || fileExists(filepath.Join(c, "WeChat.exe"))) {
			return c, "default"
		}
	}
	return "", ""
}

func programFiles() string {
	if v := os.Getenv("ProgramFiles"); v != "" {
		return v
	}
	return `C:\Program Files`
}

func programFiles32() string {
	if v := os.Getenv("ProgramFiles(x86)"); v != "" {
		return v
	}
	return programFiles()
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// ---- 目录槽位面 ----

// dataGens 两代微信数据目录的规格（目录名 + 标签）。
var dataGens = []struct{ kind, dirName, label string }{
	{"data40", "xwechat_files", "4.0 数据目录"},
	{"data30", "WeChat Files", "3.x 数据目录"},
}

// collectDirs 汇总安装目录槽位 + 两代数据目录候选点。
func collectDirs(installs []LocalInstall) []DirSlot {
	var dirs []DirSlot
	seenID := map[string]bool{}
	add := func(slot DirSlot) {
		if slot.Path == "" || seenID[slot.ID] {
			return
		}
		seenID[slot.ID] = true
		dirs = append(dirs, slot)
	}
	for _, in := range installs {
		if in.InstallDir == "" {
			continue
		}
		origin := in.InstallDirOrigin
		if origin == "" {
			origin = "registry"
		}
		add(DirSlot{
			ID: dirSlotID("install", in.InstallDir), Kind: "install",
			Label:  fmt.Sprintf("安装目录 · %s", in.Generation),
			Path:   in.InstallDir,
			Origin: origin,
			Exists: in.InstallDirExists,
		})
	}
	for _, g := range dataGens {
		for _, slot := range dataSlots(g.kind, g.dirName, g.label) {
			add(slot)
		}
	}
	return dirs
}

// dataSlots 单代数据目录候选：config/注册表记录 > 文档目录默认 > 各固定盘符根。
func dataSlots(kind, dirName, label string) []DirSlot {
	var out []DirSlot
	seen := map[string]bool{}
	add := func(origin, path string, note string) {
		path = strings.Trim(strings.TrimSpace(path), `"`)
		if path == "" || seen[strings.ToLower(path)] {
			return
		}
		seen[strings.ToLower(path)] = true
		slot := DirSlot{
			ID: dirSlotID(kind, path), Kind: kind, Label: label,
			Path: path, Origin: origin, Exists: dirExists(path),
		}
		if note != "" {
			slot.Notes = append(slot.Notes, note)
		}
		if slot.Exists && dataDirActive(path) {
			slot.Active = true
		}
		out = append(out, slot)
	}
	if kind == "data40" {
		for _, p := range configRecordedDirs(dirName) {
			add("config", p, "来源：客户端 config 记录（组件安装路径反推）")
		}
	}
	if kind == "data30" {
		for _, p := range registryRecordedDirs(dirName) {
			add("registry", p, "来源：HKCU\\Software\\Tencent\\WeChat 值反推（3.x 无实机样本，尽力探测）")
		}
	}
	if docs := documentsDir(); docs != "" {
		add("documents", filepath.Join(docs, dirName), "")
	}
	for _, root := range fixedDriveRoots() {
		add("driveRoot", filepath.Join(root, dirName), "候选点：盘符根目录（存储位置可自定义，未在用也可能留有历史数据）")
	}
	return out
}

// myDocumentRe 4.0 自定义存储位置的登记键（真机实证：config\<hash>.ini 内
// "MyDocument:" 一行；默认路径时值为空，回落 Documents 候选）。
var myDocumentRe = regexp.MustCompile(`(?m)^MyDocument:\s*(.*)$`)

// configRecordedDirs 从 %APPDATA%\Tencent\xwechat\config 文本里反推数据目录：
// ① MyDocument: 登记值（主通道）；② 任意文本里内嵌的 xwechat_files 绝对路径
// （组件安装记录等旁证）。仅采信"目录真实存在"的读数，不拿记录字符串冒充。
func configRecordedDirs(dirName string) []string {
	var found []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		if p == "" {
			return
		}
		if k := strings.ToLower(p); !seen[k] {
			seen[k] = true
			found = append(found, p)
		}
	}
	dir := weixinConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return found
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, ierr := e.Info()
		if ierr != nil || fi.Size() > 64*1024 {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		// 腾讯 ini 存在 UTF-16/带 BOM 形态：整删 NUL 再扫（ASCII 文件不受影响），
		// 否则键名与冒号之间夹 NUL 会让正则空跑。
		b = bytes.ReplaceAll(b, []byte{0}, nil)
		text := string(b)
		if m := myDocumentRe.FindStringSubmatch(text); m != nil {
			v := m[1]
			switch {
			case strings.EqualFold(filepath.Base(v), dirName):
				if dirExists(v) {
					add(v)
				}
			case v != "" && dirExists(filepath.Join(v, dirName)):
				add(filepath.Join(v, dirName))
			}
		}
		for _, m := range configDataDirRe.FindAllString(text, -1) {
			p := strings.ReplaceAll(m, `\\`, `\`) // JSON 转义形态还原
			idx := strings.LastIndex(p, dirName)
			if idx < 0 {
				continue
			}
			p = p[:idx+len(dirName)]
			if dirExists(p) {
				add(p)
			}
		}
	}
	return found
}

// registryRecordedDirs 从 HKCU\Software\Tencent\WeChat 的字符串值里捞 3.x 数据目录。
// 本机无 3.x 样本，属"结构留探测位"：命中存在的目录才采信。
func registryRecordedDirs(dirName string) []string {
	var found []string
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Tencent\WeChat`, registry.READ)
	if err != nil {
		return found
	}
	defer key.Close()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return found
	}
	for _, n := range names {
		v, _, verr := key.GetStringValue(n)
		if verr != nil {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if v == "" {
			continue
		}
		if strings.EqualFold(filepath.Base(v), dirName) && dirExists(v) {
			found = append(found, v)
		} else if joined := filepath.Join(v, dirName); dirExists(joined) {
			found = append(found, joined)
		}
	}
	return found
}

// dataDirActive 在用判定：顶层含 all_users（4.0）/ All Users（3.x）或账号目录 wxid_*/gh_*。
func dataDirActive(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		if n == "all_users" || n == "all users" || strings.HasPrefix(n, "wxid_") || strings.HasPrefix(n, "gh_") {
			return true
		}
	}
	return false
}

// documentsDir 真实"文档"目录（注册表 User Shell Folders 还原重定向，如本机 E:\System\文档）。
func documentsDir() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.READ)
	if err == nil {
		raw, _, verr := key.GetStringValue("Personal")
		key.Close()
		if verr == nil && raw != "" {
			if expanded, eerr := registry.ExpandString(raw); eerr == nil && expanded != "" {
				return expanded
			}
			return raw
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Documents")
	}
	return ""
}

// downloadsDir 真实"下载"目录（N38 安装包落位；注册表 User Shell Folders
// 的 KnownFolder GUID 还原重定向，如 OneDrive 接管场景，与 documentsDir 同谱）。
func downloadsDir() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.READ)
	if err == nil {
		raw, _, verr := key.GetStringValue("{374DE290-123F-4565-9164-39C4925E467B}")
		key.Close()
		if verr == nil && raw != "" {
			if expanded, eerr := registry.ExpandString(raw); eerr == nil && expanded != "" {
				return expanded, nil
			}
			return raw, nil
		}
	}
	if home, herr := os.UserHomeDir(); herr == nil {
		return filepath.Join(home, "Downloads"), nil
	}
	return "", errors.New("无法解析系统下载目录，请复制直链到浏览器下载")
}

// fixedDriveRoots 固定磁盘根目录列表（C:\ D:\ …）。
func fixedDriveRoots() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var out []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + `:\`
		p, err := windows.UTF16PtrFromString(letter)
		if err != nil {
			continue
		}
		if windows.GetDriveType(p) == windows.DRIVE_FIXED {
			out = append(out, letter)
		}
	}
	return out
}
