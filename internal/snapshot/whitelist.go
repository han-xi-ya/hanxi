package snapshot

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 白名单是快照安全边界的唯一闸门（PLAN_SNAPSHOT §2.3）：正向列举用户数据，
// 明文雷区（runtime/ 运行时 TOML、logs/、versions/ 托管安装树、installers/）
// 不在列举之内即天然排除；列举之内再排除原子写/取证中间产物。
// 本文件全部为纯函数（除 WhitelistRoots/ScanMtime 的 Stat），可单测穷举。

// 数据根下的白名单顶层条目（斜杠分隔相对路径前缀）。
// state/ == 模块状态目录边界（v0.3.x 数据根治理后，见 settings/state_migrate.go），
// memo/ 为第二步文件库化后的便签文件库（不存在时 ScanMtime/Roots 自然跳过）。
const (
	rootConfig  = "config.json"
	rootState   = "state"
	rootMemo    = "memo"
	statePrefix = rootState + "/"
	memoPrefix  = rootMemo + "/"
)

// tmpDebrisRe 原子写残骸 `<name>.json.tmp.<pid>`（jsonstore 与 settings 两套同构格式）。
var tmpDebrisRe = regexp.MustCompile(`\.json\.tmp\.\d+$`)

// corruptDebrisRe 损坏隔离取证副本 `config.json.corrupt-<时间戳>`（app.go 装配根改名产物）。
var corruptDebrisRe = regexp.MustCompile(`\.corrupt-\d{8}-\d{6}$`)

// migratedMemoName memo 文件库化迁移的旧 JSON 留底（改名即提交点，保留期内不入库，
// Q6 拍板两个版本周期后删旧链）。
const migratedMemoName = "memo.json.migrated"

// Whitelisted 判定数据根相对路径（斜杠分隔）是否纳入快照。
// 判定顺序：先顶层前缀白名单，再中间产物黑名单。
func Whitelisted(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if rel == "" || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return false
	}
	if !inWhitelistScope(rel) {
		return false
	}
	base := rel[strings.LastIndex(rel, "/")+1:]
	if base == "" {
		return false // 目录条目本身（"state/"）不作为文件
	}
	if tmpDebrisRe.MatchString(base) || corruptDebrisRe.MatchString(base) {
		return false
	}
	if base == migratedMemoName {
		return false
	}
	return true
}

// inWhitelistScope 顶层前缀判定：config.json 精确 + state/** + memo/**。
func inWhitelistScope(rel string) bool {
	return rel == rootConfig ||
		strings.HasPrefix(rel, statePrefix) ||
		strings.HasPrefix(rel, memoPrefix)
}

// WhitelistRoots 返回数据根下当前存在的白名单顶层条目（git pathspec / 拷贝根用）。
// 只列存在的条目：pathspec 指向不存在路径会让 git add 直接报错。
func WhitelistRoots(dataDir string) []string {
	roots := make([]string, 0, 3)
	if _, err := os.Stat(filepath.Join(dataDir, rootConfig)); err == nil {
		roots = append(roots, rootConfig)
	}
	if fi, err := os.Stat(filepath.Join(dataDir, rootState)); err == nil && fi.IsDir() {
		roots = append(roots, rootState)
	}
	if fi, err := os.Stat(filepath.Join(dataDir, rootMemo)); err == nil && fi.IsDir() {
		roots = append(roots, rootMemo)
	}
	return roots
}

// ScanMtime 遍历白名单文件取最近修改时间（巡检脏判定，PLAN §2.2 推论：
// 所有落盘都表现为白名单文件 rename 式 mtime 跳变，os.Stat 即够，免埋点）。
// 返回零 time 与 found=false 表示白名单内暂无文件；单个条目 Stat 失败静默跳过
// （文件正被原子写 rename，下一拍再看）。
func ScanMtime(dataDir string) (max time.Time, found bool) {
	consider := func(path string) {
		fi, err := os.Stat(path)
		if err != nil || fi.IsDir() {
			return
		}
		if m := fi.ModTime(); m.After(max) {
			max, found = m, true
		}
	}
	consider(filepath.Join(dataDir, rootConfig))
	for _, dir := range []string{rootState, rootMemo} {
		base := filepath.Join(dataDir, dir)
		_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil // 目录不存在/条目竞态消失：跳过（原子写 rename 下一拍再看）
			}
			rel, rerr := filepath.Rel(dataDir, p)
			if rerr == nil && Whitelisted(rel) {
				consider(p)
			}
			return nil
		})
	}
	return max, found
}
