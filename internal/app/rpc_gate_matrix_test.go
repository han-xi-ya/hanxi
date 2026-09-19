// rpc_gate_matrix_test.go 是 Wave 3"未安装/停用模块无法从任何已知入口旁路执行"
// 的静态穷举门（AST 级，41 模块逐个校验）：
//  1. Module 类型实现 extapi.GateAware（SetGate 存在，允许与 Info 分居不同文件）；
//  2. New 构造中创建 extapi.NewLeaseHolder；
//  3. 所有持有 *extapi.LeaseHolder 字段的服务结构体，其每个导出方法
//     （= Wails 绑定面，前端可直调）方法体必须调用 Enter()——豁免表除外。
//     口径：返回 error 的方法拒则回传；纯 void 方法 Enter+早退（拒即静默，
//     无需改签名）；仅返回单值无 error 的方法必须扩展为 (T, error) 如实上抛，
//     禁止"拒则回空值"的静默消化（误导性空表同样是故障）。
//
// 豁免表按"装配布线 setter"纪律维护（ADR-0001 Wave 3 注记）：这些方法的 Go 直调
// 时序先于 gate 注入，接门会破坏装配；新增豁免必须附原因并在此登记。
package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// gateExemptMethods 是服务结构体上不接调用门的导出方法（装配布线通道）。
var gateExemptMethods = map[string]string{
	"SetWailsApp":       "装配注入 Wails App 引用，直调时序先于 gate",
	"SetHistory":        "装配注入历史 store，直调时序先于 gate",
	"SetMemoHook":       "装配注入 fileshare→memo 联动钩子，直调时序先于 gate",
	"SetMainWindow":     "装配注入主窗引用（quickmenu 唤窗），直调时序先于 gate",
	"SetHotkeyRegistry": "装配交接热键注册表（msgboard），直调时序先于 gate",
	// G-A/G-C 收敛新增（Wave 3）：
	"SetSnipHotkeyBinding": "装配注入 snip 热键落实通道（ocr，app/hotkeys.go），直调时序先于 gate",
}

func TestRPCGateCoverage(t *testing.T) {
	root := repositoryRoot(t)
	contract := loadCompositionContract(t, root)
	for _, mod := range contract.Modules {
		t.Run(mod.ID, func(t *testing.T) {
			checkPackageGate(t, root+"/internal/modules/"+mod.ID)
		})
	}
}

func checkPackageGate(t *testing.T, pkgDir string) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkgDir, func(fi fs.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析 %s: %v", pkgDir, err)
	}

	var (
		holderStructs      = map[string]bool{} // 持 LeaseHolder 字段的服务结构体
		moduleStructs      = map[string]bool{} // 实现 Info() 的 Module 候选（跨文件安全）
		gateAwareStructs   = map[string]bool{} // 实现 SetGate() 的结构体候选
		sawLeaseHolderCall bool
	)

	type exportedMethod struct {
		recv, name string
		decl       *ast.FuncDecl
	}
	var methods []exportedMethod

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			// 结构体字段收集。
			ast.Inspect(file, func(n ast.Node) bool {
				if ts, ok := n.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						for _, f := range st.Fields.List {
							if fieldIsLeaseHolder(f) {
								holderStructs[ts.Name.Name] = true
							}
						}
					}
				}
				return true
			})
			// 声明遍历（不依赖声明顺序）。
			for _, decl := range file.Decls {
				d, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				recv := receiverName(d)
				switch {
				case d.Name.Name == "Info" && recv != "":
					moduleStructs[recv] = true
				case d.Name.Name == "SetGate" && recv != "":
					gateAwareStructs[recv] = true
				case d.Name.Name == "New" && recv == "":
					if containsCall(d, "NewLeaseHolder") {
						sawLeaseHolderCall = true
					}
				}
				if recv != "" && ast.IsExported(d.Name.Name) {
					methods = append(methods, exportedMethod{recv: recv, name: d.Name.Name, decl: d})
				}
			}
		}
	}

	// GateAware：某个实现 Info() 的 Module 结构体同时实现 SetGate()。
	sawGateAware := false
	for s := range moduleStructs {
		if gateAwareStructs[s] {
			sawGateAware = true
		}
	}
	if !sawGateAware {
		t.Error("Module 类型未实现 extapi.GateAware.SetGate")
	}
	if !sawLeaseHolderCall {
		t.Error("New() 未创建 extapi.NewLeaseHolder")
	}
	if len(holderStructs) == 0 {
		t.Fatal("包内没有持有 *extapi.LeaseHolder 的服务结构体")
	}

	for _, m := range methods {
		if !holderStructs[m.recv] {
			continue // 非 RPC 服务面
		}
		if containsCall(m.decl, "Enter") {
			continue
		}
		if _, ok := gateExemptMethods[m.name]; ok {
			continue
		}
		t.Errorf("RPC 旁路：%s.%s 未接调用门（如是装配布线通道，需在 gateExemptMethods 登记原因）", m.recv, m.name)
	}
}

func receiverName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	switch t := d.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func fieldIsLeaseHolder(f *ast.Field) bool {
	star, ok := f.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "extapi" && sel.Sel.Name == "LeaseHolder"
}

// containsCall 检查函数声明内是否存在指定名字的调用（任意接收者）。
func containsCall(d *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(d, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name == name {
				found = true
			}
		case *ast.Ident:
			if fn.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}
