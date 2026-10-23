package keyops

// 目录围栏：本包唯一的"能碰哪儿"判定。
//
// 为什么这一层必须写得比功能本身更小心：DeletePair 的 path 来自前端与主机账本
// （JSON 文件，可被同步链路上的任何一环改过），围栏一旦失效，这个函数的杀伤半径
// 就是**任意文件删除**。所以判定原则只有一条：**判不出来就拒**，绝不"按字面放行、
// 出问题时再说"。
//
// 三道判定（缺一不可）：
//  1. **形态收敛**：Abs + Clean，再对已存在的祖先做 EvalSymlinks。这一道把 8.3 短名
//     （`ADMINI~1`）、大小写、`..` 全部统一成一种写法——不统一，"围栏根"与"目标路径"
//     就是同一个东西的两种写法，比较会在合法路径上误拒（本机实测过一次）。
//  2. **解析后的落点判定**：EvalSymlinks 之后仍须在 dir 内。软链接（以及 POSIX 上的
//     symlink）指到外面去，在这里现形。
//  3. **逐段重解析点判定**（rejectReparseUnder）：路径从 dir 往下走的每一段都不许是
//     重解析点。**这道不是冗余**：本机实测 Go 的 EvalSymlinks **不解析 Windows junction**
//     ——junction 目录在 Lstat 里就是普通目录，不会被当成链接读，于是 `keys/junction/x`
//     原样返回、字面上仍在围栏内，跟着它删就删到外面去了。本仓踩过 junction 跟随删除的坑
//     （构建 worktree 那次连坐删空主仓 node_modules），只有查 FILE_ATTRIBUTE_REPARSE_POINT
//     才抓得住它。判定实现按平台分文件（reparse_windows.go / reparse_other.go）。
//
// 如实写明本层的限度：以上是**检查时**的判定，检查与写删之间没有原子性（TOCTOU）。
// 本机有权限的其它进程若在这两条指令之间把目录换成链接，本层拦不住；真正的边界是
// Windows 账户与 ACL（以及"这个目录归谁"），不是这里的比较。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// resolveDir 把 dir 归一成"可比较的真实绝对路径"。dir 必须存在：
// 它是调用方（父包）给出的工作目录，不存在就说明前置条件没成立，
// 本包不替调用方建目录——静默建目录等于往用户指定的任意路径落一个新目录，
// 那是比写坏一把钥匙更大的动作。
func resolveDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("%w: 密钥目录参数为空", ErrInvalidInput)
	}
	if strings.ContainsRune(dir, 0) {
		return "", fmt.Errorf("%w: 密钥目录含 NUL 字节", ErrInvalidInput)
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", fmt.Errorf("解析密钥目录绝对路径失败: %w", err)
	}
	root, err := evalExistingPrefix(abs)
	if err != nil {
		return "", err
	}
	// 目录必须真实存在且是目录。不替调用方建目录，也不"当作空目录继续往下走"：
	// 报错清楚比在第五步写盘时才炸出一句难懂的 IO 错误好。
	fi, serr := os.Stat(root)
	if serr != nil {
		if errors.Is(serr, os.ErrNotExist) {
			return "", fmt.Errorf("%w: 密钥目录 %s 不存在", ErrTargetMissing, root)
		}
		return "", fmt.Errorf("检查密钥目录 %s 失败: %w", root, serr)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%w: 给定的密钥目录 %s 是文件，不是目录", ErrNotRegularFile, root)
	}
	return root, nil
}

// evalExistingPrefix 从长到短找到第一个真实存在的祖先，对它做 EvalSymlinks，
// 再把还不存在的尾巴原样接回去。
//
// 为什么不直接 filepath.EvalSymlinks(整条)：新建密钥时目标本来就不存在，
// 官方函数对不存在的路径直接报错。但**祖先链必须解析干净**——逃逸恰恰发生在
// 祖先目录是软链接/junction 的情形里，"目标不在"不能成为跳过围栏的理由。
func evalExistingPrefix(abs string) (string, error) {
	var tail []string
	cur := abs
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			out := real
			for i := len(tail) - 1; i >= 0; i-- {
				out = filepath.Join(out, tail[i])
			}
			return filepath.Clean(out), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			// 权限、坏重解析点之类的真故障：拒，不降级成"按字面算"。
			return "", fmt.Errorf("解析 %s 的真实路径失败，为避免误判位置已中止: %w", cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur { // 已到卷根还不存在
			return "", fmt.Errorf("%w: 路径 %s 的上级目录都不存在", ErrTargetMissing, abs)
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// within 判 target 是否**严格**位于 root 之下（root 自身不算在里面：
// 把目录本身当密钥文件删掉是更糟的事故，不能靠"前缀匹配"蒙过去）。
// 两侧都要求已经 Abs+Clean+eval 过：这里做的是纯前缀比较，脏路径比不得。
func within(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	a, b := foldVolumeCase(root), foldVolumeCase(target)
	if a == b {
		return false
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(a, sep) {
		a += sep
	}
	return strings.HasPrefix(b, a)
}

// foldVolumeCase Windows 文件系统默认大小写不敏感：比较必须折叠大小写，
// 否则 `C:\SSH\id` 与 `c:\ssh\id` 会被判成两个地方——围栏就会在无关的大小写差异上
// 误拒，而误拒一旦被人当成噪音绕过，真正的逃逸也跟着被绕过了。
func foldVolumeCase(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

// outsideErr 围栏拒绝的文案：把"给的路径 / 解析后的真身 / 围栏边界"三样都摆出来，
// 让人一眼看出是软链接在捣鬼还是真传错了目录。路径不是秘密，指纹与内容才是。
func outsideErr(dir, given, resolved string) error {
	return fmt.Errorf("%w: %s 解析后实际位于 %s，不在目录 %s 内——本包只在给定目录里动作，拒绝越界",
		ErrOutsideDir, given, resolved, dir)
}

// guardNewPath 用于"在 dir 下新建/覆盖一个叫 name 的文件"（Generate）。
// name 已由 validateKeyName 挡掉分隔符，这里仍走一遍真实解析核对围栏——纵深防御：
// 日后若名字规则被放宽，围栏不该跟着一起失效。
func guardNewPath(dir, name string) (root, path string, err error) {
	root, err = resolveDir(dir)
	if err != nil {
		return "", "", err
	}
	cand := filepath.Join(root, name)
	if !within(root, cand) {
		return root, "", outsideErr(root, cand, cand)
	}
	// 与 guardAgainstRoot 同一套判定：现在 name 不含分隔符，这一段只会查到 name
	// 自身（已由 checkOverwrite 判过形态）；留着是为了日后名字规则放宽时围栏不跟着降级。
	if err = rejectReparseUnder(root, cand); err != nil {
		return root, "", err
	}
	return root, cand, nil
}

// guardExistingPath 用于"dir 下一个应该已经存在的路径"（DeletePair）。
// 返回解析后的真实绝对路径；字面与真实形态两道判定都过，任一不过即拒。
func guardExistingPath(dir, path string) (root, real string, err error) {
	root, err = resolveDir(dir)
	if err != nil {
		return "", "", err
	}
	real, err = guardAgainstRoot(root, path)
	if err != nil {
		return root, "", err
	}
	return root, real, nil
}

// guardAgainstRoot 是对**已解析围栏根**的单路径判定，返回可安全用于写删的真实路径。
// EnsureInside（给父包用的公开入口）要先解析 dir 再判多个路径，所以拆成两层。
//
// 为什么不在这里再比一次"字面 Clean 后的路径"：Clean 之后的绝对路径与解析后的真实路径
// 常常只是**同一个东西的两种写法**（本机实测：os.TempDir 给的是 `C:\Users\ADMINI~1\...`
// 短名，EvalSymlinks 把它展成长名），拿字面串比围栏会在合法路径上误拒。
// `dir/../outside` 这种穿越并不因此漏掉——Clean 已经把 `..` 折掉，折完还在不在围栏内
// 由解析后的真身决定，判定只此一处、不留两套口径。
func guardAgainstRoot(root, path string) (string, error) {
	resolved, err := resolveForWrite(path)
	if err != nil {
		return "", err
	}
	if !within(root, resolved) {
		return "", outsideErr(root, path, resolved)
	}
	// 落点在围栏内不代表路径安全：中途每一段都可能是通往外面的重解析点，
	// 而 EvalSymlinks 对 Windows junction 不解析（实测），第二道判定抓不到它。
	if err = rejectReparseUnder(root, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

// rejectReparseUnder 逐段检查 target 在 root **之下**的那几段，任何一段是重解析点就拒。
//
// 只查 root 之下：root 自己（乃至它的祖先）是链接是调用方的选择——dir 参数由父包给定，
// 本包已经把它解析成真身当围栏根了，再往外一层层追责就把"谁给的目录"这笔账算到纯逻辑层头上。
//
// 某一段此刻不存在就跳过不判：不存在的东西不可能是链接（后面的写/删会如实报"目标不存在"）。
func rejectReparseUnder(root, target string) error {
	rc, tc := splitPath(root), splitPath(target)
	if len(tc) <= len(rc) {
		return nil
	}
	if !samePathParts(rc, tc[:len(rc)]) {
		return nil // 不在同一棵树里，交给 within 的越界判定处理
	}
	// 用 filepath.Join 而不是 strings.Join 拼前缀：POSIX 的根段本身就是 "/"，
	// 拿分隔符去 join 会拼出 "//root" 这种形；Join 会把它归一回 "/root"。
	cur := filepath.Join(tc[:len(rc)]...)
	for _, seg := range tc[len(rc):] {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			continue
		}
		if reparsePoint(fi) {
			return fmt.Errorf("%w: %s 在目录 %s 之内经过了重解析点（软链接或 junction）%s，"+
				"无法确认真实落点，按越界处理——请先确认这条链接指向哪里", ErrOutsideDir, target, root, cur)
		}
	}
	return nil
}

// splitPath 把绝对路径切成可比对的段，卷标（`C:` / `\\srv\share`）单独成段，
// POSIX 的根 `/` 也占一段——否则 Join 回去会变成相对路径，Lstat 就查错了地方。
func splitPath(p string) []string {
	sep := string(filepath.Separator)
	vol := filepath.VolumeName(p)
	parts := make([]string, 0, 8)
	if vol != "" {
		parts = append(parts, vol)
	} else if strings.HasPrefix(p, sep) {
		parts = append(parts, sep)
	}
	for _, s := range strings.Split(p[len(vol):], sep) {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

// samePathParts 段级比较，Windows 上大小写不敏感（与 within 同一口径）。
func samePathParts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if foldVolumeCase(a[i]) != foldVolumeCase(b[i]) {
			return false
		}
	}
	return true
}

// resolveForWrite 把调用方给的路径收敛成"本包接下来就按它动作"的真实绝对路径。
//
// 三步各有分工，缺一步都会留下真窟窿：
//  1. `Abs + Clean`：折掉字面 `.`/`..`，得到可比较的绝对形态；
//  2. `Lstat` 最终组件：**重解析点一律拒**（软链接/junction/目录/设备）。
//     少了这一步，`dir/link/../secret` 这类"穿过链接再回退"的写法会在 Clean 之后
//     看着像在 dir 里，而操作系统真去删的却是 link 对面的那份；
//  3. 解析已存在的祖先（存在时直接 EvalSymlinks 整条）：把短名、大小写与链接祖先
//     统一成盘上真身，后续比较只有一种写法。
func resolveForWrite(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: 路径为空", ErrInvalidInput)
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("%w: 路径含 NUL 字节", ErrInvalidInput)
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("解析路径绝对形式失败: %w", err)
	}
	if abs == filepath.Clean(string(filepath.Separator)) || volumeRoot(abs) == abs {
		return "", fmt.Errorf("%w: %s 指向的是文件系统根，本包不接受它当密钥文件", ErrNotRegularFile, path)
	}
	// 第 2 步。目标不存在（新建/删除竞态之后）不算错，交给后面的存在性判定去报。
	if fi, lerr := os.Lstat(abs); lerr == nil {
		if err = ensureWritableTarget(abs, fi); err != nil {
			return "", err
		}
	} else if !errors.Is(lerr, os.ErrNotExist) {
		return "", fmt.Errorf("检查 %s 失败，为避免动到别处已中止: %w", abs, lerr)
	}
	// 第 3 步。
	resolved, rerr := filepath.EvalSymlinks(abs)
	if rerr != nil {
		if !errors.Is(rerr, os.ErrNotExist) {
			return "", fmt.Errorf("解析 %s 的真实路径失败，为避免动到别处已中止: %w", abs, rerr)
		}
		// 目标还不存在（新建，或删除竞态之后）：围栏不能因为"文件不在"就跳过，
		// 仍要把祖先链解析干净。
		resolved, rerr = evalExistingPrefix(abs)
		if rerr != nil {
			return "", rerr
		}
	}
	return filepath.Clean(resolved), nil
}

// volumeRoot 返回路径所在卷的根（`C:\` 或 `\\srv\share`），取不到回原值。
func volumeRoot(path string) string {
	if v := filepath.VolumeName(path); v != "" {
		return v + string(filepath.Separator)
	}
	return path
}

// EnsureInside 是给调用方用的**公开围栏入口**。
//
// 为什么需要它：SetPassphrase 的签名里没有 dir（对外契约已定，不动它），
// 所以它无法自己判"这把钥匙还在不在密钥目录里"。父包把 RPC 映射过来时，
// 应当先用本函数把账本里的路径钉回密钥目录，判过了再调 SetPassphrase。
// 反过来说：**没调本函数就调 SetPassphrase，等于放弃了目录围栏**——
// 这句话写在这里而不是只写在注释里，是因为它就是本包唯一的围栏缺口。
func EnsureInside(dir, path string) (string, error) {
	root, err := resolveDir(dir)
	if err != nil {
		return "", err
	}
	return guardAgainstRoot(root, path)
}

// ensureWritableTarget 已存在的目标必须是普通文件。软链接/junction/目录一律拒，
// 且**不受 Overwrite 影响**——确认覆盖的是"那个文件"，不是一个通往别处的入口。
//
// 为什么链接也拒（而不是"解析到围栏内就跟着写"）：写操作走 tmp+rename，
// rename 会**用真文件顶掉那个链接**——链接本身消失了，指向的东西原封不动。
// 用户看到"改口令成功"，实际得到的是"我的链接被删了"。这类事宁可拒绝并说明，
// 也不静默改掉别人的目录结构。删除同理：删链接不叫删钥匙，台账会自相矛盾。
func ensureWritableTarget(path string, fi os.FileInfo) error {
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%w: %s 是符号链接或 junction（重解析点），本包不跟着链接写删——请先确认它指向哪里", ErrNotRegularFile, path)
	case fi.IsDir():
		return fmt.Errorf("%w: %s 是目录，密钥操作中这里应当是私钥文件", ErrNotRegularFile, path)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("%w: %s 形态是 %s，不是普通文件", ErrNotRegularFile, path, fi.Mode())
	}
	return nil
}

// requireRegularFile 判目标存在且是普通文件，返回其元数据。
func requireRegularFile(path string) (os.FileInfo, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrTargetMissing, path)
		}
		return nil, fmt.Errorf("读取 %s 的文件信息失败: %w", path, err)
	}
	if err = ensureWritableTarget(path, fi); err != nil {
		return nil, err
	}
	return fi, nil
}
