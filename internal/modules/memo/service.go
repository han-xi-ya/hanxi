package memo

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
	"hanxi/internal/notify"
	"hanxi/internal/settings"
)

// MemoService 便签业务服务。
// 存储后端二态：默认文件库（memo/<id>.md，写盘点单条化）；仅当旧库 memo.json
// 仍在位且迁移未成功（损坏/暂存失败）时回落旧整库 Store，保证"迁移不成也不丢数据"。
type MemoService struct {
	files      *FileStore
	store      *Store // 旧库回落态才非 nil；文件库模式恒 nil
	useFiles   bool
	mu         sync.RWMutex
	items      []MemoItem
	wailsApp   *application.App
	onChanged  func() // 测试/内部观察钩子；仅在提交成功后调用
	holder     *extapi.LeaseHolder
	dataDir    string // 数据根（ClearAll 残片收口用：暂存目录挂在这下面）
	legacyPath string // 旧库 <StateDir>/memo.json（其 .migrated/.corrupt- 派生残片同源收口）

	// 悬浮速记卡（N16 B 批）：偏好、热键注册器引用与卡窗状态。记账全部走
	// sheetMu，与便签数据锁 mu 互不相涉；Wails 窗口 API 一律锁外调用防锁反转。
	prefs        *quickMemoStore  // <stateDir>/memo-prefs.json（nil 容忍，回落默认值）
	hk           *hotkey.Registry // 全仓通用热键注册器（装配根注入，未注入退化为纯配置读写）
	sheetMu      sync.Mutex       // 串行化速记卡记账（窗口引用/显隐/空闲计时/started）
	sheetWin     *application.WebviewWindow
	sheetClosing func() // WindowClosing 拦截 hook 的注销闭包（真销毁前必摘，#53）
	sheetShown   bool
	sheetIdle    *time.Timer // 收起后的空闲销毁计时
	sheetStarted bool        // OnInit~OnDestroy 窗口期：false 即拒绝一切唤出
}

// NewMemoService 实例化便签服务：启动清扫/迁移旧库，然后把权威数据全量装载进内存
// （实测千条以下全量缓存模式，前端 List/GetStats 语义迁移前后不变）。
// 全部业务 RPC 方法经 holder 接入统一调用门（Wave 3）：停用/未安装模块的任何
// 方法调用被拒并返回明确错误，调用在途期间停用会等待 drain。
func NewMemoService(paths *settings.Paths, holder *extapi.LeaseHolder) (*MemoService, error) {
	dataDir := paths.DataDir()
	legacyPath := filepath.Join(paths.StateDir(), "memo.json")
	memoDir := filepath.Join(dataDir, "memo")

	// 上次进程遗留的暂存残骸/半程续跑（幂等）
	sweepStaleStaging(dataDir, memoDir, legacyPath)
	if need, err := memoNeedsMigration(legacyPath, memoDir); err != nil {
		slog.Error("memo: 迁移判据读取失败，本次跳过迁移", "err", err)
	} else if need {
		committed, merr := migrateMemoToFiles(legacyPath, memoDir)
		if merr != nil {
			slog.Error("memo: 文件库化迁移未完成", "committed", committed, "err", merr)
		}
	}

	files := NewFileStore(memoDir)
	s := &MemoService{files: files, holder: holder, dataDir: dataDir, legacyPath: legacyPath,
		prefs: newQuickMemoStore(paths.StateDir())}

	hasFiles, herr := memoDirHasFiles(memoDir)
	if herr != nil {
		// 目录不可读按"无已提交文件"保守处理：旧库在位则整体回落旧读写，不丢数据
		slog.Warn("memo: 文件库目录读取失败，保守按空库处理", "err", herr)
	}
	_, legacyErr := os.Stat(legacyPath)
	legacyExists := legacyErr == nil
	switch {
	case hasFiles || !legacyExists:
		// 文件库权威（正常态；含全新安装——不再复活空 memo.json）
		s.useFiles = true
		items, lerr := files.LoadAll()
		if lerr != nil {
			// 严格型读 + 不阻断启动：坏条目已在 LoadAll 内隔离取证副本，此处显式告警
			notify.Error(ID, "部分便签装载失败", lerr.Error(), "/ext/memo")
		}
		s.items = items
	case !hasFiles:
		// 迁移未成的回落态：旧整库读写照旧（BUG-032 修复点——Load 错误不再吞）
		if herr != nil {
			return nil, fmt.Errorf("便签文件库与旧库均不可读: %w", herr)
		}
		store, err := NewStore(legacyPath)
		if err != nil {
			return nil, err
		}
		items, err := store.Load()
		if err != nil {
			quarantine := legacyPath + corruptInfix + time.Now().Format("20060102-150405")
			if rerr := os.Rename(legacyPath, quarantine); rerr != nil {
				return nil, fmt.Errorf("读取便签库失败且隔离改名失败（拒绝以空库覆盖可疑数据）: %v / %w", rerr, err)
			}
			slog.Error("memo: 旧库损坏，已隔离取证副本并以空库启动（可从副本手工找回）",
				"err", err, "quarantined_to", quarantine)
			notify.Error(ID, "便签数据损坏已隔离", "memo.json 解析失败，已改名保留取证副本，本次以空库启动", "/ext/memo")
			items = []MemoItem{}
		}
		s.store, s.items = store, items
	}
	return s, nil
}

// SetWailsApp 设置 Wails App 引用。
// 装配布线: Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)——不接调用门。
func (s *MemoService) SetWailsApp(app *application.App) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wailsApp = app
}

// List 根据过滤条件检索便签
func (s *MemoService) List(filter MemoFilter) ([]MemoItem, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]MemoItem, 0, len(s.items))
	kw := strings.ToLower(strings.TrimSpace(filter.Keyword))
	filterTag := strings.TrimSpace(filter.Tag)

	for _, item := range s.items {
		// 1. 过滤置顶
		if filter.Pinned != nil && item.IsPinned != *filter.Pinned {
			continue
		}

		// 2. 过滤标签
		if filterTag != "" {
			matchedTag := false
			for _, t := range item.Tags {
				if strings.EqualFold(t, filterTag) || strings.EqualFold(strings.TrimPrefix(t, "#"), strings.TrimPrefix(filterTag, "#")) {
					matchedTag = true
					break
				}
			}
			if !matchedTag {
				continue
			}
		}

		// 3. 关键字模糊搜索 (标题、内容、标签)
		if kw != "" {
			titleMatch := strings.Contains(strings.ToLower(item.Title), kw)
			contentMatch := strings.Contains(strings.ToLower(item.Content), kw)
			tagMatch := false
			for _, t := range item.Tags {
				if strings.Contains(strings.ToLower(t), kw) {
					tagMatch = true
					break
				}
			}
			if !titleMatch && !contentMatch && !tagMatch {
				continue
			}
		}

		result = append(result, item)
	}

	// 排序逻辑：置顶始终排在最前面；其次按 UpdatedAt 或 CreatedAt 排序
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].IsPinned != result[j].IsPinned {
			return result[i].IsPinned // true 在前
		}
		if filter.SortBy == "created" {
			if filter.SortDesc {
				return result[i].CreatedAt.After(result[j].CreatedAt)
			}
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		// 默认按 updatedAt 降序
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})

	return result, nil
}

// GetStats 获取便签统计数据与标签云
func (s *MemoService) GetStats() (MemoStats, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return MemoStats{}, gateErr
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := MemoStats{
		TotalCount:  len(s.items),
		PinnedCount: 0,
		TagCloud:    make(map[string]int),
	}

	for _, item := range s.items {
		if item.IsPinned {
			stats.PinnedCount++
		}
		for _, tag := range item.Tags {
			cleaned := strings.TrimSpace(tag)
			if cleaned != "" {
				if !strings.HasPrefix(cleaned, "#") {
					cleaned = "#" + cleaned
				}
				stats.TagCloud[cleaned]++
			}
		}
	}

	return stats, nil
}

// Create 创建新便签
func (s *MemoService) Create(title, content string, tags []string, colorTag string) (MemoItem, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return MemoItem{}, gateErr
	}
	defer release()
	return s.createItem(title, content, tags, colorTag)
}

// createItem 创建落盘与内存换装的内部核心（不带调用门）：
// 供导出版 Create 与 QuickCreate 复用，QuickCreate 不再经带门 Create 二次入账。
func (s *MemoService) createItem(title, content string, tags []string, colorTag string) (MemoItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cleanedTags := cleanTags(tags)
	if colorTag == "" {
		colorTag = "blue"
	}

	item := MemoItem{
		ID:        fmt.Sprintf("memo_%d", now.UnixNano()),
		Title:     strings.TrimSpace(title),
		Content:   content,
		Tags:      cleanedTags,
		IsPinned:  false,
		IsMasked:  false,
		ColorTag:  colorTag,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// 文件库模式单条落盘、失败即返回（内存不换装，与盘不分叉）；
	// 回落态维持旧语义（先改候选整表再原子写）
	if s.useFiles {
		if err := s.files.SaveItem(item); err != nil {
			return MemoItem{}, err
		}
		s.items = append([]MemoItem{item}, s.items...)
	} else {
		next := append([]MemoItem{item}, s.items...)
		if err := s.store.Save(next); err != nil {
			return MemoItem{}, err
		}
		s.items = next
	}

	s.emitChanged()
	return item, nil
}

// QuickCreate 快捷创建 (主要供 fileshare 跨模块投递联动使用)。
// 导出版接门：memo 停用时返回明确的门拒绝错误（当前签名即返回 error，
// 直调方 fileshare 如实上浮/记录，不静默吞）。
func (s *MemoService) QuickCreate(title, content string, tags []string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	_, err := s.createItem(title, content, tags, "amber")
	return err
}

// Update 更新已有便签
func (s *MemoService) Update(id, title, content string, tags []string, colorTag string) (MemoItem, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return MemoItem{}, gateErr
	}
	defer release()

	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, it := range s.items {
		if it.ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return MemoItem{}, fmt.Errorf("便签不存在: %s", id)
	}

	candidate := s.items[idx]
	candidate.Title = strings.TrimSpace(title)
	candidate.Content = content
	candidate.Tags = cleanTags(tags)
	if colorTag != "" {
		candidate.ColorTag = colorTag
	}
	candidate.UpdatedAt = time.Now()

	if err := s.persistCandidate(candidate); err != nil {
		return MemoItem{}, err
	}
	s.items[idx] = candidate

	s.emitChanged()
	return candidate, nil
}

// persistCandidate 只把候选态落盘，不触碰现有内存；调用者须在成功后换装。
func (s *MemoService) persistCandidate(item MemoItem) error {
	if s.useFiles {
		return s.files.SaveItem(item)
	}
	next := append([]MemoItem(nil), s.items...)
	for i, it := range next {
		if it.ID == item.ID {
			next[i] = item
			break
		}
	}
	return s.store.Save(next)
}

// TogglePin 切换置顶状态。候选值先落盘，失败则内存与事件均不变。
func (s *MemoService) TogglePin(id string) (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, it := range s.items {
		if it.ID == id {
			candidate := it
			candidate.IsPinned = !candidate.IsPinned
			candidate.UpdatedAt = time.Now()
			if err := s.persistCandidate(candidate); err != nil {
				return it.IsPinned, err
			}
			s.items[i] = candidate
			s.emitChanged()
			return candidate.IsPinned, nil
		}
	}
	return false, fmt.Errorf("便签不存在: %s", id)
}

// ToggleMask 切换敏感信息遮罩。隐私态必须与持久化提交绑定：写盘失败时
// 保持原遮罩状态且不广播，避免 UI 误以为敏感信息已被安全遮住。
func (s *MemoService) ToggleMask(id string) (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, it := range s.items {
		if it.ID == id {
			candidate := it
			candidate.IsMasked = !candidate.IsMasked
			candidate.UpdatedAt = time.Now()
			if err := s.persistCandidate(candidate); err != nil {
				return it.IsMasked, err
			}
			s.items[i] = candidate
			s.emitChanged()
			return candidate.IsMasked, nil
		}
	}
	return false, fmt.Errorf("便签不存在: %s", id)
}

// Delete 删除指定便签
func (s *MemoService) Delete(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]MemoItem, 0, len(s.items))
	for _, it := range s.items {
		if it.ID != id {
			filtered = append(filtered, it)
		}
	}

	if s.useFiles {
		// 先删文件再换内存：删文件失败即返回错误，内存态与磁盘不分叉
		if err := s.files.RemoveItem(id); err != nil {
			return err
		}
	} else if err := s.store.Save(filtered); err != nil {
		return err
	}
	s.items = filtered

	s.emitChanged()
	return nil
}

// ClearAll 一键全删：清空全部便签（含敏感遮罩条目与隔离取证副本），返回本次删除的
// 便签条数。"不留残片"纪律：文件库模式清空 memo/ 目录下全部文件（正式 .md 与
// .bad-/.tmp. 孤儿一并销毁）；回落模式把旧库写回空表（旧库是读写权威，销毁它反而
// 会在下次启动复活空库文件，故原位清空）。随后清扫与便签同源的数据派生物：
// 旧库 memo.json 本体（文件库模式下只可能是回滚/异常残留）、其 .migrated 备份与
// .corrupt- 取证副本、半程暂存目录——迁移留底里带着全量旧便签正文，不清即漏。
// 部分失败不假装成功：确认删除的条目先从内存移除（内存与盘不分叉），错误如实
// 聚合上浮，前端重拉即见真实剩余；memo:changed 照常广播，多端联动同步收口。
func (s *MemoService) ClearAll() (int, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return 0, gateErr
	}
	defer release()

	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	deleted := make(map[string]bool, len(s.items))

	if s.useFiles {
		entries, rerr := os.ReadDir(s.files.dir)
		if rerr != nil && !os.IsNotExist(rerr) {
			return 0, fmt.Errorf("便签文件库目录不可读，全删中止: %w", rerr)
		}
		for _, e := range entries {
			name := e.Name()
			// RemoveAll：memo/ 目录本模块独占（快照白名单口径），异常子目录一并销毁才算收口
			if remErr := os.RemoveAll(filepath.Join(s.files.dir, name)); remErr != nil {
				if !os.IsNotExist(remErr) {
					errs = append(errs, fmt.Errorf("%s: %w", name, remErr))
					continue
				}
			}
			// 孤儿残骸（坏条隔离副本/崩溃半程 tmp）可能不对应任何在装条目，只映射回本尊 ID
			if id, ok := memoIDFromFile(name); ok {
				deleted[id] = true
			}
		}
		// 幽灵条目（内存有、盘上早已被外部删净）同样以盘为准清掉，全删后不留差集
		for _, it := range s.items {
			if deleted[it.ID] {
				continue
			}
			if _, serr := os.Stat(filepath.Join(s.files.dir, it.ID+".md")); os.IsNotExist(serr) {
				deleted[it.ID] = true
			}
		}
	} else {
		if err := s.store.Save([]MemoItem{}); err != nil {
			return 0, err
		}
		for _, it := range s.items {
			deleted[it.ID] = true
		}
	}

	// 回落模式下旧库刚写回空表、仍是权威，本体保留；文件库模式下连本体一起清
	errs = append(errs, sweepMemoArtifacts(s.dataDir, s.legacyPath, !s.useFiles)...)

	kept := make([]MemoItem, 0, len(s.items))
	for _, it := range s.items {
		if !deleted[it.ID] {
			kept = append(kept, it)
		}
	}
	removed := len(s.items) - len(kept)
	if len(s.items) > 0 {
		s.items = kept
		s.emitChanged()
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("全删收口不完全（%d 处失败，可重试）: %s", len(errs), joinErrs(errs))
	}
	return removed, nil
}

// memoIDFromFile 把 memo/ 目录内文件名映射回便签 ID：
// `<id>.md`（正式条）、`<id>.md.bad-*`（坏条隔离副本）、
// `<id>.md.tmp.<pid>`（原子写崩溃半程）都归本尊 ID；其余（理论不该存在）不归。
func memoIDFromFile(name string) (string, bool) {
	switch {
	case strings.HasSuffix(name, ".md"):
		return strings.TrimSuffix(name, ".md"), true
	case strings.Contains(name, ".md.bad-"):
		return name[:strings.Index(name, ".md.bad-")], true
	case strings.Contains(name, ".md.tmp."):
		return strings.TrimSuffix(name[:strings.Index(name, ".md.tmp.")], ".md"), true
	}
	return "", false
}

// sweepMemoArtifacts 销毁与便签同源的数据派生残片：旧库 memo.json 本体（keepLegacy=
// false 时）、其 .migrated 备份（含带时间戳变体）、.corrupt- 取证副本、数据根下的
// 半程暂存目录 <stagingPrefix>*/。返回逐个失败的错误清单（不 panic 不中断清扫）。
// 空路径（测试手工组装的服务未接 settings）各分支自然跳过。
func sweepMemoArtifacts(dataDir, legacyPath string, keepLegacy bool) []error {
	var errs []error
	if legacyPath != "" {
		if !keepLegacy {
			if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
		for _, pattern := range []string{legacyPath + migratedSuffix + "*", legacyPath + corruptInfix + "*"} {
			matches, gerr := filepath.Glob(pattern)
			if gerr != nil {
				errs = append(errs, gerr)
				continue
			}
			for _, m := range matches {
				if err := os.RemoveAll(m); err != nil && !os.IsNotExist(err) {
					errs = append(errs, err)
				}
			}
		}
	}
	if dataDir != "" {
		entries, err := os.ReadDir(dataDir)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			return errs
		}
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), stagingPrefix) {
				if rerr := os.RemoveAll(filepath.Join(dataDir, e.Name())); rerr != nil {
					errs = append(errs, rerr)
				}
			}
		}
	}
	return errs
}

// joinErrs 错误清单拼一行（joinErrors 的"装载异常"话术不适用于全删语境）。
func joinErrs(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}

// RestoreFile 单条热恢复（供 internal/snapshot「历史版本」恢复 memo/<id>.md 时经
// 装配根注入的钩子调用）：校验文件内容与 ID 一致后原样字节回写，内存换装并广播
// memo:changed——前端全量重拉即见，热生效无重启。回落旧库模式不支持（无单条概念），
// 返回可读错误引导重启。content 用 string 承载原样字节（Go string 不校验 UTF-8，
// 无损；同时避开绑定面对 []byte 的形态特化）。
// memo 停用时热恢复被门拒绝并上浮明确错误（快照页单文件回滚可见），属预期语义。
func (s *MemoService) RestoreFile(id, content string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	data := []byte(content)
	item, derr := DecodeMemo(id+".md", data)
	if derr != nil {
		return fmt.Errorf("恢复内容不是合法便签文件: %w", derr)
	}
	if item.ID != id {
		return fmt.Errorf("恢复内容 ID（%s）与目标（%s）不一致", item.ID, id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.useFiles {
		return errors.New("便签尚处旧库回落模式，暂不支持单条热恢复，请重启 Hanxi")
	}
	// 原样字节回写（保留版本内历史原文，不重编码归一）
	if err := writeFileAtomic(filepath.Join(s.files.dir, id+".md"), data); err != nil {
		return err
	}
	replaced := false
	for i, it := range s.items {
		if it.ID == id {
			s.items[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		s.items = append([]MemoItem{item}, s.items...)
	}
	s.emitChanged()
	return nil
}

// TitleForWhitelistPath 把快照白名单路径 memo/<id>.md 映射为便签标题（N33 §2
// ListFiles 左栏展示用；经装配根注入 internal/snapshot 的标题 resolver 钩子，
// 规避 snapshot→memo 包引用）。走标准调用门（RPC 门覆盖矩阵同口径）：memo
// 停用期返 false，快照页如实回落文件名，不借标题旁路复活停用模块的数据面。
// 非 memo 形态、未命中或空标题同样返 false。
func (s *MemoService) TitleForWhitelistPath(rel string) (string, bool) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", false
	}
	defer release()

	if !strings.HasPrefix(rel, "memo/") || !strings.HasSuffix(rel, ".md") {
		return "", false
	}
	id := strings.TrimSuffix(rel[strings.LastIndex(rel, "/")+1:], ".md")
	if id == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.ID == id {
			return it.Title, strings.TrimSpace(it.Title) != ""
		}
	}
	return "", false
}

func (s *MemoService) emitChanged() {
	if s.onChanged != nil {
		s.onChanged()
	}
	// memo:changed 以 Void 注册（无载荷事件），Emit 不带任何数据参数
	if s.wailsApp != nil && s.wailsApp.Event != nil {
		s.wailsApp.Event.Emit("memo:changed")
	} else if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("memo:changed")
	}
}

func cleanTags(tags []string) []string {
	seen := make(map[string]bool)
	res := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			t = "#" + t
		}
		if !seen[t] {
			seen[t] = true
			res = append(res, t)
		}
	}
	return res
}
