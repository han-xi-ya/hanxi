// Package search 基于 ES.exe（voidtools 官方命令行查询工具，MIT 许可）的内嵌搜索：
//
// ES.exe 经窗口消息 IPC 直连正在运行的 Everything 实例查询索引——零磁盘开销、秒级返回。
// 该组件与 Everything 版本解耦（IPC 协议跨版本稳定），独立存放于 dataDir/everything/es/，
// 不使用 versions 隔离目录；升级只需修改 esVersion 常量。
package search

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// ES 版本与官方资产（与 Everything 主程序独立发版）。
	// 升级：改常量 + 真机重跑单测样例即可，勿引入远程版本探测（协议稳定，无此必要）。
	esVersion = "1.1.0.37"
	esZipURL  = "https://www.voidtools.com/ES-" + esVersion + ".x64.zip"

	// searchTimeout ES 查询超时（索引驻留内存，正常亚秒级）
	searchTimeout = 10 * time.Second
	// maxResults 单次搜索上限（防前端渲染压力；超限由 service 层截断）
	maxResults = 300
	// maxZipBody ES zip 体积上限（实际约百 KB 级，防御异常响应）
	maxZipBody = 8 << 20
)

var userAgent = "HubKit/0.2"

// Result 单条搜索结果（ES 输出列的子集，其余列丢弃）。
type Result struct {
	Name     string `json:"name"`     // 文件名（目录时含尾部分隔符，见解析说明）
	Path     string `json:"path"`     // 所在目录完整路径
	Size     int64  `json:"size"`     // 字节（目录/未知为 0）
	Modified string `json:"modified"` // 修改时间（ES 原样文本）
	IsDir    bool   `json:"isDir"`    // 目录标记（ES 属性列含 D）
}

// ESVersion 返回当前捆绑的 ES 组件版本号（事件载荷与 UI 展示用）。
func ESVersion() string { return esVersion }

// ESExePath 计算搜索组件的安装路径。
func ESExePath(toolDir string) string {
	return filepath.Join(toolDir, "es.exe")
}

// EnsureESExe 幂等安装搜索引擎拷贝到 toolDir：不存在时下载官方 zip 并解压 es.exe。
// onProgress 可选：上报阶段（downloading/extract/done，失败场景由调用方处理 error 即可）。
func EnsureESExe(toolDir string, onProgress func(stage string)) error {
	emit := func(stage string) {
		if onProgress != nil {
			onProgress(stage)
		}
	}

	target := ESExePath(toolDir)
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() && fi.Mode().IsRegular() && fi.Size() > 0 {
		emit("done")
		return nil // 已安装
	}

	// 1. 下载 zip 到临时文件（zip 内建 CRC32 在解压读满时强制校验）
	emit("downloading")
	tmp, err := os.CreateTemp("", "hubkit-es-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	tmp.Close()

	client := &http.Client{Timeout: 2 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, esZipURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载 ES 搜索组件失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 ES 搜索组件失败: HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(resp.Body, maxZipBody)); err != nil {
		out.Close()
		return err
	}
	out.Close()

	// 2. 解压 es.exe（写临时名后原子 Rename，避免半成品被并发读取）
	emit("extract")
	if err := os.MkdirAll(toolDir, 0755); err != nil {
		return err
	}
	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("ES zip 损坏: %w", err)
	}
	defer zr.Close()
	var esFound bool
	for _, f := range zr.File {
		if !strings.EqualFold(f.Name, "es.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		staging := target + ".tmp"
		st, err := os.Create(staging)
		if err != nil {
			rc.Close()
			return err
		}
		// 读满触发 CRC32 校验
		_, copyErr := io.Copy(st, rc)
		rc.Close()
		st.Close()
		if copyErr != nil {
			return fmt.Errorf("ES zip 内容校验失败: %w", copyErr)
		}
		if err := os.Rename(staging, target); err != nil {
			return err
		}
		esFound = true
		break
	}
	if !esFound {
		return fmt.Errorf("ES zip 布局无效：缺少 es.exe")
	}
	emit("done")
	return nil
}

// Search 经 ES.exe 查询运行中的 Everything 实例。
// 无实例/组件缺失等错误由返回 error 描述；空结果合法返回空切片。
//
// 输出通道用 -export-tsv 写临时文件而非捕获 stdout：
// ES 直写控制台时经 OEM 代码页（中文系统 GBK），中文路径会乱码且无 -utf8 开关
// （实测 `es.exe -utf8` → "Error 6: Unknown switch"）；而导出文件恒为 UTF-8
// （-utf8-bom 双保险），是官方推荐的 Unicode 输出路径。
func Search(esExe, query string, limit int) ([]Result, error) {
	if esExe == "" {
		return nil, fmt.Errorf("搜索引擎拷贝未安装")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}
	if limit < 1 || limit > maxResults {
		limit = maxResults
	}

	// 临时导出文件：读回后删除（300 行 × 数百字节，体积可忽略）
	tmp, err := os.CreateTemp("", "hubkit-es-*.tsv")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command(esExe,
		"-export-tsv", tmpPath,
		"-utf8-bom",
		"-no-header",
		"-timeout", strconv.Itoa(int(searchTimeout/time.Millisecond)),
		"-n", strconv.Itoa(limit),
		query,
	)
	hideConsole(cmd) // ES.exe 是控制台程序：不带这一幕会闪黑窗口
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case runErr := <-done:
		if runErr != nil {
			return nil, errFromES(stderr.String())
		}
	case <-time.After(searchTimeout + 5*time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("搜索超时（%ds），请确认 Everything 实例正常", int(searchTimeout/time.Second))
	}

	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("读取搜索结果失败: %w", err)
	}

	// 导出文件每行一个全路径（ES 1.1.0.37 实测无配置时单列布局，无大小/时间列），
	// 元数据一律用本地 stat 权威补齐——顺带规避索引与磁盘间的毫秒级漂移。
	var results []Result
	for _, full := range ParseLines(raw) {
		if r, ok := ReadMeta(full); ok {
			results = append(results, r)
		}
		// 目标在查询后瞬间消失：跳过该行（索引稍迟一拍属正常）
	}
	return results, nil
}

// errFromES 把 ES 的失败输出翻译成面向用户的信息。
func errFromES(stderr string) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = "未知原因"
	}
	switch {
	case strings.Contains(msg, "IPC") || strings.Contains(msg, "Unable to connect") || strings.Contains(msg, "not running"):
		return fmt.Errorf("未连接到 Everything 实例，请先启动后台服务")
	case strings.Contains(msg, "Unknown switch"):
		return fmt.Errorf("搜索组件执行失败（%s）——请尝试在版本管理中重新安装搜索组件", msg)
	default:
		return fmt.Errorf("搜索引擎执行失败: %s", msg)
	}
}

// ParseLines 解析 ES 导出文本为全路径行。
// 实测依据（ES 1.1.0.37）：无 es.ini 配置时 stdout 与 -export-tsv 均为【每行一个全路径】的
// 单列布局（目录行以反斜杠结尾），中文经导出文件为可靠 UTF-8（可带 BOM）。
func ParseLines(data []byte) []string {
	// UTF-8 BOM 剥离
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// ReadMeta 用本地文件系统权威元数据补全单个搜索结果（名称/路径/目录标记/大小/修改时间）。
// 结果展示以 stat 为准（索引与磁盘间毫秒级差异由此吞掉）；目标已消失返回 ok=false。
func ReadMeta(fullPath string) (r Result, ok bool) {
	fi, err := os.Stat(fullPath)
	if err != nil && !strings.HasPrefix(fullPath, `\\?\`) {
		fi, err = os.Stat(`\\?\` + fullPath) // 超长路径（>260 字符）兜底
	}
	if err != nil {
		return Result{}, false
	}

	r.IsDir = fi.IsDir()
	r.Modified = fi.ModTime().Format("2006-01-02 15:04")

	// 先归一化（去掉 ES 目录行尾反斜杠），再切名称/父目录——直接对尾反斜杠路径
	// 取 filepath.Dir 会得到它自己而非父目录
	norm := strings.TrimRight(fullPath, `\/`)
	if isVolumeRoot(norm) { // 卷根（C:）
		r.Name = norm + `\`
		r.Path = ""
	} else {
		r.Name = filepath.Base(norm)
		r.Path = filepath.Dir(norm)
		if r.Path == norm {
			r.Path = "" // 无父目录（如 \\server\share 根）
		}
	}
	if !r.IsDir {
		r.Size = fi.Size()
	}
	return r, true
}

// isVolumeRoot 判断归一化路径是否为卷根（如 C:）。
func isVolumeRoot(p string) bool {
	return len(p) == 2 && p[1] == ':'
}
