package ocr

// ---------- F7 托管安装：引擎 zip 契约核心（校验 → tmp 解压 → 原子落位） ----------
//
// 安装件契约（与后厨侧钉死，两端不得擅自变更）：
//   - 每引擎一 zip：包内根即组件目录内容 + 根 manifest.json
//     {"schema":1,"engine":"paddle"|"wechat","version":"4.1.15.9",
//      "entry":"hanxi-ocr.exe","minHanxi":"","note":"中文说明"}；
//   - 旁挂 <name>.zip.sha256（一行 hex，容忍尾空白/BOM）——必检文件，缺失或
//     不符一律拒收；
//   - 命名规范 hanxi-ocr-<engine>-<version>.zip：可解析时必须与 manifest 一致
//     （矛盾拒收），不可解析的文件名以 manifest 为准（内容强、命名宽）。
//
// 落位：versions/hanxi-ocr/<engine>-<version>/（tmp 解压后 rename 原子落位）；
// 同 engine+version+SHA256 幂等返回，不同 SHA256 拒绝覆盖并要求发布新版本号；
// zip 与旁挂件移存 installers/hanxi-ocr/。
//
// 安全闸门：zip 炸弹上限（条目数/单文件与总展开字节）、ZipSlip 路径逃逸拒绝、
// 版本令牌白名单（目录名由版本拼接，注入面收死）。wechat 包只认本地拖入，
// 本文件不存在任何下载/索引入口（BACKLOG F7 三闸纪律）。

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"
)

const (
	// hostedDirName 托管版本树与包缓存目录名（versions/hanxi-ocr、installers/hanxi-ocr）。
	hostedDirName = "hanxi-ocr"
	// hostedManifestSchema 当前唯一支持的契约 schema 版本。
	hostedManifestSchema = 1

	// hostedTmpPrefix 解压中转目录前缀（list 扫描时按前缀忽略）。
	hostedTmpPrefix = ".tmp-"
)

// 解压保护：zip 炸弹上限（条目数 / 单文件展开 / 总展开字节）。
// 上限取"真实组件 × 10"量级：wechat 最小集 ≈42MB、paddle 含模型数百 MB，
// 2GB 总限足够容纳未来大模型包，又能拦住压缩比 1000:1 级别的恶意件。
// var 化供单测收窄预算构造越限场景（测试内串行改回）。
var (
	maxHostedEntries       = 5000
	maxHostedFileBytes     = int64(1 << 30)
	maxHostedTotalBytes    = int64(2 << 30)
	maxHostedManifestBytes = int64(1 << 20)
)

// hostedManifestZipName zip 包内根清单文件名（与 manifestName 同字面量，包内根锚定）。
var hostedManifestZipName = filepath.ToSlash(manifestName)

// hostedVersionRe 托管版本目录名规范：<engine>-<version令牌>。
var hostedVersionRe = regexp.MustCompile(`^(wechat|paddle)-([A-Za-z0-9][A-Za-z0-9._-]{0,63})$`)

// hostedZipNameRe 安装包命名规范：hanxi-ocr-<engine>-<version>.zip（大小写宽容）。
var hostedZipNameRe = regexp.MustCompile(`(?i)^hanxi-ocr-(wechat|paddle)-([A-Za-z0-9][A-Za-z0-9._-]{0,63})\.zip$`)

// versionTokenRe 版本令牌白名单（防目录名注入）：字母数字起头，允许 . _ -，
// 不得以 . 或 - 结尾（Windows 目录名尾部点/连字符非法或语义不定）。
var versionTokenRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// hostedManifest 安装包根 manifest.json 的契约结构。后厨超集令（2026-09-18）：
// 契约六字段之外还携带 name/files/build_date 等额外键——一律忽略向前兼容，
// 严禁 DisallowUnknownFields；files[]（path/sha256）用于解压后逐文件自校验。
type hostedManifest struct {
	Schema   int                   `json:"schema"`
	Engine   string                `json:"engine"`
	Version  string                `json:"version"`
	Entry    string                `json:"entry"`
	MinHanxi string                `json:"minHanxi"`
	Note     string                `json:"note"`
	Files    []hostedManifestEntry `json:"files"`
}

// hostedManifestEntry manifest.files 逐文件摘要件（size 不参与校验，只认 sha256）。
type hostedManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// validate 契约矩阵校验（schema/engine/version/entry 四闸，中文报错）。
func (m hostedManifest) validate() error {
	if m.Schema != hostedManifestSchema {
		return fmt.Errorf("manifest schema 版本不支持：期望 %d，实际 %d（请升级 Hanxi 或向后厨索取新版安装包）", hostedManifestSchema, m.Schema)
	}
	if !isKnownEngine(m.Engine) {
		return fmt.Errorf("manifest engine 字段无效：%q（可选 wechat / paddle）", m.Engine)
	}
	if err := validateVersionToken(m.Version); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(m.Entry), serviceExeName) {
		return fmt.Errorf("manifest entry 契约要求为 %s（实际 %q），本安装包与 Hanxi 不兼容", serviceExeName, m.Entry)
	}
	if err := validateMinHanxi(m.MinHanxi, product.Version); err != nil {
		return err
	}
	return nil
}

// validateMinHanxi 按 SemVer 比较安装包最低 Hanxi 版本；空值保持旧包兼容。
func validateMinHanxi(minVersion, currentVersion string) error {
	minVersion = strings.TrimSpace(minVersion)
	if minVersion == "" {
		return nil
	}
	minSemver, ok := parseHostedSemver(minVersion)
	if !ok {
		return fmt.Errorf("manifest minHanxi %q 不是合法 SemVer", minVersion)
	}
	currentSemver, ok := parseHostedSemver(currentVersion)
	if !ok {
		return fmt.Errorf("当前 Hanxi 版本 %q 不是合法 SemVer，无法校验安装包最低版本要求", currentVersion)
	}
	if compareParsedSemver(currentSemver, minSemver) < 0 {
		return fmt.Errorf("安装包要求 Hanxi >= %s，当前版本为 %s；请先升级 Hanxi", minVersion, currentVersion)
	}
	return nil
}

type hostedSemver struct {
	major uint64
	minor uint64
	patch uint64
	pre   []string
}

func parseHostedSemver(version string) (hostedSemver, bool) {
	var out hostedSemver
	v := strings.TrimSpace(version)
	if strings.HasPrefix(v, "v") {
		v = v[1:]
	}
	if v == "" || strings.ContainsAny(v, " \t\r\n") {
		return out, false
	}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		if i == len(v)-1 || !validSemverIdentifiers(v[i+1:], false) {
			return out, false
		}
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
		if !validSemverIdentifiers(pre, true) {
			return out, false
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	numbers := []*uint64{&out.major, &out.minor, &out.patch}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return hostedSemver{}, false
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return hostedSemver{}, false
		}
		*numbers[i] = value
	}
	if pre != "" {
		out.pre = strings.Split(pre, ".")
	}
	return out, true
}

func validSemverIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	for _, ident := range strings.Split(value, ".") {
		if ident == "" {
			return false
		}
		numeric := true
		for _, r := range ident {
			if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-') {
				return false
			}
			if r < '0' || r > '9' {
				numeric = false
			}
		}
		if rejectNumericLeadingZero && numeric && len(ident) > 1 && ident[0] == '0' {
			return false
		}
	}
	return true
}

func compareParsedSemver(a, b hostedSemver) int {
	for _, pair := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		an, aErr := strconv.ParseUint(a.pre[i], 10, 64)
		bn, bErr := strconv.ParseUint(b.pre[i], 10, 64)
		switch {
		case aErr == nil && bErr == nil:
			if an < bn {
				return -1
			}
			if an > bn {
				return 1
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if cmp := strings.Compare(a.pre[i], b.pre[i]); cmp != 0 {
				return cmp
			}
		}
	}
	return len(a.pre) - len(b.pre)
}

// validateVersionToken 版本令牌白名单校验（中文报错）。
func validateVersionToken(v string) error {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return fmt.Errorf("manifest version 字段为空")
	case !versionTokenRe.MatchString(v):
		return fmt.Errorf("版本号 %q 含非法字符（仅限字母数字与 . _ -，不得以 . 或 - 结尾）", v)
	case strings.HasSuffix(v, ".") || strings.HasSuffix(v, "-"):
		return fmt.Errorf("版本号 %q 不得以 . 或 - 结尾（Windows 目录名限制）", v)
	}
	return nil
}

// hostedVersionDirName 引擎 + 版本 → 托管版本目录名。
func hostedVersionDirName(engine, version string) string {
	return engine + "-" + strings.TrimSpace(version)
}

// compareHostedVersion 托管版本号排序（点分逐段：双数字段按数值，否则按字典序；
// 前缀相同段多者大）。仅供"取最新"启发式使用，不追求 semver 完备。
func compareHostedVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		if aerr == nil && berr == nil {
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
			continue
		}
		if c := strings.Compare(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// hostedManager 托管版本树管理器（纯文件系统，无网络面）。路径注入便于单测。
type hostedManager struct {
	versionsRoot   string // <数据根>/versions/hanxi-ocr
	installersRoot string // <数据根>/installers/hanxi-ocr
	mu             *sync.RWMutex
}

var hostedTreeLocks sync.Map // map[clean versionsRoot]*sync.RWMutex

func hostedTreeLock(versionsRoot string) *sync.RWMutex {
	key := strings.ToLower(filepath.Clean(versionsRoot))
	lock, _ := hostedTreeLocks.LoadOrStore(key, &sync.RWMutex{})
	return lock.(*sync.RWMutex)
}

func deleteHostedTreeLock(versionsRoot string) {
	hostedTreeLocks.Delete(strings.ToLower(filepath.Clean(versionsRoot)))
}

func newHostedManager(versionsRoot, installersRoot string) *hostedManager {
	return &hostedManager{
		versionsRoot:   versionsRoot,
		installersRoot: installersRoot,
		mu:             hostedTreeLock(versionsRoot),
	}
}

// ---------- 校验链 ----------

// inspectHostedZip 打开安装包并跑静态校验（炸弹上限/ZipSlip/manifest 矩阵/
// 命名一致性），返回契约摘要。不产生任何磁盘写入。
func inspectHostedZip(zipPath string) (hostedManifest, string, error) {
	var m hostedManifest
	st, err := os.Stat(zipPath)
	if err != nil || !st.Mode().IsRegular() {
		return m, "", fmt.Errorf("安装包不存在或不是普通文件：%s", zipPath)
	}
	if !strings.EqualFold(filepath.Ext(zipPath), ".zip") {
		return m, "", fmt.Errorf("只接受 .zip 安装包（收到：%s）", filepath.Base(zipPath))
	}
	// 命名规范交叉校验用（可解析才交叉，不可解析以 manifest 为准）
	var nameEngine, nameVersion string
	if g := hostedZipNameRe.FindStringSubmatch(filepath.Base(zipPath)); g != nil {
		nameEngine, nameVersion = strings.ToLower(g[1]), g[2]
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return m, "", fmt.Errorf("无法打开 zip（文件可能损坏）：%v", err)
	}
	defer zr.Close()

	if len(zr.File) == 0 || len(zr.File) > maxHostedEntries {
		return m, "", fmt.Errorf("zip 条目数 %d 超出托管上限（1~%d），拒收", len(zr.File), maxHostedEntries)
	}
	var declared int64
	var manifestRaw []byte
	var hasEntry bool
	for _, f := range zr.File {
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return m, "", fmt.Errorf("zip 含非法路径条目 %q，拒收", f.Name)
		}
		if !f.FileInfo().IsDir() {
			declared += int64(f.UncompressedSize64)
		}
		slashed := filepath.ToSlash(clean)
		switch {
		case slashed == hostedManifestZipName:
			if manifestRaw == nil {
				b, e := readZipFileLimited(f, maxHostedManifestBytes)
				if e != nil {
					return m, "", fmt.Errorf("读取 zip 内 %s 失败: %w", manifestName, e)
				}
				manifestRaw = b
			}
		case slashed == serviceExeName:
			hasEntry = true
		}
	}
	if declared > maxHostedTotalBytes {
		return m, "", fmt.Errorf("zip 展开总大小约 %.1f GB 超出托管上限 %d GB，拒收",
			float64(declared)/(1<<30), maxHostedTotalBytes>>30)
	}
	if manifestRaw == nil {
		return m, "", fmt.Errorf("安装包根目录缺少 %s（包内根须即组件目录内容，勿嵌套外层文件夹）", manifestName)
	}
	if !hasEntry {
		return m, "", fmt.Errorf("安装包根目录缺少入口文件 %s", serviceExeName)
	}
	if err := json.Unmarshal(manifestRaw, &m); err != nil {
		return m, "", fmt.Errorf("manifest.json 解析失败：%w", err)
	}
	if err := m.validate(); err != nil {
		return m, "", err
	}
	if nameEngine != "" {
		if !strings.EqualFold(nameVersion, m.Version) {
			return m, "", fmt.Errorf("文件名版本 %q 与 manifest 版本 %q 矛盾，包与名不同源，拒收", nameVersion, m.Version)
		}
		if nameEngine != strings.ToLower(m.Engine) {
			return m, "", fmt.Errorf("文件名引擎 %q 与 manifest 引擎 %q 矛盾，包与名不同源，拒收", nameEngine, m.Engine)
		}
	}
	return m, zipPath, nil
}

// readZipFileLimited 读取 zip 条目前 n 字节（超限报错）。
func readZipFileLimited(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("条目 %s 超出读取上限", f.Name)
	}
	return data, nil
}

// verifyHostedZipSHA 旁挂件核对：要求 <zipPath>.sha256 存在且与实际哈希一致。
// 内容一行 hex，容忍 BOM 与首尾空白（旁挂常经 PowerShell/浏览器转手）。
func verifyHostedZipSHA(zipPath string) (string, error) {
	sidePath := zipPath + ".sha256"
	raw, err := os.ReadFile(sidePath)
	if err != nil {
		return "", fmt.Errorf("缺少校验文件：%s（与 zip 同目录旁挂一行 sha256；请向后厨取齐三件套再安装）", filepath.Base(sidePath))
	}
	text := strings.TrimPrefix(string(raw), "\ufeff") // UTF-8 BOM 容忍（旁挂件常经 PowerShell 转手）
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", fmt.Errorf("校验文件 %s 内容为空", filepath.Base(sidePath))
	}
	want := strings.ToLower(fields[0])
	if len(want) != 64 {
		return "", fmt.Errorf("校验文件 %s 不是合法的 sha256 摘要（%q）", filepath.Base(sidePath), fields[0])
	}
	if _, e := hex.DecodeString(want); e != nil {
		return "", fmt.Errorf("校验文件 %s 不是十六进制 sha256：%v", filepath.Base(sidePath), e)
	}
	f, e := os.Open(zipPath)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e := io.Copy(h, f); e != nil {
		return "", fmt.Errorf("读取安装包失败: %w", e)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != want {
		return "", fmt.Errorf("SHA256 校验失败：期望 %s，实际 %s（安装包可能已损坏或被篡改）", want, actual)
	}
	return actual, nil
}

// ---------- 安装 ----------

// installZip 完整安装链：sha256 旁挂核对 → 静态校验 → tmp 解压 → 原子落位
// versions/hanxi-ocr/<engine>-<version>/ → zip 与旁挂件移存 installers/。
// 同版本同哈希幂等返回；同版本不同哈希拒绝覆盖，防止版本号内容漂移。
func (hm *hostedManager) installZip(zipPath string) (HostedVersion, error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.installZipLocked(zipPath)
}

func (hm *hostedManager) installZipLocked(zipPath string) (HostedVersion, error) {
	var empty HostedVersion
	zipPath = filepath.Clean(strings.TrimSpace(zipPath))

	zipSHA, err := verifyHostedZipSHA(zipPath)
	if err != nil {
		return empty, err
	}
	m, _, err := inspectHostedZip(zipPath)
	if err != nil {
		return empty, err
	}

	if err := os.MkdirAll(hm.versionsRoot, 0755); err != nil {
		return empty, fmt.Errorf("创建托管目录失败: %w", err)
	}
	target := filepath.Join(hm.versionsRoot, hostedVersionDirName(m.Engine, m.Version))
	if installedSHA, ok := readHostedSHA(filepath.Join(target, "meta.json")); ok {
		if strings.EqualFold(installedSHA, zipSHA) {
			return hm.hostedVersionLocked(target, m), nil
		}
		return empty, fmt.Errorf("%s v%s 已安装，但现有包与本次安装包 SHA256 不同；为防止同版本内容漂移，拒绝覆盖，请发布并使用新版本号",
			engineLabel(m.Engine), m.Version)
	}
	if st, statErr := os.Lstat(target); statErr == nil {
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return empty, fmt.Errorf("托管版本目标不是普通目录，拒绝覆盖：%s", target)
		}
		return empty, fmt.Errorf("%s v%s 已安装但缺少可信 SHA256 元信息；拒绝覆盖，请卸载后重装或发布新版本号",
			engineLabel(m.Engine), m.Version)
	} else if !os.IsNotExist(statErr) {
		return empty, fmt.Errorf("检查现有托管版本失败: %w", statErr)
	}
	tmp, err := os.MkdirTemp(hm.versionsRoot, hostedTmpPrefix+m.Engine+"-")
	if err != nil {
		return empty, fmt.Errorf("创建临时解压目录失败: %w", err)
	}
	if err := extractHostedZip(zipPath, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return empty, err
	}
	// 解压后逐文件自校验（manifest.files 为包内完整性自证，缺失则跳过——兼容旧包）
	if err := verifyHostedFiles(tmp, m.Files); err != nil {
		_ = os.RemoveAll(tmp)
		return empty, err
	}

	// 目标不存在才原子落位；同 engine+version 已在前面按包哈希判定幂等或拒绝。
	if err := os.Rename(tmp, target); err != nil {
		_ = os.RemoveAll(tmp)
		return empty, fmt.Errorf("版本目录落位失败: %w", err)
	}

	// 元信息（安装时间/包哈希/来源）与托管族 meta.json 口径一致
	installedAt := time.Now().Format("2006-01-02 15:04:05")
	if err := writeHostedJSON(filepath.Join(target, "meta.json"), map[string]any{
		"installedAt": installedAt,
		"isImport":    true,
		"sha256":      zipSHA,
		"source":      filepath.Base(zipPath),
		"engine":      m.Engine,
		"version":     m.Version,
	}); err != nil {
		_ = os.RemoveAll(target)
		return empty, fmt.Errorf("写入托管版本元信息失败: %w", err)
	}

	// zip 原件移存 installers/（移存失败不判安装失败：组件已落位，仅归档降级为警告）
	if err := hm.archiveInstaller(zipPath); err != nil {
		slog.Warn("ocr 托管安装：安装包归档 installers/ 失败（不影响已安装版本）", "zip", zipPath, "err", err)
	}

	exe := filepath.Join(target, serviceExeName)
	st, err := os.Stat(exe)
	size := int64(0)
	if err == nil {
		size = st.Size()
	}
	return HostedVersion{
		Engine:      m.Engine,
		Version:     m.Version,
		Dir:         target,
		ExePath:     exe,
		Size:        size,
		InstalledAt: installedAt,
		Note:        m.Note,
		State:       hostedStateReady,
	}, nil
}

func readHostedSHA(metaPath string) (string, bool) {
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return "", false
	}
	var meta struct {
		SHA256 string `json:"sha256"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return "", false
	}
	sha := strings.ToLower(strings.TrimSpace(meta.SHA256))
	if len(sha) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(sha); err != nil {
		return "", false
	}
	return sha, true
}

func (hm *hostedManager) hostedVersionLocked(target string, m hostedManifest) HostedVersion {
	exe := filepath.Join(target, serviceExeName)
	state := hostedStateReady
	errText := ""
	size := int64(0)
	if st, err := os.Lstat(exe); err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		state = hostedStateBroken
		errText = fmt.Sprintf("入口文件缺失或损坏：%s", exe)
	} else {
		size = st.Size()
	}
	return HostedVersion{
		Engine:      m.Engine,
		Version:     m.Version,
		Dir:         target,
		ExePath:     exe,
		Size:        size,
		InstalledAt: readHostedInstalledAt(filepath.Join(target, "meta.json")),
		Note:        m.Note,
		State:       state,
		Error:       errText,
	}
}

// verifyHostedFiles 按 manifest.files 逐文件复核解压结果（sha256 一致才算装好）。
// 空清单跳过（契约只钉六字段，files 为后厨超集扩展）；清单里的路径同样过
// ZipSlip 闸——manifest 由包自带，不可信其字面。
func verifyHostedFiles(targetDir string, files []hostedManifestEntry) error {
	for _, fe := range files {
		clean := filepath.Clean(filepath.FromSlash(fe.Path))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("manifest.files 含非法路径 %q，拒收", fe.Path)
		}
		want := strings.ToLower(strings.TrimSpace(fe.SHA256))
		if len(want) != 64 {
			return fmt.Errorf("manifest.files[%s] 摘要不是合法 sha256：%q", fe.Path, fe.SHA256)
		}
		full := filepath.Join(targetDir, clean)
		f, err := os.Open(full)
		if err != nil {
			return fmt.Errorf("解压后缺少 manifest 点名的文件 %s：%w", fe.Path, err)
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		f.Close()
		if copyErr != nil {
			return fmt.Errorf("读取 %s 失败: %w", fe.Path, copyErr)
		}
		if actual := hex.EncodeToString(h.Sum(nil)); actual != want {
			return fmt.Errorf("逐文件校验失败：%s（期望 %s，实际 %s）", fe.Path, want, actual)
		}
	}
	return nil
}

// extractHostedZip 全量保布局解压（ZipSlip 拒绝 + 炸弹逐字节上限 + 读满触发 CRC）。
// 任一条目违规即报错中止，由调用方清理 tmp。
func extractHostedZip(zipPath, targetDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	var total int64
	for _, f := range zr.File {
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("zip 含非法路径条目 %q，拒收", f.Name)
		}
		target := filepath.Join(targetDir, clean)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if f.UncompressedSize64 > uint64(maxHostedFileBytes) {
			return fmt.Errorf("条目 %s 声明展开 %.1f GB，超单文件上限 %d GB，拒收",
				f.Name, float64(f.UncompressedSize64)/(1<<30), maxHostedFileBytes>>30)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		cw := &cappedWriter{w: out, maxFile: maxHostedFileBytes, total: &total, maxTotal: maxHostedTotalBytes}
		// 必须读满：提前返回会跳过 archive/zip 内建 CRC32 校验
		_, copyErr := io.Copy(cw, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return fmt.Errorf("解压条目 %s 失败: %w", f.Name, copyErr)
		}
	}
	// 入口与清单落盘自检（zip 头声明与磁盘实况双保险）
	if !isRegularNonEmpty(filepath.Join(targetDir, serviceExeName)) {
		return fmt.Errorf("解压后缺少可用的入口文件 %s", serviceExeName)
	}
	if !isRegularNonEmpty(filepath.Join(targetDir, manifestName)) {
		return fmt.Errorf("解压后缺少 %s", manifestName)
	}
	return nil
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
		return 0, fmt.Errorf("条目展开超过单文件上限 %d GB（zip 炸弹防护）", c.maxFile>>30)
	}
	if *c.total+n > c.maxTotal {
		return 0, fmt.Errorf("zip 展开总大小超过上限 %d GB（zip 炸弹防护）", c.maxTotal>>30)
	}
	written, err := c.w.Write(p)
	c.written += int64(written)
	*c.total += int64(written)
	return written, err
}

// archiveInstaller zip 与旁挂件移存 installers/hanxi-ocr/（异卷 rename 失败退化为复制+删除）。
func (hm *hostedManager) archiveInstaller(zipPath string) error {
	if err := os.MkdirAll(hm.installersRoot, 0755); err != nil {
		return err
	}
	name := filepath.Base(zipPath)
	if err := moveFile(zipPath, filepath.Join(hm.installersRoot, name)); err != nil {
		return err
	}
	side := zipPath + ".sha256"
	if isRegularFile(side) {
		_ = moveFile(side, filepath.Join(hm.installersRoot, filepath.Base(side))) // 旁挂尽力归档，失败不影响
	}
	return nil
}

// moveFile 尽力移动：先 rename（Windows 同卷原子、覆盖语义），跨卷退化为复制+删除。
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		in.Close()
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		in.Close()
		out.Close()
		return err
	}
	in.Close()
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// ---------- 列表 / 解析 / 卸载 ----------

const (
	hostedStateReady  = "ready"
	hostedStateBroken = "broken"
)

// list 扫描托管版本树（engine 升序按 engineOrder，engine 内版本降序）。
// 条目名不合规的目录直接忽略（可能是外来文件）；合规但入口损坏的列入 broken 态。
func (hm *hostedManager) list() []HostedVersion {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.listLocked()
}

func (hm *hostedManager) listLocked() []HostedVersion {
	entries, err := os.ReadDir(hm.versionsRoot)
	if err != nil {
		return nil
	}
	var out []HostedVersion
	for _, ent := range entries {
		if !ent.IsDir() || strings.HasPrefix(ent.Name(), hostedTmpPrefix) || strings.Contains(ent.Name(), ".old-") {
			continue
		}
		g := hostedVersionRe.FindStringSubmatch(ent.Name())
		if g == nil {
			continue
		}
		dir := filepath.Join(hm.versionsRoot, ent.Name())
		info := HostedVersion{
			Engine:  g[1],
			Version: g[2],
			Dir:     dir,
			ExePath: filepath.Join(dir, serviceExeName),
			State:   hostedStateReady,
		}
		switch st, e := os.Lstat(info.ExePath); {
		case e != nil || !st.Mode().IsRegular() || st.Size() == 0:
			info.State = hostedStateBroken
			info.Error = fmt.Sprintf("入口文件缺失或损坏：%s", info.ExePath)
		default:
			info.Size = st.Size()
		}
		if raw, e := os.ReadFile(filepath.Join(dir, manifestName)); e == nil {
			var m hostedManifest
			if json.Unmarshal(raw, &m) == nil {
				info.Note = m.Note
			}
		}
		info.InstalledAt = readHostedInstalledAt(filepath.Join(dir, "meta.json"))
		out = append(out, info)
	}
	// 排序：engineOrder 次序 → 版本降序
	order := map[string]int{}
	for i, id := range engineOrder {
		order[id] = i
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			if order[a.Engine] < order[b.Engine] ||
				(order[a.Engine] == order[b.Engine] && compareHostedVersion(a.Version, b.Version) >= 0) {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// readHostedInstalledAt 从 meta.json 取安装时间（缺省回退目录 mtime）。
func readHostedInstalledAt(metaPath string) string {
	if raw, err := os.ReadFile(metaPath); err == nil {
		var meta struct {
			InstalledAt string `json:"installedAt"`
		}
		if json.Unmarshal(raw, &meta) == nil && meta.InstalledAt != "" {
			return meta.InstalledAt
		}
	}
	if st, err := os.Stat(filepath.Dir(metaPath)); err == nil {
		return st.ModTime().Format("2006-01-02 15:04:05")
	}
	return ""
}

// resolve 解析引擎的托管最新可用版本（manifest + 入口双检），返回 exe 与版本号。
func (hm *hostedManager) resolveLatest(engineID string) (exe, version string, ok bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hostedResolveLatestLocked(hm.versionsRoot, engineID)
}

// hostedResolveLatest 保留纯路径入口供解析链与单测使用，并与同根 manager 共用树读锁。
// hostedRoot 为空（未接线/测试旧路径）恒返回 ok=false。
func hostedResolveLatest(hostedRoot, engineID string) (exe, version string, ok bool) {
	lock := hostedTreeLock(hostedRoot)
	lock.RLock()
	defer lock.RUnlock()
	return hostedResolveLatestLocked(hostedRoot, engineID)
}

func hostedResolveLatestLocked(hostedRoot, engineID string) (exe, version string, ok bool) {
	if strings.TrimSpace(hostedRoot) == "" || !isKnownEngine(engineID) {
		return "", "", false
	}
	entries, err := os.ReadDir(hostedRoot)
	if err != nil {
		return "", "", false
	}
	bestVer, bestExe := "", ""
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		g := hostedVersionRe.FindStringSubmatch(ent.Name())
		if g == nil || g[1] != engineID {
			continue
		}
		dir := filepath.Join(hostedRoot, ent.Name())
		e := filepath.Join(dir, serviceExeName)
		if !isRegularNonEmpty(e) || !isRegularNonEmpty(filepath.Join(dir, manifestName)) {
			continue
		}
		if bestExe == "" || compareHostedVersion(g[2], bestVer) > 0 {
			bestVer, bestExe = g[2], e
		}
	}
	if bestExe == "" {
		return "", "", false
	}
	return bestExe, bestVer, true
}

// hostedDirOfExe 判定路径是否落在托管版本树内（含已卸载残留的登记件），
// 返回其版本目录路径；不在树内返回 ""。登记件与托管树的从属关系判据。
func hostedDirOfExe(hostedRoot, exe string) string {
	if strings.TrimSpace(hostedRoot) == "" || strings.TrimSpace(exe) == "" {
		return ""
	}
	root := filepath.Clean(hostedRoot)
	p := filepath.Clean(exe)
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return ""
	}
	parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) < 2 || parts[0] == ".." || hostedVersionRe.FindStringSubmatch(strings.ToLower(parts[0])) == nil {
		return ""
	}
	candidate := filepath.Join(root, parts[0])
	expected, err := filepath.Rel(root, candidate)
	if err != nil || !strings.EqualFold(expected, parts[0]) {
		return ""
	}
	return candidate
}

// remove 删除一个托管版本目录（名字白名单 + 拒绝符号链接目录，RemoveAll 收口）。
func (hm *hostedManager) remove(engine, version string) (string, error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.removeLocked(engine, version)
}

func (hm *hostedManager) removeLocked(engine, version string) (string, error) {
	name := hostedVersionDirName(engine, version)
	if hostedVersionRe.FindStringSubmatch(name) == nil {
		return "", fmt.Errorf("非法的托管版本名：%q", name)
	}
	dir := filepath.Join(hm.versionsRoot, name)
	st, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("版本未安装：%s（%s）", name, err)
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("托管版本目录不是普通目录，拒绝删除：%s", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return dir, fmt.Errorf("删除版本目录失败（组件可能正在运行，请先停止识别服务）：%w", err)
	}
	return dir, nil
}

func isRegularNonEmpty(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func writeHostedJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
