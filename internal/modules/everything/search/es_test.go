package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 样例模拟 ES -export-tsv 的真实布局（ES 1.1.0.37 实测）：
// BOM + 每行一个全路径 + CRLF；目录行尾反斜杠；中文为 UTF-8。
const sampleTSV = "\xEF\xBB\xBF C:\\Users\\hanxi\\Documents\\report.pdf\r\n" +
	"D:\\工作\\2026\\年度总结.docx\r\n" +
	"E:\\projects\\项目资料\\\r\n" +
	"\r\n" +
	"F:\\root\\带反斜杠目录\\\r\n"

func TestParseLines(t *testing.T) {
	lines := ParseLines([]byte(sampleTSV))
	if len(lines) != 4 {
		t.Fatalf("期望 4 行，实际 %d: %+v", len(lines), lines)
	}
	if lines[0] != ` C:\Users\hanxi\Documents\report.pdf` {
		t.Errorf("首行解析错误（含 BOM 剥离）: %q", lines[0])
	}
	if !strings.Contains(lines[1], "工作") {
		t.Errorf("中文路径解析错误: %q", lines[1])
	}
	if lines[2] != `E:\projects\项目资料\` {
		t.Errorf("目录行尾反斜杠应保留: %q", lines[2])
	}
}

func TestParseLinesEmptyAndNoise(t *testing.T) {
	if len(ParseLines(nil)) != 0 {
		t.Error("空输入应得空列表")
	}
	if len(ParseLines([]byte("\r\n\r\n"))) != 0 {
		t.Error("纯空行应被过滤")
	}
}

func TestReadMeta(t *testing.T) {
	dir := t.TempDir()

	// 文件：名称/父目录/大小/修改时间
	file := filepath.Join(dir, "报告.pdf")
	content := []byte("hello meta")
	if err := os.WriteFile(file, content, 0644); err != nil {
		t.Fatal(err)
	}
	f, ok := ReadMeta(file)
	if !ok {
		t.Fatal("存在的文件应解析成功")
	}
	if f.Name != "报告.pdf" || f.Path != dir || f.Size != int64(len(content)) || f.IsDir {
		t.Errorf("文件元数据错误: %+v", f)
	}
	if len(f.Modified) != 16 { // 2006-01-02 15:04
		t.Errorf("修改时间格式错误: %q", f.Modified)
	}

	// 目录：IsDir + 尺寸为 0 + 名称不含尾反斜杠
	sub := filepath.Join(dir, "项目资料")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	d, ok := ReadMeta(sub + `\`)
	if !ok || !d.IsDir || d.Name != "项目资料" || d.Size != 0 || d.Path != dir {
		t.Errorf("目录元数据错误: %+v ok=%v", d, ok)
	}

	// 已消失目标
	if _, ok := ReadMeta(filepath.Join(dir, "不存在.txt")); ok {
		t.Error("已消失目标应返回 ok=false")
	}
}

func TestESExePathAndEnsure(t *testing.T) {
	dir := t.TempDir()
	if got := ESExePath(dir); got != filepath.Join(dir, "es.exe") {
		t.Errorf("ESExePath = %q", got)
	}
	// 网络不可用时 EnsureESExe 应返回错误而非半成品（离线单测只验证失败路径不落垃圾）
	if err := EnsureESExe(dir, nil); err == nil {
		_, statErr := os.Stat(filepath.Join(dir, "es.exe"))
		if statErr != nil {
			t.Errorf("已成功安装但 exe 缺失")
		}
	} else {
		// 失败时目录内不得残留 .tmp 半成品
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Errorf("失败路径残留半成品: %s", e.Name())
			}
		}
	}
}