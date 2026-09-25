package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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

// revisions git log 精简形态（统一读面 fileChanges 的映射投影：id/时间/摘要）。
func (g *gitEngine) revisions(ctx context.Context, limit int) ([]Revision, error) {
	recs, err := g.fileChanges(ctx, limit)
	if err != nil {
		return nil, err
	}
	revs := make([]Revision, 0, len(recs))
	for _, rec := range recs {
		revs = append(revs, Revision{ID: rec.ID, Time: rec.Time, Summary: rec.Summary})
	}
	return revs, nil
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

// fileChanges：`git log -z --name-status` 全版本变化事件流（ListFiles/FileHistory/
// DiffFile 共同读面）。-z 形态实探：STATUS 与每个 PATH 各成独立 NUL token
// （非 "M\tpath" 合并形态），R/C 记录后跟两个路径 token（旧名在前新名在后）。
func (g *gitEngine) fileChanges(ctx context.Context, window int) ([]versionChanges, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.fileChangesLocked(ctx, window)
}

func (g *gitEngine) fileChangesLocked(ctx context.Context, window int) ([]versionChanges, error) {
	if window <= 0 || window > maxListRevisions {
		window = maxListRevisions
	}
	out, se, err := g.run(ctx,
		"log", "-z", "--name-status", fmt.Sprintf("--max-count=%d", window),
		"--format=%H%x1f%ct%x1f%s")
	if err != nil {
		// 空仓库（首拍前）不是错误
		if strings.Contains(se, "does not have any commits yet") {
			return nil, nil
		}
		return nil, fmt.Errorf("git log 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	return parseFileLog(out), nil
}

// fileStatusRe -z 流中的变化状态 token（可带 git 残留的前置换行）：字母 + 可选分数。
var fileStatusRe = regexp.MustCompile(`^\n?[A-Z]{1,2}[0-9]*$`)

// parseFileLog 拆 `git log -z --name-status` 的 NUL token 流（独立供 fixture 单测）。
// token 分类：含 \x1f → 版本头；形如 M/A/D/R100 的状态词 → 其后跟 1 个
// （R/C 为 2 个）路径 token；其余忽略。白名单外路径剔除（改名只留一侧时
// 相应折算：新名出保 → 旧名记 D）。
func parseFileLog(out string) []versionChanges {
	var recs []versionChanges
	var pending string // 当前状态 token
	var pendingOrig string
	want := 0
	addChange := func(c fileChange) {
		if len(recs) == 0 {
			return
		}
		last := &recs[len(recs)-1]
		last.Changes = append(last.Changes, c)
	}
	for _, tok := range strings.Split(out, "\x00") {
		if i := strings.IndexByte(tok, '\x1f'); i >= 0 {
			fields := strings.SplitN(strings.TrimPrefix(tok, "\n"), "\x1f", 3)
			pending, pendingOrig, want = "", "", 0
			if len(fields) < 2 {
				continue
			}
			unixSec, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
			if err != nil {
				continue
			}
			subject := ""
			if len(fields) == 3 {
				subject = strings.TrimSuffix(fields[2], "\n")
			}
			recs = append(recs, versionChanges{
				ID:      fields[0],
				Time:    time.Unix(unixSec, 0).Format(time.RFC3339),
				Summary: strings.TrimPrefix(subject, "checkpoint: "),
			})
			continue
		}
		t := strings.Trim(tok, "\n\r")
		if t == "" {
			pending, pendingOrig, want = "", "", 0
			continue
		}
		if want > 0 {
			p := filepath.ToSlash(t)
			want--
			if want > 0 { // R/C 第一段 = 旧名
				pendingOrig = p
				continue
			}
			st := ""
			if pending != "" {
				st = string(pending[0])
			}
			origOK := pendingOrig != "" && Whitelisted(pendingOrig)
			pathOK := Whitelisted(p)
			switch {
			case origOK && pathOK:
				addChange(fileChange{Status: st, Path: p, Orig: pendingOrig})
			case origOK: // 改名出保：对旧名等同删除
				addChange(fileChange{Status: "D", Path: pendingOrig})
			case pathOK: // 自保外改名而来：只报新名
				addChange(fileChange{Status: st, Path: p})
			}
			pending, pendingOrig = "", ""
			continue
		}
		if fileStatusRe.MatchString(tok) {
			pending = t
			pendingOrig = ""
			want = 1
			if t[0] == 'R' || t[0] == 'C' {
				want = 2
			}
		}
	}
	return recs
}

// fileHistory 单文件时间线：`git log --follow -z --name-status -- <path>` 读面，
// 改名链沿途回旧名。git 对"旧名在改名 commit"本身就报 D（实探口径），与新名
// 侧的 R 事件合成 N33 §0.3-A 恢复链的两半。
func (g *gitEngine) fileHistory(ctx context.Context, rel string, window int) ([]FileRevision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	evts, err := g.followEventsLocked(ctx, rel, window)
	if err != nil {
		return nil, err
	}
	revs := make([]FileRevision, 0, len(evts))
	for _, e := range evts {
		revs = append(revs, e.FileRevision)
	}
	return revs, nil
}

// followEvent 改名链上一个时间线事件：AtPath 为该版本中文件实际使用的链上名，
// PrePath 为其内容在父版本中挂名的路径（A 事件无意义，取同 AtPath）。
type followEvent struct {
	FileRevision
	AtPath  string
	PrePath string
}

// followEventsLocked git log --follow 读面 + 链名折算（调用方持 g.mu）。
func (g *gitEngine) followEventsLocked(ctx context.Context, rel string, window int) ([]followEvent, error) {
	if window <= 0 || window > maxListRevisions {
		window = maxListRevisions
	}
	out, se, err := g.run(ctx, "log", "--follow", "-z", "--name-status",
		fmt.Sprintf("--max-count=%d", window), "--format=%H%x1f%ct%x1f%s", "--", rel)
	if err != nil {
		// 空仓库（首拍前）不是错误
		if strings.Contains(se, "does not have any commits yet") {
			return nil, nil
		}
		return nil, fmt.Errorf("git log 失败: %v (%s)", err, strings.TrimSpace(se))
	}
	return followChainEvents(rel, parseFileLog(out)), nil
}

// followChainEvents 把 follow 流记录折算为查询名的时间线（纯函数，独立供单测）：
// 链名自查询名起，遇"改名到达"（R 记录的新侧）事件记 R、链名回拨旧名；
// 其余记录路径即链上名。git 已把"改名离开"对旧名报为 D，无需再折算。
func followChainEvents(rel string, recs []versionChanges) []followEvent {
	chain := rel
	var out []followEvent
	for _, rec := range recs {
		ch, ok := matchFileChange(rec.Changes, chain)
		if !ok {
			continue
		}
		st := ch.Status
		if st == "" {
			st = "M"
		}
		at := ch.Path
		pre := ch.Path
		if ch.Orig != "" {
			if ch.Orig == chain && ch.Path != chain {
				// 该名字改名离开：对它是 D（git 多数已直接报 D，此处兜底折算）
				st = "D"
				at = chain
				pre = chain
			} else {
				// 改名到达本名：事件 R；父版本内容挂在旧名下
				st = "R"
				pre = ch.Orig
				chain = ch.Orig // 更老的记录以旧名续查
			}
		}
		out = append(out, followEvent{
			FileRevision: FileRevision{RevisionID: rec.ID, Time: rec.Time, Status: st, Summary: rec.Summary},
			AtPath:       at,
			PrePath:      pre,
		})
	}
	return out
}

// matchFileChange 在该版变化事件里找与链名对应的记录（新旧名两侧都认）。
func matchFileChange(changes []fileChange, rel string) (fileChange, bool) {
	for _, c := range changes {
		if c.Path == rel || c.Orig == rel {
			return c, true
		}
	}
	return fileChange{}, false
}

// fileDiff 新旧双读：New=show id:<链上名>，Old=show id^:<旧链名>；A 无旧、D 无新、
// 首版无父/父中无该形态 → Old 空（N33 §3）。id 允许短 hash，命中的版本补全为
// 完整 hash 再取 blob；不在观察窗内如实报错，不伪造对比。
func (g *gitEngine) fileDiff(ctx context.Context, id, relPath string) (fileDiffRaw, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !Whitelisted(relPath) || strings.Contains(relPath, "..") {
		return fileDiffRaw{}, fmt.Errorf("非法的对比路径: %s", relPath)
	}
	if !gitHexRe.MatchString(id) {
		return fileDiffRaw{}, fmt.Errorf("git 模式版本标识应为 commit hash: %s", id)
	}
	evts, err := g.followEventsLocked(ctx, relPath, maxListRevisions)
	if err != nil {
		return fileDiffRaw{}, err
	}
	for _, e := range evts {
		if e.RevisionID != id && !strings.HasPrefix(e.RevisionID, id) {
			continue
		}
		full := e.RevisionID
		raw := fileDiffRaw{Status: e.Status, Summary: e.Summary}
		if raw.Status == "D" {
			// 删除事件：新版所无，旧版仍在
			raw.Old = g.parentBlob(ctx, full, e.PrePath)
			return raw, nil
		}
		data, berr := g.readBlob(ctx, full+":"+e.AtPath)
		if berr != nil {
			return fileDiffRaw{}, fmt.Errorf("该版本中不存在 %s（git show: %v）", relPath, berr)
		}
		raw.New = string(data)
		if raw.Status != "A" {
			raw.Old = g.parentBlob(ctx, full, e.PrePath)
		}
		return raw, nil
	}
	return fileDiffRaw{}, fmt.Errorf("版本 %s 中 %s 无变化记录（或已超出最近 %d 版观察窗）", id, relPath, maxListRevisions)
}

// parentBlob 父版本 blob 宽容读：无父（首版）、父中无该路径或改名链错位 → 空。
func (g *gitEngine) parentBlob(ctx context.Context, fullID, path string) string {
	if !g.hasParent(ctx, fullID) {
		return ""
	}
	data, err := g.readBlob(ctx, fullID+"^:"+path)
	if err != nil {
		return ""
	}
	return string(data)
}

// hasParent rev-parse --verify --quiet 判父存在（快照历史线性，父即上一版）。
func (g *gitEngine) hasParent(ctx context.Context, id string) bool {
	out, _, err := g.run(ctx, "rev-parse", "--verify", "--quiet", id+"^")
	return err == nil && strings.TrimSpace(out) != ""
}

// readBlob show <rev-spec> 的 stdout 字节（Output 而非 Combined：一个字节都不能混）。
// 调用方必须已持 g.mu（gitEngine 仓库级串行闸）。
func (g *gitEngine) readBlob(ctx context.Context, revSpec string) ([]byte, error) {
	cmd := g.command(ctx, "show", revSpec)
	var so bytes.Buffer
	cmd.Stdout = &so
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return so.Bytes(), nil
}

// file 取版本内字节（gitEngine 仓库级串行闸）。路径引擎侧自检白名单，
// 与服务层闸门构成双重防线（双闸纪律，N33 §3）。
func (g *gitEngine) file(ctx context.Context, id, relPath string) ([]byte, error) {
	if !Whitelisted(relPath) || strings.Contains(relPath, "..") {
		return nil, fmt.Errorf("非法的快照路径: %s", relPath)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	data, err := g.readBlob(ctx, id+":"+relPath)
	if err != nil {
		return nil, fmt.Errorf("该版本中不存在 %s（git show: %v）", relPath, err)
	}
	return data, nil
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
