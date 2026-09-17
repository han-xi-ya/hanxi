package memo

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 每条一文件的便签存储（PLAN_SNAPSHOT §3.4）：`memo/<id>.md`，最小手写 frontmatter
// ——`---` 包裹 `key: value` 行（不引 YAML 库），正文原样。目的有二：
//  1. 快照 diff 精确到"改了哪一条"（整库 JSON 时代一次编辑全文件重写）；
//  2. 写盘点从"整体重写 5 处"降为"单条写"，删除/崩溃不再殃及全库。
//
// 读策略延续仓内严格型：单条解析失败只废该条（报错日志），不静默清空创作内容。
// tags 行为 JSON 数组字符串（`tags: ["#a","#b"]`）——PLAN 原案逗号分隔无法无损
// 承载含逗号的标签，单行 JSON 数组同样零依赖且可手读。

// memoFileName `<id>.md`；id 仅安全字符集（新建 id 为 memo_<unixnano>，迁移沿用存量）。
var memoFileNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.md$`)

// EncodeMemo 序列化单条便签为 md 字节。
func EncodeMemo(item MemoItem) ([]byte, error) {
	tags, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", item.ID)
	fmt.Fprintf(&b, "title: %s\n", sanitizeLine(item.Title))
	fmt.Fprintf(&b, "tags: %s\n", tags)
	fmt.Fprintf(&b, "pinned: %t\n", item.IsPinned)
	fmt.Fprintf(&b, "masked: %t\n", item.IsMasked)
	fmt.Fprintf(&b, "color: %s\n", sanitizeLine(item.ColorTag))
	fmt.Fprintf(&b, "created: %s\n", item.CreatedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "updated: %s\n", item.UpdatedAt.Format(time.RFC3339Nano))
	b.WriteString("---\n")
	b.WriteString(item.Content) // 正文原样：字节级往返，不做换行归一
	return []byte(b.String()), nil
}

// DecodeMemo 从 md 字节解析单条便签（fileName 仅用于报错定位）。
// 未知 key 忽略（前向兼容：将来加字段旧读者不炸）；关键结构缺失判错。
// 正文按 split/join 还原，不做换行归一：字节级往返（含 Windows 粘贴的 CRLF）。
func DecodeMemo(fileName string, data []byte) (MemoItem, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return MemoItem{}, fmt.Errorf("%s: 缺少 frontmatter 头", fileName)
	}
	rest := text[4:]
	end := -1
	var body string
	// frontmatter 结束行：第一个单独 "---" 行（正文里再有 "---" 也轮不到它，
	// 结束行只可能出现在其前——搜索顺序即语义）
	lines := strings.Split(rest, "\n")
	for i, ln := range lines {
		if strings.TrimRight(ln, " \t") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return MemoItem{}, fmt.Errorf("%s: frontmatter 未闭合", fileName)
	}
	body = strings.Join(lines[end+1:], "\n")

	var item MemoItem
	item.ID = strings.TrimSuffix(fileName, ".md")
	for _, ln := range lines[:end] {
		ln = strings.TrimRight(ln, " \t")
		if ln == "" {
			continue
		}
		key, val, ok := strings.Cut(ln, ":")
		if !ok {
			return MemoItem{}, fmt.Errorf("%s: 畸形 frontmatter 行 %q", fileName, ln)
		}
		val = strings.TrimSpace(val)
		switch key {
		case "id":
			if val != "" {
				item.ID = val
			}
		case "title":
			item.Title = val
		case "tags":
			var tags []string
			if err := json.Unmarshal([]byte(val), &tags); err != nil {
				return MemoItem{}, fmt.Errorf("%s: tags 解析失败: %w", fileName, err)
			}
			item.Tags = tags
		case "pinned":
			item.IsPinned = val == "true"
		case "masked":
			item.IsMasked = val == "true"
		case "color":
			item.ColorTag = val
		case "created":
			t, err := time.Parse(time.RFC3339Nano, val)
			if err != nil {
				return MemoItem{}, fmt.Errorf("%s: created 解析失败: %w", fileName, err)
			}
			item.CreatedAt = t
		case "updated":
			t, err := time.Parse(time.RFC3339Nano, val)
			if err != nil {
				return MemoItem{}, fmt.Errorf("%s: updated 解析失败: %w", fileName, err)
			}
			item.UpdatedAt = t
		default:
			// 未知 key：忽略
		}
	}
	if item.ID == "" {
		return MemoItem{}, fmt.Errorf("%s: 缺少 id", fileName)
	}
	if item.ColorTag == "" {
		item.ColorTag = "blue"
	}
	item.Content = body
	return item, nil
}

// sanitizeLine frontmatter 值压成单行：标题里的换行折为空格。
// 标题本就是单行输入语义（多行内容归正文），此处防手工编辑破坏文件结构。
func sanitizeLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// FileStore 便签文件库引擎：dir = <数据根>/memo/，每条一文件。
type FileStore struct {
	dir string
}

func NewFileStore(dir string) *FileStore { return &FileStore{dir: dir} }

// Dir 文件库目录（快照白名单 memo/ 的实体）。
func (f *FileStore) Dir() string { return f.dir }

// LoadAll 读回全部便签。单条解析失败只丢该条（隔离副本 + 报错日志），
// 其余照常装载——延续"读侧严格区分但绝不连坐全库"的文化。
func (f *FileStore) LoadAll() ([]MemoItem, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []MemoItem{}, nil
		}
		return nil, err
	}
	items := make([]MemoItem, 0, len(entries))
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || !memoFileNameRe.MatchString(e.Name()) {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(f.dir, e.Name()))
		if rerr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), rerr))
			continue
		}
		item, derr := DecodeMemo(e.Name(), data)
		if derr != nil {
			quarantine := filepath.Join(f.dir, e.Name()+".bad-"+time.Now().Format("20060102-150405"))
			if os.Rename(filepath.Join(f.dir, e.Name()), quarantine) == nil {
				slog.Error("memo: 便签文件解析失败，已隔离取证副本（该条未装载）", "file", e.Name(), "quarantined_to", quarantine, "err", derr)
			} else {
				slog.Error("memo: 便签文件解析失败且隔离改名失败（该条未装载）", "file", e.Name(), "err", derr)
			}
			errs = append(errs, derr)
			continue
		}
		items = append(items, item)
	}
	return items, joinErrors(errs)
}

// SaveItem 单条原子落盘（jsonstore 同构 tmp+rename；不校验 JSON——md 原样字节）。
func (f *FileStore) SaveItem(item MemoItem) error {
	if !memoFileNameRe.MatchString(item.ID + ".md") {
		return fmt.Errorf("非法便签 ID: %q", item.ID)
	}
	data, err := EncodeMemo(item)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(f.dir, item.ID+".md"), data)
}

// RemoveItem 删除单条（不存在视为成功）。
func (f *FileStore) RemoveItem(id string) error {
	if !memoFileNameRe.MatchString(id + ".md") {
		return fmt.Errorf("非法便签 ID: %q", id)
	}
	err := os.Remove(filepath.Join(f.dir, id+".md"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writeFileAtomic tmp+fsync+rename 原子写（与 jsonstore.Save 同构，内容为原样字节）。
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	fh, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := fh.Write(data); err != nil {
		fh.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := fh.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// joinErrors 聚合解析错误（保留 std errors.Join 语义的轻量版，调用方只判空）。
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return fmt.Errorf("%d 条便签装载异常: %s", len(errs), strings.Join(parts, "; "))
}
