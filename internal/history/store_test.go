package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func mustSave(t *testing.T, s *Store, r Record) {
	t.Helper()
	if err := s.Save(r); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
}

// ---------- 桶裁剪 ----------

func TestTrimNewestFirst(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < maxPerBucket+1; i++ { // 201 条触发裁剪
		mustSave(t, s, Record{FuncType: "ocr", Summary: fmt.Sprintf("第%d条", i)})
	}
	list, err := s.List("ocr", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != maxPerBucket {
		t.Fatalf("桶长 %d, want %d", len(list), maxPerBucket)
	}
	if list[0].Summary != "第200条" {
		t.Fatalf("头插序失真: 首条 = %q", list[0].Summary)
	}
	if list[maxPerBucket-1].Summary != "第1条" { // 第 0 条已被裁掉
		t.Fatalf("最旧保留条失真: 末条 = %q", list[maxPerBucket-1].Summary)
	}
	// 恰满 200 不裁：再存 199 条边界（从 200 倒推验证）
	s2 := newTestStore(t)
	for i := 0; i < maxPerBucket; i++ {
		mustSave(t, s2, Record{FuncType: "ocr", Summary: fmt.Sprintf("x%d", i)})
	}
	if got, _ := s2.List("ocr", ""); len(got) != maxPerBucket {
		t.Fatalf("恰 200 条不应裁剪: %d", len(got))
	}
}

// ---------- 截断 ----------

func TestTruncateLongFields(t *testing.T) {
	s := newTestStore(t)
	long := strings.Repeat("鉴", maxFieldRunes+100) // 中文 rune 计，非字节
	mustSave(t, s, Record{FuncType: "ocr", Input: long, Output: long})
	list, err := s.List("ocr", "")
	if err != nil {
		t.Fatal(err)
	}
	r := list[0]
	for _, got := range []string{r.Input, r.Output} {
		if !strings.HasSuffix(got, truncatedSuffix) {
			t.Fatalf("超限应带截断标记: …%q", tail(got, 20))
		}
		body := strings.TrimSuffix(got, truncatedSuffix)
		if utf8.RuneCountInString(body) != maxFieldRunes {
			t.Fatalf("截断长度 %d, want %d", utf8.RuneCountInString(body), maxFieldRunes)
		}
	}
	// 恰好 4000 不截
	s2 := newTestStore(t)
	exact := strings.Repeat("a", maxFieldRunes)
	mustSave(t, s2, Record{FuncType: "ocr", Input: exact})
	got, _ := s2.List("ocr", "")
	if got[0].Input != exact {
		t.Fatal("恰达上限不应追加截断标记")
	}
}

// ---------- 脱敏 ----------

func TestRedactAtSaveEntry(t *testing.T) {
	s := newTestStore(t)
	mustSave(t, s, Record{
		FuncType: "ocr",
		Summary:  `识别含 password=hunter22 的截图`,
		Input:    `C:\a\token=abcdefgh12345.png`,
		Output:   "正文含 Bearer abcdefgh12345-XYZ 的请求头",
		Extra:    `auth: "s3cretvalue"`,
	})
	list, err := s.List("ocr", "")
	if err != nil {
		t.Fatal(err)
	}
	all := list[0]
	for _, v := range []string{all.Summary, all.Input, all.Output, all.Extra} {
		if strings.Contains(v, "hunter22") || strings.Contains(v, "abcdefgh12345") || strings.Contains(v, "s3cretvalue") {
			t.Fatalf("凭据未被脱敏: %q", v)
		}
	}
	if !strings.Contains(all.Summary, `password="******"`) {
		t.Fatalf("Redact 口径失真: %q", all.Summary)
	}
	// 磁盘文件同样无原文（构造入口收口，落盘必经）
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(s.path), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter22") {
		t.Fatal("落盘文件泄露原文")
	}
}

// ---------- Summary 回落 ----------

func TestSummaryFallbackFromInput(t *testing.T) {
	s := newTestStore(t)
	longInput := strings.Repeat("图", 60) + ".png"
	mustSave(t, s, Record{FuncType: "ocr", Input: longInput})
	list, _ := s.List("ocr", "")
	if list[0].Summary != headRunes(longInput, summaryFromInputRunes) {
		t.Fatalf("Summary 应回落 Input 前 %d 字: %q", summaryFromInputRunes, list[0].Summary)
	}
}

// ---------- 损坏容错（严格档） ----------

func TestCorruptFileStickyNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	garbage := `{"ocr": [这不是合法`
	if err := os.WriteFile(path, []byte(garbage), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	if s.Degraded() == nil {
		t.Fatal("损坏文件应置 loadErr")
	}
	// 写全禁：Save / Delete / Clear 均返回错误
	if err := s.Save(Record{FuncType: "ocr", Input: "x"}); err == nil {
		t.Fatal("损坏驻留期应禁 Save")
	}
	if err := s.Delete(1); err == nil {
		t.Fatal("损坏驻留期应禁 Delete")
	}
	if err := s.Clear("ocr"); err == nil {
		t.Fatal("损坏驻留期应禁 Clear")
	}
	if _, err := s.List("ocr", ""); err == nil {
		t.Fatal("损坏驻留期 List 应报错供面板提示")
	}
	// 绝不空库覆写：磁盘字节原样保留
	raw, _ := os.ReadFile(path)
	if string(raw) != garbage {
		t.Fatal("损坏文件被覆写，违反严格档")
	}
	// 空文件同样驻留（不视为空库）
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, FileName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if NewStore(dir2).Degraded() == nil {
		t.Fatal("0 字节文件应驻留禁写")
	}
}

// ---------- 清桶 / 删除 / 查询 ----------

func TestClearOnlyCurrentBucket(t *testing.T) {
	s := newTestStore(t)
	mustSave(t, s, Record{FuncType: "ocr", Input: "a"})
	mustSave(t, s, Record{FuncType: "portkill", Input: "8080"})
	if err := s.Clear("ocr"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.List("ocr", ""); len(got) != 0 {
		t.Fatal("当前桶应清空")
	}
	if got, _ := s.List("portkill", ""); len(got) != 1 {
		t.Fatal("Q5 裁定：清空仅作用当前桶，他桶必须原样保留")
	}
	// 清不存在的桶：无操作成功
	if err := s.Clear("no-such"); err != nil {
		t.Fatalf("清不存在桶应幂等: %v", err)
	}
}

func TestDeleteByID(t *testing.T) {
	s := newTestStore(t)
	mustSave(t, s, Record{FuncType: "ocr", Input: "keep"})
	mustSave(t, s, Record{FuncType: "ocr", Input: "drop"})
	list, _ := s.List("ocr", "")
	dropID := list[0].ID // 头插，最新在前
	if err := s.Delete(dropID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.List("ocr", "")
	if len(list) != 1 || list[0].Input != "keep" {
		t.Fatalf("删除失真: %+v", list)
	}
	if err := s.Delete(999999); err == nil {
		t.Fatal("删不存在 ID 应报错")
	}
}

func TestListKeywordCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	mustSave(t, s, Record{FuncType: "portkill", Summary: "查询端口 :8080", Input: "8080"})
	mustSave(t, s, Record{FuncType: "portkill", Summary: "终止 node.exe", Input: "pid 4321", Extra: "KILL"})
	for _, kw := range []string{"8080", "查询"} {
		if got, _ := s.List("portkill", kw); len(got) != 1 {
			t.Fatalf("关键词 %q 应命中 1 条: %+v", kw, got)
		}
	}
	if got, _ := s.List("portkill", "kill"); len(got) != 1 || got[0].Input != "pid 4321" {
		t.Fatalf("Extra 应参与大小写不敏感匹配: %+v", got)
	}
	if got, _ := s.List("portkill", "  "); len(got) != 2 {
		t.Fatal("空白关键词视同全桶")
	}
}

// ---------- ID 与重启续接 ----------

func TestIDMonotonicAndPersisted(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	mustSave(t, s, Record{FuncType: "ocr", Input: "a"})
	mustSave(t, s, Record{FuncType: "ocr", Input: "b"})
	list, _ := s.List("ocr", "")
	first, second := list[1].ID, list[0].ID
	if second <= first {
		t.Fatalf("ID 应严格递增: %d → %d", first, second)
	}
	// 落盘可回放（顶层 map[funcType][]Record 格式）
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var dump map[string][]Record
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatalf("落盘格式非 map[funcType][]Record: %v", err)
	}
	if len(dump["ocr"]) != 2 {
		t.Fatalf("落盘条数失真: %+v", dump)
	}
	// 重开实例：新 ID 续接存量最大值之上（重启续接）
	s2 := NewStore(dir)
	mustSave(t, s2, Record{FuncType: "ocr", Input: "c"})
	list2, _ := s2.List("ocr", "")
	if len(list2) != 3 || list2[0].ID <= second {
		t.Fatalf("重启续接失真: %+v", list2)
	}
}

func TestSaveRejectsEmptyFuncType(t *testing.T) {
	if err := newTestStore(t).Save(Record{Input: "x"}); err == nil {
		t.Fatal("空 funcType 应拒绝")
	}
}

// ---------- 并发写 ----------

func TestConcurrentSave(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Save(Record{FuncType: "ocr", Input: fmt.Sprintf("c-%d", i)}); err != nil {
				t.Errorf("并发 Save 失败: %v", err)
			}
		}(i)
	}
	wg.Wait()
	list, err := s.List("ocr", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 20 {
		t.Fatalf("并发写入丢条: %d/20", len(list))
	}
	seen := map[int64]bool{}
	for _, r := range list {
		if seen[r.ID] {
			t.Fatal("并发下 ID 重号")
		}
		seen[r.ID] = true
	}
}

func tail(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[utf8.RuneCountInString(s)-n:])
}
