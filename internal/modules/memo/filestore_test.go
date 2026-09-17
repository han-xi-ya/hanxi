package memo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleItem() MemoItem {
	return MemoItem{
		ID:        "memo_1758000000000000000",
		Title:     "  多  空格  标题  ",
		Content:   "第一行\n第二行 with #hash and --- fake fence\n",
		Tags:      []string{"#SQL", "#Token", "#含,逗号"},
		IsPinned:  true,
		IsMasked:  false,
		ColorTag:  "amber",
		CreatedAt: time.Date(2026, 9, 17, 10, 0, 0, 0, time.Local),
		UpdatedAt: time.Date(2026, 9, 17, 10, 5, 0, 0, time.Local),
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	item := sampleItem()
	data, err := EncodeMemo(item)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeMemo(item.ID+".md", data)
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, data)
	}
	if got.ID != item.ID {
		t.Errorf("ID = %q", got.ID)
	}
	// 标题按单行语义折叠空白（sanitizeLine）
	if want := "多 空格 标题"; got.Title != want {
		t.Errorf("Title = %q, want %q", got.Title, want)
	}
	// 正文原样（含伪 fence 行与空行语义）：字节级往返
	if got.Content != item.Content {
		t.Errorf("Content = %q, want %q", got.Content, item.Content)
	}
	if strings.Join(got.Tags, "|") != "#SQL|#Token|#含,逗号" {
		t.Errorf("Tags = %v", got.Tags)
	}
	if !got.IsPinned || got.IsMasked || got.ColorTag != "amber" {
		t.Errorf("flags = %+v", got)
	}
	if !got.CreatedAt.Equal(item.CreatedAt) || !got.UpdatedAt.Equal(item.UpdatedAt) {
		t.Errorf("times = %v %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestDecodeStrictness(t *testing.T) {
	if _, err := DecodeMemo("a.md", []byte("# 无 frontmatter\n")); err == nil {
		t.Error("缺头应报错")
	}
	if _, err := DecodeMemo("a.md", []byte("---\nid: a\n")); err == nil {
		t.Error("未闭合应报错")
	}
	if _, err := DecodeMemo("a.md", []byte("---\nbogus line without colon\n---\n")); err == nil {
		t.Error("畸形行应报错")
	}
	// 未知 key 忽略（前向兼容）
	got, err := DecodeMemo("b.md", []byte("---\nid: b\nfutureField: xxx\n---\n正文\n"))
	if err != nil || got.ID != "b" || got.Content != "正文\n" {
		t.Errorf("unknown key: %+v %v", got, err)
	}
	// 空正文合法
	got, err = DecodeMemo("c.md", []byte("---\nid: c\n---\n"))
	if err != nil || got.Content != "" {
		t.Errorf("empty body: %+v %v", got, err)
	}
}

func TestFileStoreSaveLoadRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memo")
	fs := NewFileStore(dir)
	item := sampleItem()
	if err := fs.SaveItem(item); err != nil {
		t.Fatal(err)
	}
	items, err := fs.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("LoadAll = %+v", items)
	}
	if err := fs.RemoveItem(item.ID); err != nil {
		t.Fatal(err)
	}
	items, _ = fs.LoadAll()
	if len(items) != 0 {
		t.Fatalf("Remove 后仍有条目: %+v", items)
	}
	// 删除不存在 = 幂等成功
	if err := fs.RemoveItem("nope"); err != nil {
		t.Errorf("RemoveItem(nope) = %v", err)
	}
	// 非法 ID 拒写
	if err := fs.SaveItem(MemoItem{ID: "../evil"}); err == nil {
		t.Error("路径穿越 ID 应拒")
	}
	if err := fs.SaveItem(MemoItem{ID: "a b/c"}); err == nil {
		t.Error("非法字符 ID 应拒")
	}
}

func TestFileStoreLoadAllQuarantinesBad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memo")
	fs := NewFileStore(dir)
	item := sampleItem()
	if err := fs.SaveItem(item); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memo_bad.md"), []byte("garbage not frontmatter"), 0644); err != nil {
		t.Fatal(err)
	}
	items, err := fs.LoadAll()
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("好条目必须照常装载: %+v", items)
	}
	if err == nil {
		t.Error("坏条目应聚合成错误上报（不静默）")
	}
	entries, _ := os.ReadDir(dir)
	bad := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bad-") {
			bad++
		}
	}
	if bad != 1 {
		t.Errorf("坏文件应隔离为 .bad- 取证副本，found=%d", bad)
	}
}
