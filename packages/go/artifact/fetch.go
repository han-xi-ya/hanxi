package artifact

// ---------- 受控下载：官方摘要必检，镜像只是传输来源 ----------
//
// Fetch 的完整性四层兜底（与 ocr/litemonitor 已验证经验同构）：
//  1. 官方 SHA-256 必检（缺失或不符一律拒收，镜像不配有"信任"）；
//  2. Content-Length 声明与实收字节双核（防截断/代理篡改）；
//  3. 流式大小上限断言（声明超限直接拒收，未声明则边收边核）；
//  4. 落盘后全量重读复核摘要（"全核"，与流式哈希互为印证）。
//
// 网络纪律（PLAN §10.5）：
//   - 只允许 https；http 仅限回环地址（本地联调/测试服务）；
//   - 重定向可跟随但不跨 scheme，且逐跳复检上述规则；
//   - 回环请求强制绕过代理（Transport 层 Proxy=nil），
//     避免用户环境变量里的代理劫持本机 HTTP 探测；
//   - 错误与进度不回显带 query 的完整 URL（可能含 token），统一脱敏。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// fetchUserAgent 下载标识（便于上游/镜像侧统计与排障）。
	fetchUserAgent = "hanxi-artifact/1.0"
	// defaultFetchTimeout 调用方未给出预算时的总超时兜底。
	defaultFetchTimeout = 10 * time.Minute
	// defaultFetchMaxBytes 调用方未设上限时的流式大小兜底（4 GiB）。
	defaultFetchMaxBytes = int64(4 << 30)
	// maxRedirects 单候选源允许的重定向跳数。
	maxRedirects = 10
)

// Source 描述一次受控下载。SHA256 为官方摘要（必检），不信任镜像本身，
// Mirrors 只是同摘要的备用传输来源。
type Source struct {
	URL      string   // 主地址（HTTPS）
	Mirrors  []string // 备用同摘要镜像
	SHA256   string   // 期望摘要（64 hex），必检
	MaxBytes int64    // 大小上限（流式断言，Content-Length 与实收都核；<=0 视为未设，回退 4GiB 兜底）
	FileName string   // 落地名（sanitize 后；仅用作临时件命名与诊断）
}

// fetchTransport 共享传输层：外部走系统代理，回环一律直连。
var fetchTransport = &http.Transport{
	Proxy: func(req *http.Request) (*url.URL, error) {
		if isLoopbackHost(req.URL.Hostname()) {
			return nil, nil // 回环禁用代理（本机服务/测试服务器不可被环境代理劫持）
		}
		return http.ProxyFromEnvironment(req)
	},
	DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout: 15 * time.Second,
	MaxIdleConnsPerHost: 2,
}

// Fetch 下载 → 流式 SHA-256 → 全核 → 原子 rename 到 destPath。
// ctx 取消即删临时件；timeout 为整个 Fetch（含镜像回退）的超时预算，
// <=0 时回退 defaultFetchTimeout；重定向跟随但不跨 scheme。
func Fetch(ctx context.Context, src Source, destPath string, prog func(Progress), timeout time.Duration) error {
	emit := func(stage string, done, total int64, msg string) {
		if prog != nil {
			prog(Progress{Stage: stage, Done: done, Total: total, Message: msg})
		}
	}
	fail := func(err error) error {
		emit(StageError, 0, 0, err.Error())
		return err
	}

	// ---- 参数闸（先审配置，再碰网络） ----
	want := strings.ToLower(strings.TrimSpace(src.SHA256))
	if want == "" {
		return fail(errors.New("必须提供官方 SHA-256 摘要（托管下载不允许无校验安装）"))
	}
	if len(want) != 64 {
		return fail(fmt.Errorf("SHA256 摘要 %q 不是合法的 64 位十六进制", src.SHA256))
	}
	if _, err := hex.DecodeString(want); err != nil {
		return fail(fmt.Errorf("SHA256 摘要 %q 不是合法的十六进制: %w", src.SHA256, err))
	}
	maxBytes := src.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultFetchMaxBytes
	}
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		return fail(errors.New("destPath 不能为空"))
	}
	if strings.ContainsAny(src.FileName, `/\`) {
		// 落地名只应为基名；含分隔符即调用方拼接有误，直接拒（错误早、可诊断）
		return fail(fmt.Errorf("FileName %q 含路径分隔符（落地名只允许基名）", src.FileName))
	}

	// ---- 候选源审校（主地址非法即失败；镜像非法同样报错，不静默吞配置错误） ----
	endpoints := make([]*url.URL, 0, 1+len(src.Mirrors))
	primary, err := validateEndpoint(src.URL)
	if err != nil {
		return fail(fmt.Errorf("主下载地址无效: %w", err))
	}
	endpoints = append(endpoints, primary)
	for _, m := range src.Mirrors {
		if strings.TrimSpace(m) == "" {
			continue
		}
		u, err := validateEndpoint(m)
		if err != nil {
			return fail(fmt.Errorf("镜像地址 %s 无效: %w", redactURL(m), err))
		}
		endpoints = append(endpoints, u)
	}

	// ---- 超时预算：参数化，挂在调用方 ctx 之下（取消语义优先） ----
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	emit(StageResolve, 0, 0, fmt.Sprintf("准备下载 %s（共 %d 个候选源）", sanitizeFilePart(src.FileName), len(endpoints)))

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fail(fmt.Errorf("创建下载目录失败: %w", err))
	}

	client := &http.Client{Transport: fetchTransport, CheckRedirect: checkRedirect}
	partPrefix := filepath.Join(filepath.Dir(destPath),
		filepathBase(destPath)+"."+sanitizeFilePart(src.FileName)+".part-")

	var lastErr error
	for _, u := range endpoints {
		partPath := partPrefix + randomHex(6)
		err := fetchOne(opCtx, client, u, partPath, want, maxBytes,
			func(done, total int64) { emit(StageDownload, done, total, "") })
		if err == nil {
			emit(StageVerify, 0, 0, "流式与落盘 SHA-256 全核通过，准备原子落位")
			// 原子落位：Windows 同卷 rename 可替换已存在同名文件。
			if err := os.Rename(partPath, destPath); err != nil {
				_ = os.Remove(partPath)
				return fail(fmt.Errorf("下载文件落位失败: %w", err))
			}
			emit(StageDone, 1, 1, fmt.Sprintf("下载完成并通过 SHA-256 全核：%s", filepath.Base(destPath)))
			return nil
		}
		_ = os.Remove(partPath) // 失败即清临时件，不留垃圾
		lastErr = err
		// 取消/超时属于全局预算，不再换源重试
		if ctxErr := opCtx.Err(); ctxErr != nil {
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return fail(fmt.Errorf("下载超时（预算 %s）: %w", timeout, err))
			}
			return fail(fmt.Errorf("下载已取消: %w", err))
		}
	}
	return fail(fmt.Errorf("所有候选源均失败，最后错误: %w", lastErr))
}

// fetchOne 从单个候选源下载到 partPath：
// 流式 SHA-256 + 字节双核 + 超限断言 + 落盘全量复核。成功时临时件留在盘上待 rename。
func fetchOne(ctx context.Context, client *http.Client, u *url.URL, partPath, wantSHA string, maxBytes int64, onBytes func(done, total int64)) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", fetchUserAgent)

	out, err := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("创建临时下载文件失败: %w", err)
	}
	defer func() {
		if err != nil {
			out.Close()
		}
	}()

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s 失败: %w", redactURL(u.String()), unwrapURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d（%s）", resp.StatusCode, redactURL(u.String()))
	}

	total := int64(0)
	if resp.ContentLength > 0 {
		total = resp.ContentLength
		if total > maxBytes {
			return fmt.Errorf("服务器声明大小 %d 字节超过上限 %d 字节，拒收", total, maxBytes)
		}
	}

	h := sha256.New()
	buf := make([]byte, 64*1024)
	var done int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if done+int64(n) > maxBytes {
				return fmt.Errorf("实收字节超过上限 %d（zip 炸弹/异常放大防护）", maxBytes)
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fmt.Errorf("写入临时文件失败: %w", werr)
			}
			h.Write(buf[:n])
			done += int64(n)
			if onBytes != nil {
				onBytes(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("传输中断（已收 %d 字节）: %w", done, readErr)
		}
	}
	if total > 0 && done != total {
		return fmt.Errorf("下载不完整：声明 %d 字节，实际 %d 字节", total, done)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return fmt.Errorf("落盘同步失败: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	// 流式哈希即时比对（先报错省一次磁盘遍历）
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA) {
		return fmt.Errorf("SHA256 校验失败：期望 %s，实际 %s（传输损坏或被篡改，%s 不可信）", wantSHA, got, redactURL(u.String()))
	}
	// 全核：落盘文件重读复核（防写盘路径上的损坏/静默截断）
	actual, err := fileSHA256(partPath)
	if err != nil {
		return err
	}
	if actual != wantSHA {
		return fmt.Errorf("落盘 SHA256 复核失败：期望 %s，实际 %s", wantSHA, actual)
	}
	return nil
}

// validateEndpoint 审校下载端点：只允许 https；http 仅限回环。
func validateEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("URL 解析失败: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("非回环地址禁用明文 http（必须 HTTPS）")
		}
	default:
		return nil, fmt.Errorf("不支持的协议 %q（仅允许 https 及回环 http）", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("URL 缺少主机名")
	}
	return u, nil
}

// checkRedirect 重定向闸门：不跨 scheme、跳数受限、逐跳复检端点规则。
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("重定向超过 %d 跳上限", maxRedirects)
	}
	origin := strings.ToLower(via[0].URL.Scheme)
	target := strings.ToLower(req.URL.Scheme)
	if origin != target {
		return fmt.Errorf("拒绝跨协议重定向 %s -> %s（降级/逃逸防护）", origin, target)
	}
	if _, err := validateEndpoint(req.URL.String()); err != nil {
		return fmt.Errorf("重定向目标被拒: %w", err)
	}
	return nil
}

// isLoopbackHost 判定主机名是否回环（localhost / 127.0.0.0/8 / ::1）。
func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redactURL 抹去 URL 中的 query/fragment/用户名（可能含 token），只留可诊断的骨架。
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<无效URL>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// unwrapURLError 把 *url.Error 的噪声剥掉，只留原始错误。
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// partResidueRe 匹配 Fetch 强杀残件的落地形状：`.part-` 标记后为纯小写十六
// 进制随机后缀且到此结尾（randomHex(6) = 12 位；熵源退化时 UnixNano 的 %x
// 为 16 位，取 {6,} 留裕度）。锚定结尾并要求 hex 字符集，避免误伤恰含
// ".part-" 字样的外来文件（如 report.part-1.zip）。
var partResidueRe = regexp.MustCompile(`\.part-[0-9a-f]{6,}$`)

// CleanStaleParts 受控清理入口：收尸 Fetch 遭遇强杀时来不及删除的
// `<name>.<rand>.part-<hex>` 临时件（目录面 CleanupAbandoned/AbandonedDirs
// 只认事务目录前缀，文件面残件由本函数负责，见 ADR-0002 §4）。
//
// 纪律：
//   - 逐目录浅扫（不递归），目录不存在/不可读静默跳过（懒建根下属常态）；
//   - 三条件齐备才删：文件名匹配 .part-<hex> 形状、Lstat 为普通文件
//     （拒目录与符号链接占名）、修改时间早于 olderThan 截止线
//     （活跃下载的 mtime 随写盘持续刷新，天然受保护）；
//   - 返回实际删除的文件路径清单（供调用方落日志）；单项删除失败
//     （仍被占用/权限拒绝）静默跳过，留待下次启动重试。
func CleanStaleParts(dirs []string, olderThan time.Duration) []string {
	if olderThan <= 0 {
		return nil // 非正预算视为调用方配置错误，宁可不删不误删
	}
	cutoff := time.Now().Add(-olderThan)
	var removed []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if !partResidueRe.MatchString(ent.Name()) {
				continue
			}
			full := filepath.Join(dir, ent.Name())
			st, err := os.Lstat(full)
			if err != nil || !st.Mode().IsRegular() {
				continue // 目录/链接占名：形状命中也不碰
			}
			if st.ModTime().After(cutoff) {
				continue // 未超龄：可能是仍在推进的下载
			}
			if err := os.Remove(full); err != nil {
				continue // 删不掉留待下次收尸
			}
			removed = append(removed, full)
		}
	}
	return removed
}

// randomHex 生成 n 字节随机十六进制串（临时件唯一后缀）。
func randomHex(n int) string {
	b := make([]byte, n)
	// crypto/rand 读失败属系统熵源故障，退化为时间戳后缀（仍保证唯一性足够）
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
