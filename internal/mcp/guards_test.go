package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoStdoutWrites 静态守卫"MCP 进程任何代码不得 print 到 stdout"（PLAN_MCP §2.1 纪律）：
// 包内非测试文件禁止出现 os.Stdout 引用与 fmt.Print* 系（stdout 即 JSON-RPC 协议通道，
// 任何杂写都是协议污染；日志一律 slog→文件/stderr）。
func TestNoStdoutWrites(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "os" && x.Sel.Name == "Stdout" {
					t.Errorf("%s: 引用 os.Stdout 违反 stdio 协议通道纪律", name)
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "fmt" {
						switch sel.Sel.Name {
						case "Print", "Println", "Printf":
							t.Errorf("%s: fmt.%s 写 stdout 违反协议通道纪律", name, sel.Sel.Name)
						}
					}
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何包源文件——守卫失效")
	}
}

// TestMCPGoVersionPinned 决策 6：mcp-go 锁 v0.41.1（离线可构建已证，v1.x 待生态稳定另议）。
// go.mod 漂移即红——升级必须显式改本测试并复验 API。
func TestMCPGoVersionPinned(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^\tgithub\.com/mark3labs/mcp-go (v[0-9][0-9.\-]*[0-9a-z])`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatal("go.mod 中找不到 github.com/mark3labs/mcp-go 依赖声明")
	}
	if string(m[1]) != "v0.41.1" {
		t.Errorf("mcp-go version = %s, want v0.41.1（PLAN_MCP 决策 6 锁版本）", m[1])
	}
}

// TestToolNamesStable 工具英文名是对外契约（客户端配置/授权键/文档引用），改名即断链：
// 白名单钉死，新增工具须同步进本表与 knownModuleIDs。
func TestToolNamesStable(t *testing.T) {
	want := map[string]string{
		"hanxi_envcheck_detect": "envcheck",
		"hanxi_file_search":     "everything",
		"hanxi_ocr_recognize":   "ocr",
		"hanxi_memo_search":     "memo",
	}
	if len(toolDefs) > len(want) {
		t.Fatalf("工具面只能从白名单扩张到 %d 件，当前 %d 件", len(want), len(toolDefs))
	}
	for _, def := range toolDefs {
		if want[def.Name] != def.ModuleID {
			t.Errorf("tool %q module = %q, want %q", def.Name, def.ModuleID, want[def.Name])
		}
	}
}
