package mcpwizard

// access_write_test.go：R6 写引擎单元面（建档/改键/拒盲写/重置备份/呈现口径）。
// 「写出的字节读者认不认」不在此测——那是 access_readmatch_test.go 对拍矩阵的职责。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// accessBytes 读回 svc 授权文件原始字节（断言"恰好八键/无 BOM"等字面契约用）。
func accessBytes(t *testing.T, svc *McpWizardService) []byte {
	t.Helper()
	data, err := os.ReadFile(svc.accessPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSetToolAccessCreatesAndToggles(t *testing.T) {
	svc, _ := newTestService(t, nil)

	// 缺文件凭空建档：目录由写方创建（读方永不建——写方补位是本文件的职责设定）。
	orphan := &McpWizardService{accessPath: filepath.Join(t.TempDir(), "no-such-dir", "access.json"), writeFn: atomicWrite, now: svc.now}
	info, err := orphan.SetToolAccess("ocr", true)
	if err != nil {
		t.Fatalf("缺档首次开关应建档成功: %v", err)
	}
	if !info.Readable || !info.Tools.Ocr || info.Tools.Envcheck {
		t.Fatalf("建档后呈现异常: %+v", info)
	}

	// 常规链：八键归一、改一键不动其余、撤权回 false（扩充批后 sysinfo/logs/portscan/lan 同台）。
	info, err = svc.SetToolAccess("envcheck", true)
	if err != nil || !info.Tools.Envcheck {
		t.Fatalf("开启 envcheck 失败: %+v %v", info, err)
	}
	if _, err = svc.SetToolAccess("memo", true); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetToolAccess("sysinfo", true); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetToolAccess("logs", true); err != nil {
		t.Fatal(err)
	}
	info, err = svc.SetToolAccess("envcheck", false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Tools.Envcheck || !info.Tools.Memo || !info.Tools.Sysinfo || !info.Tools.Logs || !info.Readable {
		t.Errorf("改一键不应波及其余键: %+v", info.Tools)
	}
	// 落盘字面契约：恰好八键（键数=accessToolKeys 契约数，契约扩充批四→六→八键
	// 的历史硬编码 6 已改随键集自适应，"恰好"语义不变）、version=1、无 BOM（首字节即 '{'）。
	data := accessBytes(t, svc)
	if data[0] != '{' {
		t.Errorf("不得带 BOM/前导垃圾，首字节 %q", data[0])
	}
	var raw struct {
		Version int             `json:"version"`
		Tools   map[string]bool `json:"tools"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("落盘非合法 JSON: %v", err)
	}
	if raw.Version != 1 || len(raw.Tools) != len(accessToolKeys) {
		t.Fatalf("应为 version=1 + 恰好八键（len(accessToolKeys)）: %s", data)
	}
	for _, key := range accessToolKeys {
		if _, ok := raw.Tools[key]; !ok {
			t.Errorf("缺键 %s: %s", key, data)
		}
	}
}

func TestSetToolAccessRejectsUnknownKey(t *testing.T) {
	svc, _ := newTestService(t, nil)
	if _, err := svc.SetToolAccess("envchek", true); err == nil || !strings.Contains(err.Error(), "未知授权键") {
		t.Fatalf("拼错键必须拒绝: %v", err)
	}
	if _, err := os.Stat(svc.accessPath); !os.IsNotExist(err) {
		t.Error("非法键调用不得产生文件")
	}
}

// TestSetToolAccessRefusesBlindWrite 读方不采信的每一类形态，写方都必须拒盲写，
// 且拒绝路径一个字节不碰目标文件（覆盖修复只认显式 ResetAccess）。
func TestSetToolAccessRefusesBlindWrite(t *testing.T) {
	cases := map[string]string{
		"未知tools键":  `{"version":1,"tools":{"envcheck":true,"portkill":true}}`,
		"未知顶层字段":    `{"version":1,"tools":{"envcheck":true},"extra":1}`,
		"version为2": `{"version":2,"tools":{"envcheck":true}}`,
		"缺tools对象":  `{"version":1}`,
		"尾随垃圾":      `{"version":1,"tools":{"envcheck":true}}{"version":1}`,
		"半截JSON":    `{"version":1,"tools":`,
		"空文件":       ``,
		"BOM开头":     string([]byte{0xEF, 0xBB, 0xBF}) + `{"version":1,"tools":{"envcheck":true}}`,
		"值为字符串":     `{"version":1,"tools":{"envcheck":"yes"}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			svc, _ := newTestService(t, nil)
			writeFile(t, svc.accessPath, content)
			_, err := svc.SetToolAccess("memo", true)
			if err == nil || !strings.Contains(err.Error(), "修复") {
				t.Fatalf("损坏态必须拒盲写并指引修复: %v", err)
			}
			if readFile(t, svc.accessPath) != content {
				t.Error("拒绝路径不得改动 access.json")
			}
			info, ierr := svc.GetAccessOverview()
			if ierr != nil {
				t.Fatal(ierr)
			}
			if info.Readable || info.Tools.Memo {
				t.Errorf("拒写后呈现仍须为读者视角全关: %+v", info)
			}
		})
	}
}

func TestResetAccessBacksUpAndRewritesAllOff(t *testing.T) {
	svc, _ := newTestService(t, nil)
	corrupt := `{"version":1,"tools":{"envcheck":true,"portkill":true}}`
	writeFile(t, svc.accessPath, corrupt)

	res, err := svc.ResetAccess()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.BackupPath == "" {
		t.Fatalf("重置应成功并报备份路径: %+v", res)
	}
	bakData, err := os.ReadFile(res.BackupPath)
	if err != nil || string(bakData) != corrupt {
		t.Fatalf("覆盖前旧档必须原样另存: %q %v", bakData, err)
	}
	if !strings.HasPrefix(filepath.Base(res.BackupPath), "access.json.hanxi-bak-") {
		t.Errorf("备份命名应随本包 .hanxi-bak-<ts> 先例: %s", res.BackupPath)
	}
	info, err := svc.GetAccessOverview()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Readable || info.Tools != (AccessTools{}) {
		t.Errorf("重置后应为可采信的全关标准档: %+v", info)
	}
	// 重置后开关恢复可用（拒写态解除）。
	if _, err := svc.SetToolAccess("everything", true); err != nil {
		t.Errorf("重置后 SetToolAccess 应恢复正常: %v", err)
	}
}

func TestResetAccessCreatesWhenMissing(t *testing.T) {
	svc, _ := newTestService(t, nil)
	res, err := svc.ResetAccess()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.BackupPath != "" {
		t.Fatalf("无旧档重置：成功且无备份: %+v", res)
	}
	info, _ := svc.GetAccessOverview()
	if !info.Exists || !info.Readable || info.Tools != (AccessTools{}) {
		t.Errorf("凭空建档应成全关标准档: %+v", info)
	}
}

func TestAccessOverviewReaderViewPresentation(t *testing.T) {
	svc, _ := newTestService(t, nil)

	// 缺文件：Exists=false 但属合法态（非损坏），呈现六 false。
	info, err := svc.GetAccessOverview()
	if err != nil {
		t.Fatal(err)
	}
	if info.Exists || info.Readable || info.Tools != (AccessTools{}) {
		t.Fatalf("缺档呈现异常: %+v", info)
	}
	if !strings.Contains(info.Note, "fail-closed") || !strings.Contains(info.Note, "建档") {
		t.Errorf("缺档文案应说明合法全关+开关建档: %s", info.Note)
	}

	// 合法但非标准形态（缺键——含"契约扩充批之前的四键老档"升级场景）：读方采信，
	// 呈现按缺键=false，写侧回写时归一八键。
	writeFile(t, svc.accessPath, `{"version":1,"tools":{"envcheck":true}}`)
	info, _ = svc.GetAccessOverview()
	if !info.Readable || !info.Tools.Envcheck || info.Tools.Memo || info.Tools.Sysinfo || info.Tools.Logs {
		t.Fatalf("部分键合法档呈现异常: %+v", info)
	}

	// 损坏：危险态呈现必须给修复台阶，且工具值按读者视角全 false（不展示读者不认的字面值）。
	writeFile(t, svc.accessPath, `{"version":1,,}`)
	info, _ = svc.GetAccessOverview()
	if !info.Exists || info.Readable || info.Tools != (AccessTools{}) {
		t.Fatalf("损坏呈现异常: %+v", info)
	}
	if !strings.Contains(info.Note, "损坏") || !strings.Contains(info.Note, "fail-closed") || !strings.Contains(info.Note, "修复") {
		t.Errorf("损坏文案应含现象+口径+出路: %s", info.Note)
	}
	// 旧红线延续（R2 收口口径）：不得暗示存在无确认的自动初始化/重建方
	if strings.Contains(info.Note, "首次运行") {
		t.Errorf("损坏文案不得暗示自动重建: %s", info.Note)
	}
}
