package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform/windows"
)

// gitEngine：外部 git + 分离仓库（PLAN §2.5 仓库位置裁定）。
// `--git-dir=<数据根>/.snapshots/repo.git --work-tree=<数据根>` 每条命令固定两前缀，
// 数据面零污染（用户目录不落 .gitignore、不与用户自行 git init 冲突）。
// 红线：本文件不存在任何 remote/push 语义——历史永远只在这块盘上。

// localStaleLockAfter 陈旧 index.lock 隔离阈值（MooTool 同款 5min：小于此值可能
// 真有并发 git 进程，宁可不拍也不能抢删别人锁）。
const localStaleLockAfter = 5 * time.Minute

// porcelainEntry git status --porcelain=v1 -z 的单条记录。
type porcelainEntry struct {
	Status string // 两字符 XY（X=index 态，Y=worktree 态）
	Path   string
	Orig   string // 仅 R/C：原路径
}

// gitEngine 与影子备份引擎实现同一 engine 接口，服务层对两模式无感。
type gitEngine struct {
	exe      string
	gitDir   string
	workTree string

	mu sync.Mutex // 仓库级串行闸：tick / 手动 / 退出补拍三源共此一仓，忙即跳过不排队
}

func newGitEngine(dataDir, snapshotDir string, probe gitProbe) (*gitEngine, error) {
	g := &gitEngine{
		exe:      probe.Path,
		gitDir:   filepath.Join(snapshotDir, gitRepoDirName),
		workTree: dataDir,
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := g.ensureRepo(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *gitEngine) mode() string { return ModeGit }

// command 构造统一防护参数的一次 git 调用（PLAN §2.5 必带参数清单）：
//   - GIT_OPTIONAL_LOCKS=0：status 不再顺手刷 index（免与真写 index 者抢锁）；
//   - GIT_TERMINAL_PROMPT=0：任何场景都不得弹终端提问（静默红线）；
//   - GIT_CONFIG_NOSYSTEM=1：系统级配置（含模板 hooks 等怪癖）不得影响快照；
//   - -c 身份钉死：提交身份 = Hanxi 快照身份，绝不读写用户全局配置；
//   - core.autocrlf=false：JSON/md 原样入库，禁止换行改写；
//   - commit.gpgsign=false：用户全局开了签名也不静默炸；
//   - safe.directory：提权实例（Administrator 属主）下防 dubious-ownership 拒命；
//   - commit --no-verify：用户本地 hooks 拦不住快照（pre-commit/commit-msg 全跳）。
func (g *gitEngine) command(ctx context.Context, args ...string) *exec.Cmd {
	head := []string{
		"--git-dir=" + g.gitDir,
		"--work-tree=" + g.workTree,
		"-c", "user.name=Hanxi",
		"-c", "user.email=snapshot@hanxi.local",
		"-c", "core.autocrlf=false",
		"-c", "commit.gpgsign=false",
		"-c", "safe.directory=" + filepath.Clean(g.workTree),
	}
	cmd := exec.CommandContext(ctx, g.exe, append(head, args...)...)
	// 工作目录钉在数据根：status 相对路径、pathspec、cwd 推导全部以最自然口径生效
	cmd.Dir = g.workTree
	windows.HideConsole(cmd)
	cmd.Env = append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	return cmd
}

// run 执行并返回 stdout/stderr 文本（错误合并供 index.lock 判词）。
func (g *gitEngine) run(ctx context.Context, args ...string) (string, string, error) {
	cmd := g.command(ctx, args...)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	return so.String(), se.String(), err
}

// ensureRepo 首次 init（幂等：HEAD 存在即已建）+ local 配置 + info/exclude 黑名单。
func (g *gitEngine) ensureRepo(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.gitDir, "HEAD")); err == nil {
		// 已初始化：exclude 仍要重写一次——升级新增排除模式（如 memo.json.migrated）
		// 得补进存量仓库；写失败沿用既有规则继续，不拦门。
		if err := g.writeExclude(); err != nil {
			slog.Warn("snapshot: info/exclude 刷新失败（沿用既有规则继续）", "err", err)
		}
		return nil
	}
	if err := os.MkdirAll(g.snapshotParent(), 0755); err != nil {
		return err
	}
	if _, se, err := g.run(ctx, "init", "-q"); err != nil {
		return fmt.Errorf("git init 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	// local 身份持久化一份（-c 参数每次仍带，此为双保险，PLAN"一次性设 local"）
	for _, kv := range [][2]string{
		{"user.name", "Hanxi"},
		{"user.email", "snapshot@hanxi.local"},
		{"core.autocrlf", "false"},
		{"commit.gpgsign", "false"},
	} {
		if _, _, err := g.run(ctx, "config", "--local", kv[0], kv[1]); err != nil {
			return fmt.Errorf("git config --local %s 失败", kv[0])
		}
	}
	return g.writeExclude()
}

// snapshotParent .snapshots 目录本身。
func (g *gitEngine) snapshotParent() string { return filepath.Dir(g.gitDir) }

// writeExclude 反向白名单写进 git-dir 的 info/exclude（--git-dir 隔离后数据根不落
// 任何 git 可见文件，PLAN §2.5）：先顶层全忽略、再逐条放行白名单——作用域判定
// 与 whitelist.go 的 Whitelisted() 保持同一份清单（改一边必须改两边）。
// 相比命令级 pathspec 限定，exclude 方案的实质优势：白名单目录整个消失
// （如 memo 库清空）时，git 对"已跟踪文件的删除"仍会上报，历史不会卡在幽灵文件上。
func (g *gitEngine) writeExclude() error {
	infoDir := filepath.Join(g.gitDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		return err
	}
	content := strings.Join([]string{
		"# hanxi snapshot 自动维护：白名单之外的数据根内容一律不入库",
		"/*",
		"!/" + rootConfig,
		"!/" + rootState + "/",
		"!/" + rootMemo + "/",
		// 白名单内再排中间产物（不带斜杠的模式任意层级生效：
		// memo.json.tmp.<pid> 藏在 state/ 下也拦住）
		"*.tmp.*",
		"*.corrupt-*",
		migratedMemoName,
		"",
	}, "\n")
	return os.WriteFile(filepath.Join(infoDir, "exclude"), []byte(content), 0644)
}

// changes status 全量 + Whitelisted 双保险过滤（作用域实际由 info/exclude 反向白名单
// 圈定，此处过滤防 exclude 刷新失败/存量仓库规则落后时越界入保）。
func (g *gitEngine) changes(ctx context.Context) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, se, err := g.run(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("git status 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	var files []string
	seen := make(map[string]bool)
	for _, e := range parsePorcelain(out) {
		path := e.Path
		if e.Orig != "" && Whitelisted(e.Orig) && !Whitelisted(path) {
			path = e.Orig // 改名出白名单：旧路径的删除也要入保
		}
		if !Whitelisted(path) || seen[path] {
			continue
		}
		allowMissing := e.Status[0] == 'D' || e.Status[1] == 'D' || (e.Orig != "" && path == e.Orig)
		if err := validateWhitelistPathOnDisk(g.workTree, path, allowMissing); err != nil {
			return nil, err
		}
		seen[path] = true
		files = append(files, path)
	}
	return files, nil
}

// commit：add -A（作用域=exclude）→ 人话摘要 commit（无变更视为成功 skip——与
// changes 之间的竞态窗口就靠 git 自身的 "nothing to commit" 语义收口）。
func (g *gitEngine) commit(ctx context.Context, files []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, err := enumerateWhitelist(g.workTree, true); err != nil {
		return err
	}
	if _, se, err := g.runLockedAware(ctx, "add", "-A"); err != nil {
		return fmt.Errorf("git add 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	msg := "checkpoint: " + summarizeFiles(files)
	if _, se, err := g.runLockedAware(ctx, "commit", "-q", "--no-verify", "-m", msg); err != nil {
		if strings.Contains(se, "nothing to commit") {
			return nil // 竞态窗口内容归零：skip 即正确
		}
		return fmt.Errorf("git commit 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	return nil
}

// runLockedAware 带陈旧 index.lock 恢复的一次性重试（PLAN §2.5）：
// 报错含 index.lock → 锁龄超阈值才 rename 隔离（防抢删活锁）→ 重试原命令 → 删隔离件。
// 调用方必须已持 g.mu。
func (g *gitEngine) runLockedAware(ctx context.Context, args ...string) (string, string, error) {
	out, se, err := g.run(ctx, args...)
	if err == nil || !strings.Contains(se, "index.lock") {
		return out, se, err
	}
	lock := filepath.Join(g.gitDir, "index.lock")
	if !lockIsStale(lock) {
		return out, se, err // 锁新鲜：可能真有并发 git，本轮放弃，下个 tick 再来
	}
	quarantined := fmt.Sprintf("%s.stale-%s", lock, time.Now().Format(backupTimeLayout))
	if rerr := os.Rename(lock, quarantined); rerr != nil {
		return out, se, err
	}
	slog.Warn("snapshot: 隔离陈旧 index.lock 后重试", "lock", lock, "age_over", localStaleLockAfter)
	out, se, err = g.run(ctx, args...)
	_ = os.Remove(quarantined)
	return out, se, err
}

// lockIsStale 锁文件存在且 mtime 超阈值（Go 侧不做句柄探测，mtime 判据已足：
// 快照是本进程单飞闸保护下的唯一写者，出现活锁只可能是上次进程暴尸）。
func lockIsStale(lock string) bool {
	fi, err := os.Stat(lock)
	if err != nil {
		return false
	}
	return time.Since(fi.ModTime()) > localStaleLockAfter
}

// revisions git log 精简形态（%x1f 字段分隔 + -z 记录分隔，正文自产无分隔符）。
func (g *gitEngine) revisions(ctx context.Context, limit int) ([]Revision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, se, err := g.run(ctx,
		"log", "-z", fmt.Sprintf("--max-count=%d", limit),
		"--pretty=format:%H%x1f%ct%x1f%s")
	if err != nil {
		// 空仓库（首拍前）不是错误
		if strings.Contains(se, "does not have any commits yet") {
			return nil, nil
		}
		return nil, fmt.Errorf("git log 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	return parseGitLog(out), nil
}

// parseGitLog 拆 git log -z 记录（独立函数供 fixture 单测）。
func parseGitLog(out string) []Revision {
	revs := make([]Revision, 0, 16)
	for _, rec := range strings.Split(out, "\x00") {
		rec = strings.Trim(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		fields := strings.SplitN(rec, "\x1f", 3)
		if len(fields) != 3 {
			continue
		}
		var unixSec int64
		if _, err := fmt.Sscanf(fields[1], "%d", &unixSec); err != nil {
			continue
		}
		revs = append(revs, Revision{
			ID:      fields[0],
			Time:    time.Unix(unixSec, 0).Format(time.RFC3339),
			Summary: strings.TrimPrefix(fields[2], "checkpoint: "),
		})
	}
	return revs
}

// revisionFiles：git show --name-status --format=<id> 的清单解析。
func (g *gitEngine) revisionFiles(ctx context.Context, id string) ([]RevisionFile, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, se, err := g.run(ctx, "show", "--name-status", "--format=", id)
	if err != nil {
		return nil, fmt.Errorf("git show 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	return parseNameStatus(out), nil
}

// parseNameStatus 拆 `git show --name-status` 行（R 记录取新路径，独立供 fixture 测）。
func parseNameStatus(out string) []RevisionFile {
	var files []RevisionFile
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		path := fields[len(fields)-1] // R/C: old \t new → 取新路径
		st := string(status[0])
		if Whitelisted(path) {
			files = append(files, RevisionFile{Path: filepath.ToSlash(path), Status: st})
		}
	}
	return files
}

// file 取版本内字节（Output 而非 Combined：stdout 之外一个字节都不能混）。
func (g *gitEngine) file(ctx context.Context, id, relPath string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cmd := g.command(ctx, "show", id+":"+relPath)
	var so bytes.Buffer
	cmd.Stdout = &so
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("该版本中不存在 %s（git show: %v）", relPath, err)
	}
	return so.Bytes(), nil
}

// heal 连续失败自愈：现仓库整体改名保留（尽力不丢历史），重 init 空仓库再战
// （PLAN §6.2：不再静默烧 tick）。
func (g *gitEngine) heal(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, err := os.Stat(g.gitDir); err == nil {
		quarantined := g.gitDir + ".broken-" + time.Now().Format(backupTimeLayout)
		if rerr := os.Rename(g.gitDir, quarantined); rerr != nil {
			slog.Error("snapshot: 自愈改名失败，放弃本轮重建", "err", rerr)
			return
		}
		slog.Warn("snapshot: 损坏仓库已隔离保留，重建新仓库", "kept_at", quarantined)
	}
	if err := g.ensureRepo(ctx); err != nil {
		slog.Error("snapshot: 仓库重建失败", "err", err)
	}
}

// summarizeFiles 人话摘要：变更文件名列表（恢复 UI 直接展示，PLAN §3.3）。
func summarizeFiles(files []string) string {
	const maxListed = 5
	parts := files
	suffix := ""
	if len(files) > maxListed {
		parts = files[:maxListed]
		suffix = fmt.Sprintf(" 等 %d 个文件", len(files))
	}
	return strings.Join(baseNames(parts), ", ") + suffix
}

// baseNames 摘要展示只留文件名（相对路径太长会顶满列表；恢复时才回传全路径）。
func baseNames(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if i := strings.LastIndex(p, "/"); i >= 0 {
			out = append(out, p[i+1:])
		} else {
			out = append(out, p)
		}
	}
	return out
}

// parsePorcelain 解析 `status --porcelain=v1 -z` 原始输出。
// -z 记录布局："XY <path>\0"，R/C 多一段 "<orig>\0"。独立函数供 fixture 单测。
func parsePorcelain(out string) []porcelainEntry {
	var entries []porcelainEntry
	tokens := strings.Split(out, "\x00")
	for i := 0; i < len(tokens); i++ {
		rec := tokens[i]
		if len(rec) < 4 { // 至少 "XY p"
			continue
		}
		e := porcelainEntry{Status: rec[:2], Path: rec[3:]}
		if (e.Status[0] == 'R' || e.Status[0] == 'C') && i+1 < len(tokens) {
			e.Orig = tokens[i+1]
			i++
		}
		entries = append(entries, e)
	}
	return entries
}
