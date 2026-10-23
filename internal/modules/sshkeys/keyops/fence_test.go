package keyops

// 围栏用例。这一组是全包最重要的测试：它管的是"这个函数最多能伤到哪"。
// 除了字符串层面的 within 判定，还包含**真造一条链接**再试着越界的用例
// ——软链接造不出来时退而求其次造 junction（Windows 重解析点，本仓踩过的那类雷），
// 两种都造不出来就如实跳过，绝不伪装成"测过了"。

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWithinPrefixRules 验 within 的纯前缀判定。
//
// 注意它的契约是**只吃已经 Abs+Clean 过的路径**（调用方 guardAgainstRoot 保证这一点），
// 所以这里不放 `..\` 这类未收敛的样例——把"再猜一次"塞进比较函数，只会让两道判定
// 互相依赖、谁都不再可信。
func TestWithinPrefixRules(t *testing.T) {
	sep := string(filepath.Separator)
	join := func(parts ...string) string { return strings.Join(parts, sep) }
	keys := join("root", "keys") // 围栏根

	type pair struct {
		root   string
		target string
		want   bool
		why    string
	}
	cases := []pair{
		{keys, join(keys, "id_x"), true, "直系子文件"},
		{keys, join(keys, "sub", "id_x"), true, "孙层也在围栏内"},
		{keys, keys, false, "目录自身不算在内"},
		{keys, join(keys+"_evil", "id_x"), false, "同前缀的兄弟目录不能被前缀匹配蒙混"},
		{keys, keys + "_evil", false, "同上，只差一个分隔符就要命"},
		{keys, join("root", "key"), false, "更短的兄弟名"},
		{keys, join("other", "keys", "id_x"), false, "换了上级就是越界"},
		{keys, "root", false, "父目录"},
		{keys, join("root", "keys2", "id_x"), false, "兄弟目录下的同名前缀"},
		{"", join(keys, "id_x"), false, "空围栏一律不认"},
		{keys, "", false, "空目标一律不认"},
	}
	if runtime.GOOS == "windows" {
		// Windows 专属：大小写与盘符大小写都不该影响判定
		cases = append(cases,
			pair{strings.ToUpper(keys), strings.ToLower(keys) + sep + "id_x", true, "Windows 大小写不敏感"},
			pair{join("C:", keys), join("c:", keys, "id_x"), true, "盘符大小写不同也认"},
		)
	} else {
		cases = append(cases,
			pair{keys, join("root", "KEYS", "id_x"), false, "POSIX 大小写敏感：两个不同目录不许当成同一个"},
		)
	}

	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			if got := within(c.root, c.target); got != c.want {
				t.Errorf("within(%q, %q) 应为 %v（%s），实得 %v", c.root, c.target, c.want, c.why, got)
			}
		})
	}
}

func TestResolveDirNormalizesNoiseAndShortForm(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "keys")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatalf("建子目录失败: %v", err)
	}
	canonical, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatalf("解析基准路径失败: %v", err)
	}

	// 各种"指向同一个目录"的脏写法，解析后必须都归到同一条真实路径上——
	// 否则同一次操作里"围栏根"与"目标路径"会来自两套写法，比较就开始漂移了。
	for _, noisy := range []string{
		sub,
		filepath.Join(sub, "."),
		filepath.Join(dir, "keys", "..", "keys"),
		filepath.Join(dir, "keys") + string(filepath.Separator),
	} {
		got, rerr := resolveDir(noisy)
		if rerr != nil {
			t.Fatalf("resolveDir(%q) 失败: %v", noisy, rerr)
		}
		if !samePath(got, canonical) {
			t.Errorf("resolveDir(%q) 应归一到 %q，实得 %q", noisy, canonical, got)
		}
	}

	// 不存在的目录：拒（本包不替调用方建目录）。
	if _, rerr := resolveDir(filepath.Join(dir, "not-there")); !errors.Is(rerr, ErrTargetMissing) {
		t.Errorf("目录不存在应报目标不存在，实得: %v", rerr)
	}
	// 是文件不是目录：拒。
	aFile := filepath.Join(dir, "keys.txt")
	if werr := os.WriteFile(aFile, []byte("x\n"), 0o644); werr != nil {
		t.Fatalf("写干扰文件失败: %v", werr)
	}
	if _, rerr := resolveDir(aFile); !errors.Is(rerr, ErrNotRegularFile) {
		t.Errorf("dir 指向文件应报不是目录，实得: %v", rerr)
	}
	// 空串：拒。
	if _, rerr := resolveDir("  "); !errors.Is(rerr, ErrInvalidInput) {
		t.Errorf("空目录参数应报参数不合法，实得: %v", rerr)
	}
}

func TestEnsureInsideRejectsLiteralEscape(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	outsideDir := filepath.Join(root, "outside")
	for _, d := range []string{dir, outsideDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	victim := filepath.Join(outsideDir, "id_victim")
	if err := os.WriteFile(victim, []byte("不该被动\n"), 0o600); err != nil {
		t.Fatalf("写受害者文件失败: %v", err)
	}
	// 错误文案里出现的是**解析后的真身**（本机 os.TempDir 给的是 8.3 短名，
	// EvalSymlinks 会展成长名），断言也照真身来比，不拿短名去硬撞。
	wantRoot, werr := filepath.EvalSymlinks(dir)
	if werr != nil {
		t.Fatalf("解析围栏根失败: %v", werr)
	}

	cases := []struct {
		given string
		why   string
	}{
		{filepath.Join(dir, "..", "outside", "id_victim"), "dir/../outside 经典穿越"},
		{filepath.Join(dir, "..", "..", "outside", "id_victim"), "穿两级"},
		{victim, "直接给绝对路径（就在外面）"},
		{filepath.Join(dir, "sub", "..", "..", "outside", "id_victim"), "先下再上"},
		{dir + string(filepath.Separator) + ".." + string(filepath.Separator) + "outside" + string(filepath.Separator) + "id_victim", "未 Clean 的脏写法（Clean 之后仍是外面）"},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			got, err := EnsureInside(dir, c.given)
			if !errors.Is(err, ErrOutsideDir) {
				t.Fatalf("应报越界，实得 got=%q err=%v", got, err)
			}
			// 文案里把"给的路径 / 解析后的真身 / 围栏"三样都摆出来，便于诊断
			for _, frag := range []string{"解析后实际位于", wantRoot} {
				if !strings.Contains(err.Error(), frag) {
					t.Errorf("越界文案应含 %q，实得: %v", frag, err)
				}
			}
			// 关键：拒绝即什么都没动
			if string(readFileBytes(t, victim)) != "不该被动\n" {
				t.Fatal("围栏拒绝之后受害者文件仍被改动")
			}
		})
	}

	// 正当路径必须放行，并给出解析后的绝对路径（真身是长名，不是 os.TempDir 给的短名）
	legit := filepath.Join(dir, "id_ok")
	resolved, err := EnsureInside(dir, legit)
	if err != nil {
		t.Fatalf("围栏内的路径应放行，实得: %v", err)
	}
	if !samePath(resolved, filepath.Join(wantRoot, "id_ok")) {
		t.Errorf("放行结果应为 %q，实得 %q", filepath.Join(wantRoot, "id_ok"), resolved)
	}
	// 顺手钉住另一件事：返回的是**长名真身**。本机 %TEMP% 给的是 8.3 短名（含 `~`），
	// 短名不展成同一形态就会与围栏根对不上——那正是先前误拒合法路径的根因。
	if strings.Contains(legit, "~") && samePath(resolved, legit) {
		t.Errorf("短名写法 %q 没有被展成真身，实得 %q", legit, resolved)
	}
	// 空串与 NUL 都属参数不合法，不该走到 IO
	if _, err = EnsureInside(dir, ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("空路径应报参数不合法，实得: %v", err)
	}
	if _, err = EnsureInside(dir, "id_ok\x00"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("含 NUL 的路径应报参数不合法，实得: %v", err)
	}
}

// TestEnsureInsideRejectsLinkAncestorEscape 是这道围栏的**正题**：
// 目录里放一条指向外部的链接（软链接或 junction），穿过它的路径字面上仍在 dir 内，
// 只有解析重解析点才能看出来它通向外面。
func TestEnsureInsideRejectsLinkAncestorEscape(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	victim := filepath.Join(outside, "id_victim")
	if err := os.WriteFile(victim, []byte("不该被动\n"), 0o600); err != nil {
		t.Fatalf("写受害者文件失败: %v", err)
	}

	link := filepath.Join(dir, "link")
	tryDirLink(t, link, outside)

	through := filepath.Join(link, "id_victim")
	if _, err := os.Stat(through); err != nil {
		t.Skipf("链接没能真的通向受害者文件（%v），这条用例在此环境无法成立", err)
	}
	got, err := EnsureInside(dir, through)
	if !errors.Is(err, ErrOutsideDir) {
		t.Fatalf("穿过链接的越界必须被拒，实得 got=%q err=%v", got, err)
	}
	if string(readFileBytes(t, victim)) != "不该被动\n" {
		t.Fatal("围栏拒绝之后受害者文件仍被改动")
	}

	// 同一条链接当作删除目标，也必须拒（DeletePair 用的是同一道判定）
	if _, derr := DeletePair(dir, through, "id_victim"); !errors.Is(derr, ErrOutsideDir) {
		t.Errorf("DeletePair 穿链接越界应被拒，实得: %v", derr)
	}
	if !fileExists(victim) {
		t.Fatal("受害者文件被删掉了——围栏失效")
	}
}

// TestFenceRejectsWindowsJunction 单独把 **junction** 这一种重解析点拎出来实测。
//
// 为什么不能用上面那组的软链接用例代替：软链接与 junction 在盘上是两种不同的
// reparse tag，`os.Symlink` 造出来的不是本仓踩过的那颗雷（构建 worktree 的 junction
// 连坐删除）。上一组用例在本机走的是软链接分支，所以这条**强制走 mklink /J**，
// 拿到的是 junction 的真身证据；造不出来（无 cmd、权限不足）才如实跳过。
func TestFenceRejectsWindowsJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("junction 是 Windows 重解析点，本用例在 %s 上无意义", runtime.GOOS)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	victim := filepath.Join(outside, "id_victim")
	if err := os.WriteFile(victim, []byte("不该被动\n"), 0o600); err != nil {
		t.Fatalf("写受害者文件失败: %v", err)
	}

	link := filepath.Join(dir, "junction")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
	if err != nil {
		t.Skipf("本机造不出 junction（cmd mklink /J 失败：%v：%s），这条实测缺口需如实上报",
			err, strings.TrimSpace(string(out)))
	}
	reportLinkMechanism(t, link, outside)

	through := filepath.Join(link, "id_victim")
	// 先自证链接真的通向外面那份（否则后面的拒绝可能只是"路径不存在"的假绿）
	if _, serr := os.Stat(through); serr != nil {
		t.Skipf("junction 没通向受害者（%v），用例在此环境不成立", serr)
	}

	if _, eerr := EnsureInside(dir, through); !errors.Is(eerr, ErrOutsideDir) {
		t.Errorf("穿过 junction 的 EnsureInside 应报越界，实得: %v", eerr)
	}
	if _, derr := DeletePair(dir, through, "id_victim"); !errors.Is(derr, ErrOutsideDir) {
		t.Errorf("穿过 junction 的 DeletePair 应报越界，实得: %v", derr)
	}
	if string(readFileBytes(t, victim)) != "不该被动\n" {
		t.Fatal("junction 之后的文件被动到了——围栏在这类重解析点上失效")
	}
}

// TestDeletePairDoesNotFollowLinkThenDotDot 验一个更阴的写法：`keys/link/../id_real`。
// filepath.Clean 会把它折成 `keys/id_real`（**看着完全合法、就在围栏里**），
// 但操作系统真去打开时是按段走的：进 link（通往外面）再回退，落点是外面的 id_real。
//
// 本包靠"动作只用解析后的真身路径"堵住这条路：折出来那个 `keys/id_real` 不存在，
// 于是如实报"目标不存在"，外面那份一个字节都动不到。这条用例是那句实话的唯一证据。
func TestDeletePairDoesNotFollowLinkThenDotDot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	realKey := filepath.Join(outside, "id_real")
	writeKeyInto(t, realKey, "")
	tryDirLink(t, filepath.Join(dir, "link"), outside)

	_, err := DeletePair(dir, filepath.Join(dir, "link", "..", "id_real"), "id_real")
	if err == nil {
		t.Fatal("穿过链接再回退的写法竟然删成功了")
	}
	if !errors.Is(err, ErrTargetMissing) {
		t.Errorf("应当只报「围栏内那个折出来的路径不存在」，实得: %v", err)
	}
	if !fileExists(realKey) {
		t.Fatal("外面的真私钥被删了——杀伤半径失控")
	}
}

// TestDeletePairRefusesLinkTarget 验另一侧：目标**本身**是链接时不删。
// 删链接不叫删钥匙，台账会自相矛盾；跟着链接删更是不可接受的杀伤半径。
func TestDeletePairRefusesLinkTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	realKey := filepath.Join(outside, "id_real")
	writeKeyInto(t, realKey, "")

	link := filepath.Join(dir, "id_link")
	tryDirLink(t, link, realKey)

	_, err := DeletePair(dir, link, "id_link")
	if err == nil {
		t.Fatal("目标是链接时 DeletePair 竟然成功了")
	}
	if !fileExists(realKey) {
		t.Fatal("跟着链接把真实私钥删掉了——杀伤半径失控")
	}
	// 链接本身也不该被偷偷删掉
	if !fileExists(link) {
		t.Error("拒绝之后链接不该消失")
	}
}
