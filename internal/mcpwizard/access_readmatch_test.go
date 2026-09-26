package mcpwizard

// access_readmatch_test.go —— R6 的灵魂：写读对拍。
//
// 写方（本包 access_write.go）与读方（internal/mcp/access.go，领地只读）各自持有
// 一套严格规则；本测试不用任何 mock，直接以真读方 mcp.Access 解析写方落盘的字节：
// 九键 true/false 全矩阵（2^9=512 组合，契约自四键→六键→八键→九键逐批扩量、
// 随 len(accessToolKeys) 自适应）逐键核对语义一致，另核"保存即生效"（同一 Access
// 实例在写方改档后立刻翻转）与拒绝形态的双侧同判。读方规则若有漂移（新加拒读
// 条件/键集变化），这里第一时间红。portkill 键自端口查杀批入矩阵：写侧整档回写
// 不吞破坏族键值由矩阵 + access_write_test 专门 round-trip 双重钉死。
//
// 边界豁免说明（包注释）：仅测试文件 import internal/mcp，生产编译依赖图不变。

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"hanxi/internal/mcp"
)

func TestWriterBytesPassStrictReaderMatrix(t *testing.T) {
	keys := accessToolKeys
	for combo := 0; combo < 1<<len(keys); combo++ {
		name := ""
		for i, k := range keys {
			if combo&(1<<i) != 0 {
				name += "+" + k
			}
		}
		if name == "" {
			name = "=all-false"
		}
		t.Run(fmt.Sprintf("组合%0*b%s", len(keys), combo, name), func(t *testing.T) {
			svc, _ := newTestService(t, nil)
			// 从"缺文件"合法态出发，逐键走真实 RPC 拨到目标组合。
			for i, k := range keys {
				want := combo&(1<<i) != 0
				if _, err := svc.SetToolAccess(k, want); err != nil {
					t.Fatalf("SetToolAccess(%s,%v) 失败: %v", k, want, err)
				}
			}
			reader := mcp.NewAccess(svc.accessPath) // 真读方，指向写方落的字节
			for i, k := range keys {
				want := combo&(1<<i) != 0
				if got := reader.Allowed(k); got != want {
					data, _ := os.ReadFile(svc.accessPath)
					t.Fatalf("Allowed(%s)=%v, want %v，盘上字节:\n%s", k, got, want, data)
				}
			}
			// 即时撤权/即时开权：同一读者实例不重建，改档后下一眼必须翻转
			// （读方每次 Allowed 都重读盘，进程内不养缓存）。
			pos := combo % len(keys)
			first := keys[pos]
			wantFlipped := combo&(1<<pos) == 0 // 原 false → 置 true；原 true → 置 false
			if _, err := svc.SetToolAccess(first, wantFlipped); err != nil {
				t.Fatal(err)
			}
			if got := reader.Allowed(first); got != wantFlipped {
				t.Fatalf("改档后即时性失败 Allowed(%s)=%v want %v", first, got, wantFlipped)
			}
			if !reader.Allowed(first) && reader.LastReason() == "" {
				t.Error("读者拒绝时应留诊断原因")
			}
		})
	}
}

// TestStrictLoaderAgreesWithReaderRejections 双保险：读者拒读的一切形态，
// 本包 strictLoadAccess 必须同判拒（并因此拒盲写），呈现侧同口径。
func TestStrictLoaderAgreesWithReaderRejections(t *testing.T) {
	rejected := []string{
		`{"version":2,"tools":{"envcheck":true}}`,
		// portkill 已入九键白名单，不再是未知键样本——换 destructive 侧同款假想键。
		`{"version":1,"tools":{"envcheck":true,"format_disk":true}}`,
		`{"version":1,"tools":{"envcheck":true},"future":"x"}`,
		`{"version":1}`,
		`{broken`,
		`{"version":1,"tools":{"envcheck":true}}{"version":1,"tools":{"memo":true}}`,
	}
	accepted := []string{
		`{"version":1,"tools":{}}`,
		`{"version":1,"tools":{"memo":true}}`,
		`{"version":1,"tools":{"sysinfo":true,"logs":true}}`,
		"{\"version\":1,\"tools\":{\"envcheck\":true}}\n", // 尾随换行合法（读方 access_test 同款）
	}
	for _, content := range append(rejected, accepted...) {
		path := filepath.Join(t.TempDir(), "access.json")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		st := strictLoadAccess(path)
		reader := mcp.NewAccess(path)
		allDenied := true
		for _, key := range accessToolKeys {
			if reader.Allowed(key) {
				allDenied = false
			}
		}
		wantStrictLegal := !slices.Contains(rejected, content)
		if st.legal != wantStrictLegal {
			t.Errorf("strict 判定与测试预期分叉 legal=%v want=%v 内容 %s", st.legal, wantStrictLegal, content)
		}
		if st.legal {
			// 合法且含 memo:true 的组合读方必须放行 memo（其余形态允许全 false）
			if content == "{\"version\":1,\"tools\":{\"envcheck\":true}}\n" && !reader.Allowed("envcheck") {
				t.Errorf("读方应采信合法档的 envcheck: %s", content)
			}
		} else if !allDenied {
			t.Errorf("strict 拒读但读方放行了，规则分叉: %s", content)
		}
	}
}
