package artifact

// ---------- Wave 4 DoD 缺口：磁盘满 / 权限拒绝类故障注入 ----------
//
// 这些用例模拟"落位/提交/清理"各环节遭遇文件系统拒绝的真实故障：
//   - Fetch：rename 落位被目标占位阻挡（目录 / Windows 只读文件）、
//     POSIX 父目录只读导致临时件根本建不出来；
//   - Tree.Commit：staging 内文件被句柄占用（Windows 语义下目录改名被拒）；
//   - Tree.Versions / Resolve：meta.json 半写（截断/垃圾/schema 缺失）容错；
//   - Tree.CleanupAbandoned：.removing- 残件删不掉时的返回与重试。
//
// Windows 上 chmod 语义很弱（目录只读属性不阻止建文件、POSIX 式权限位无效），
// 所以权限类注入按平台选可靠手段，注入前先自检"故障是否真的生效"，
// 不生效即 t.Skip（避免 CI/异平台上的假失败）；rename 被同名目录阻挡一类
// 在 POSIX 与 Windows 都成立，作为全平台真跑用例。

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------- 用例 1：Fetch 写盘 / 落位失败 ----------

func TestFetchPlaceFailureFaults(t *testing.T) {
	body := bytes.Repeat([]byte("fault-injected-payload"), 500)
	goodSHA := shaHex(body)
	srcOf := func(srvURL string) Source {
		return Source{URL: srvURL, SHA256: goodSHA, MaxBytes: 1 << 20, FileName: "pkg.zip"}
	}

	t.Run("目标同名目录阻挡rename落位", func(t *testing.T) {
		// 全平台可靠注入：destPath 预放同名目录 → os.Rename(文件→目录) 必失败。
		srv := serve(t, body)
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		if err := os.Mkdir(dest, 0755); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.RemoveAll(dest) }() // 还原 TempDir 可清状态

		err := Fetch(context.Background(), srcOf(srv.URL), dest, nil, 20*time.Second)
		if err == nil || !strings.Contains(err.Error(), "落位失败") {
			t.Fatalf("期望 rename 落位失败错误，实际: %v", err)
		}
		// 失败即清临时件，不留 .part-* 垃圾；目标路径无半件（仍是原空目录）。
		assertNoPartFiles(t, dir)
		ents, serr := os.ReadDir(dest)
		if serr != nil {
			t.Fatal(serr)
		}
		if len(ents) != 0 {
			t.Fatalf("rename 被拒后目标目录不得混入下载内容: %v", ents)
		}

		// 障碍移除后重试必须成功（故障非粘性）。
		if err := os.Remove(dest); err != nil {
			t.Fatal(err)
		}
		if err := Fetch(context.Background(), srcOf(srv.URL), dest, nil, 20*time.Second); err != nil {
			t.Fatalf("恢复后重试失败: %v", err)
		}
		got, rerr := os.ReadFile(dest)
		if rerr != nil || !bytes.Equal(got, body) {
			t.Fatalf("重试落位内容异常: %v", rerr)
		}
	})

	t.Run("POSIX父目录只读拒绝创建临时件", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows 目录只读属性不阻止在其中建文件，权限注入仅 POSIX 可靠")
		}
		srv := serve(t, body)
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		if err := os.Chmod(dir, 0555); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(dir, 0755) }() // 失败路径也要能还原，避免 TempDir 清理炸

		// 自检注入生效性：root 身份或忽略权限位的文件系统会让写入照样成功。
		probe := filepath.Join(dir, ".perm-probe")
		if werr := os.WriteFile(probe, []byte("x"), 0644); werr == nil {
			_ = os.Remove(probe)
			t.Skip("当前环境对 0555 目录仍可写入（如 root），权限故障注入不成立")
		}
		// 在 0555 状态下执行 Fetch：临时件创建应被权限拒绝。
		err := Fetch(context.Background(), srcOf(srv.URL), dest, nil, 20*time.Second)
		if err == nil || !strings.Contains(err.Error(), "创建临时下载文件失败") {
			t.Fatalf("期望临时件创建被权限拒绝，实际: %v", err)
		}
		assertMissing(t, dest)
		assertNoPartFiles(t, dir)

		// 恢复权限后重试成功。
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := Fetch(context.Background(), srcOf(srv.URL), dest, nil, 20*time.Second); err != nil {
			t.Fatalf("恢复权限后重试失败: %v", err)
		}
		assertPresent(t, dest)
	})

	t.Run("Windows只读目标文件阻挡替换落位", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("POSIX rename 替换目标不受其只读位影响，注入仅 Windows 成立")
		}
		srv := serve(t, body)
		dir := t.TempDir()
		dest := filepath.Join(dir, "pkg.zip")
		if err := os.WriteFile(dest, []byte("OLD"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dest, 0444); err != nil { // Windows 上映射为 FILE_ATTRIBUTE_READONLY
			t.Fatal(err)
		}
		st, serr := os.Stat(dest)
		if serr != nil {
			t.Fatal(serr)
		}
		if st.Mode().Perm()&0200 != 0 {
			t.Skip("只读属性设置未生效，注入不成立")
		}
		defer func() { _ = os.Chmod(dest, 0644) }()

		err := Fetch(context.Background(), srcOf(srv.URL), dest, nil, 20*time.Second)
		if err == nil || !strings.Contains(err.Error(), "落位失败") {
			t.Fatalf("期望落位被只读目标拒绝，实际: %v", err)
		}
		assertNoPartFiles(t, dir)
		// 目标旧内容未被半替换。
		if err := os.Chmod(dest, 0644); err != nil {
			t.Fatal(err)
		}
		if got, rerr := os.ReadFile(dest); rerr != nil || string(got) != "OLD" {
			t.Fatalf("失败的落位竟污染了目标文件: %q %v", got, rerr)
		}

		// 清除只读位与挡路文件后重试成功。
		if err := os.Remove(dest); err != nil {
			t.Fatal(err)
		}
		if err := Fetch(context.Background(), srcOf(srv.URL), dest, nil, 20*time.Second); err != nil {
			t.Fatalf("恢复后重试失败: %v", err)
		}
		got, rerr := os.ReadFile(dest)
		if rerr != nil || !bytes.Equal(got, body) {
			t.Fatalf("重试落位内容异常: %v", rerr)
		}
	})
}

// ---------- 用例 2：Tree.Commit 时 staging 内文件被占用 ----------

func TestTreeCommitStagingFileLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("类 POSIX 平台打开句柄不阻止目录改名，占用注入仅 Windows 成立")
	}
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")
	staging := stageContent(t, tree, "txlock", "app.exe", "payload")
	target := filepath.Join(root, "demo_5.0.0")
	sha := strings.Repeat("aa", 32)

	// Go 在 Windows 上以 FILE_SHARE_READ|WRITE（不含 DELETE）打开文件：
	// 句柄存续期间 staging 目录改名必被拒。
	f, err := os.Open(filepath.Join(staging, "app.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // 断言失败也要释放，否则 TempDir 清理被锁

	// 自检注入生效性：若探测改名竟然成功，说明占用未锁住目录，skip 防假失败。
	if perr := os.Rename(staging, staging+"-probe"); perr == nil {
		_ = os.Rename(staging+"-probe", staging)
		t.Skip("打开句柄未能阻止 staging 改名，占用注入在当前环境不成立")
	}

	err = tree.Commit(staging, "5.0.0", Meta{ZipSHA256: sha})
	if err == nil || !strings.Contains(err.Error(), "落位失败") {
		t.Fatalf("期望占用导致落位失败，实际: %v", err)
	}
	// 目标版本目录不得出现（不留"有目录无账本"或半搬动的损坏现场）。
	if _, serr := os.Stat(target); !os.IsNotExist(serr) {
		t.Fatalf("失败的 Commit 不应产生目标目录: %v", serr)
	}
	// staging 完好：目录还在、内容未损，具备重试条件。
	if got, rerr := os.ReadFile(filepath.Join(staging, "app.exe")); rerr != nil || string(got) != "payload" {
		t.Fatalf("失败后 staging 内容异常: %q %v", got, rerr)
	}

	// 释放句柄后同一 staging 必须可直接重试成功。
	if cerr := f.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if err := tree.Commit(staging, "5.0.0", Meta{ZipSHA256: sha}); err != nil {
		t.Fatalf("解除占用后重试 Commit 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "app.exe")); err != nil {
		t.Fatalf("重试落位内容丢失: %v", err)
	}
	if _, err := tree.Resolve("5.0.0"); err != nil {
		t.Fatalf("重试后账本不可解析: %v", err)
	}
}

// ---------- 用例 3：Tree.Versions 对半写 meta.json 的容错 ----------

func TestTreeVersionsHalfWrittenMeta(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")

	// 一个正常版本作为对照锚点。
	s := stageContent(t, tree, "tx-ok", "app.exe", "x")
	if err := tree.Commit(s, "1.0.0", Meta{ZipSHA256: strings.Repeat("11", 32)}); err != nil {
		t.Fatal(err)
	}

	// 手工预放三个"账本异常"版本目录（模拟写 meta.json 中途掉电/崩溃）。
	badMetas := map[string]string{
		"demo_1.2.0": `{"schema":1,"tool":"demo","version":"1.2.0","zipSHA256":"aa`, // 截断
		"demo_1.1.0": `!!! not json at all !!!`,                                     // 垃圾字节
		"demo_1.0.1": `{"version":"1.0.1"}`,                                         // JSON 合法但缺 schema（readTreeMeta 视为不可信）
	}
	for dir, raw := range badMetas {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, metaFileName), []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Versions 不得 panic、不得报错；异常账本目录如实列出（Meta 零值，供上层展示"异常安装"）。
	list, err := tree.Versions()
	if err != nil {
		t.Fatalf("Versions 对坏账本必须容错而非报错: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("版本数量 = %d, want 4: %+v", len(list), list)
	}
	wantOrder := []string{"1.2.0", "1.1.0", "1.0.1", "1.0.0"}
	for i, want := range wantOrder {
		if list[i].Version != want {
			t.Fatalf("降序排序异常 @%d: %+v", i, list)
		}
	}
	for _, v := range list {
		switch v.Version {
		case "1.0.0":
			if v.Meta.Schema != DefaultSchema {
				t.Fatalf("正常账本被误伤: %+v", v.Meta)
			}
		case "1.2.0", "1.1.0", "1.0.1":
			if v.Meta.Schema != 0 {
				t.Fatalf("坏账本应以零值 Meta 列出（不可信），实际: %+v", v.Meta)
			}
		}
	}

	// Resolve("") 取最新"可用"：必须跳过三个坏账本，落到唯一可信的 1.0.0。
	latest, err := tree.Resolve("")
	if err != nil {
		t.Fatalf("存在可信版本时 Resolve('') 不应报错: %v", err)
	}
	if filepath.Base(latest) != "demo_1.0.0" {
		t.Fatalf("Resolve('') 应跳过坏账本取可信最新: %s", latest)
	}

	// 半写账本目录同版本再提交：拒绝覆盖（防止把伪安装洗成合法），且原目录不被破坏。
	s2 := stageContent(t, tree, "tx-recommit", "app.exe", "y")
	if err := tree.Commit(s2, "1.1.0", Meta{ZipSHA256: strings.Repeat("22", 32)}); err == nil ||
		!strings.Contains(err.Error(), "缺少可信元信息") {
		t.Fatalf("对坏账本目录的再提交必须拒绝: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo_1.1.0", metaFileName)); err != nil {
		t.Fatal("拒绝路径不得破坏既有现场")
	}
}

// ---------- 用例 4：CleanupAbandoned 对删不掉的 .removing- ----------

func TestTreeCleanupAbandonedLockedRemoving(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("POSIX 持句柄不阻止 unlink，.removing- 删不掉的注入仅 Windows 成立")
	}
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")

	// 正式版本一个，确保收尸不误伤。
	s := stageContent(t, tree, "tx1", "app.exe", "x")
	if err := tree.Commit(s, "1.0.0", Meta{ZipSHA256: strings.Repeat("33", 32)}); err != nil {
		t.Fatal(err)
	}

	// 构造两个孤儿：一个内部文件被句柄锁住（删不掉），一个普通可删。
	locked := filepath.Join(root, "demo_2.0.0"+removingMarker+"111")
	plain := filepath.Join(root, tmpPrefix+"orphan")
	for _, d := range []string{locked, plain} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f.bin"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(filepath.Join(locked, "f.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	leftovers := tree.CleanupAbandoned() // 不得 panic

	// 自检注入生效性：若持句柄仍被删净，说明当前环境删不掉的前提不成立。
	if len(leftovers) == 0 {
		t.Skip("句柄未阻止 .removing- 删除，占用注入在当前环境不成立")
	}
	if len(leftovers) != 1 || filepath.Clean(leftovers[0]) != filepath.Clean(locked) {
		t.Fatalf("残件列表异常: %v", leftovers)
	}
	// 可删的照常收走；锁住的完整保留（可枚举、待重试）；正式版本不受波及。
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatalf("普通孤儿未被清理: %v", err)
	}
	if _, err := os.Stat(locked); err != nil {
		t.Fatalf("锁住的残件应完整保留而非半删: %v", err)
	}
	if _, err := tree.Resolve("1.0.0"); err != nil {
		t.Fatalf("收尸误伤正式版本: %v", err)
	}

	// 解除占用后下一轮收尸必须成功（重试语义）。
	if cerr := f.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if leftovers = tree.CleanupAbandoned(); len(leftovers) != 0 {
		t.Fatalf("解除占用后重试运行应清零，实际残留: %v", leftovers)
	}
	if _, err := os.Stat(locked); !os.IsNotExist(err) {
		t.Fatalf("重试后 .removing- 残件仍未删除: %v", err)
	}
}
