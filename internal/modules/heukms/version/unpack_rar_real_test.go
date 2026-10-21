package version

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// unpack_rar_real_test.go 是 RAR5 通道的**真实夹具**用例：
//
// 本仓不入库上游二进制（"用户侧按需下载、不镜像分发"的合规基线），而纯 Go
// 生态又没有 RAR **写**入库可供 CI 造包，所以真解包路径只能在机主本机跑一次：
//
//	HEU_RAR夹具 = 上游 64.0.0 的 HEU_KMS_Activator_v64.0.0.rar（自行下载）
//	HANXI_HEU_RAR=/d/heu/HEU_KMS_Activator_v64.0.0.rar \
//	  go test -run TestUnpackRarReal -v ./internal/modules/heukms/version/
//
// 缺省（未设环境变量）整组跳过，不参与 CI；安全判据本身由 planRarUnpack /
// sanitizeRarName 的纯函数用例锁死（同文件 version_test.go）。
//
// 真值锚点取上游在包内 `更新日志.txt` 逐版公布的主程序哈希（不是 GitHub 整包
// digest——那是 iso/rar 文件本身的摘要）：v64.0.0 的 exe SHA256 为
// 6ad72af453a9c4db99a30317e97689e893a110681e84655392865c8a813e8a5c，
// 2026-10-19 实测解包产物与此逐字节一致（取证记录见 TROUBLESHOOTING #108）。
const realExeSHA256 = "6ad72af453a9c4db99a30317e97689e893a110681e84655392865c8a813e8a5c"

func realRarFixture(t *testing.T) string {
	t.Helper()
	path := os.Getenv("HANXI_HEU_RAR")
	if path == "" {
		t.Skip("未设 HANXI_HEU_RAR（上游 rar 夹具路径），真实解包用例跳过；安全判据见 version_test.go 纯函数用例")
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("HANXI_HEU_RAR 指向的文件不可用: %s", path)
	}
	return path
}

func sha256Of(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestUnpackRarReal(t *testing.T) {
	rar := realRarFixture(t)

	// 1. 条目审阅（pass1 顺带读满全部条目，CRC 在本遍即结算）
	entries, err := listRarEntries(context.Background(), rar)
	if err != nil {
		t.Fatalf("listRarEntries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("真包里应有条目")
	}
	t.Logf("包内条目 %d 枚", len(entries))

	target, err := planRarUnpack(entries)
	if err != nil {
		t.Fatalf("planRarUnpack 对真实上游包应放行: %v", err)
	}
	t.Logf("选定主程序条目: %s", target)

	// 2. 解包落盘并对上游公布值核账
	out := t.TempDir()
	exe, err := unpackRar(context.Background(), rar, out)
	if err != nil {
		t.Fatalf("unpackRar: %v", err)
	}
	if filepath.Base(exe) != filepath.Base(target) {
		t.Fatalf("落地文件名应与包内一致（进程探测依赖），got %q want %q", exe, target)
	}
	if got := sha256Of(t, exe); got != realExeSHA256 {
		t.Fatalf("解包产物哈希与上游公布值不符：got %s want %s", got, realExeSHA256)
	}
	// 只落主程序一枚，附属（cmd/jpg/txt）不进托管目录
	ents, _ := os.ReadDir(out)
	if len(ents) != 1 {
		t.Fatalf("解包应只落主程序，实得 %d 项", len(ents))
	}
	if err := requireMZ(exe); err != nil {
		t.Fatalf("真 exe 应过 MZ 闸: %v", err)
	}
}

func TestUnpackRarRealRejectsTamperedPackage(t *testing.T) {
	rar := realRarFixture(t)

	// 复制一份并篡改压缩数据中段一个字节（夹具不落仓库，故只动临时副本）
	data, err := os.ReadFile(rar)
	if err != nil {
		t.Fatalf("读夹具失败: %v", err)
	}
	bad := make([]byte, len(data))
	copy(bad, data)
	at := len(bad) / 2
	bad[at] ^= 0xff
	tmp := filepath.Join(t.TempDir(), "tampered.rar")
	if err := os.WriteFile(tmp, bad, 0o644); err != nil {
		t.Fatalf("写篡改副本失败: %v", err)
	}

	out := t.TempDir()
	if _, err := unpackRar(context.Background(), tmp, out); err == nil {
		t.Fatal("篡改包必须被拒（rardecode 自带文件 CRC 校验）")
	} else {
		t.Logf("篡改包按预期被拒: %v", err)
	}
	// 关键设计断言：**错误数据不得留在目标目录**（先 .partial 后 rename 的收口）
	ents, _ := os.ReadDir(out)
	if len(ents) != 0 {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("解包失败后不得留半件/错件，实得: %v", names)
	}
}
