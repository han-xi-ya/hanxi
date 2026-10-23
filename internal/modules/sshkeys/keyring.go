package sshkeys

// 本机密钥台账（钥匙环）：扫 ~/.ssh，**以"一把钥匙"为单位**列出。
//
// 为什么按钥匙而不是按文件：早先每个文件一行，`id_x` 与 `id_x.pub` 同指纹各占一行，
// 读的人以为有两把钥匙——今天机主就在这上面删错了东西。一把钥匙的私钥与公钥是
// 同一个实体的两面，必须合成一行；"缺配套公钥"或"公钥成了孤儿"则作为状态显式呈现，
// 而不是靠多出来的一行来表达。
//
// **硬边界：密钥内容永不出现在返回值里。** 对外只有路径、类型、公钥指纹、注释、
// 口令三态与被引用情况。指纹与注释优先取自 .pub（公钥本就可公开）；只在确认私钥
// 未加密时才从私钥本身算公钥——加密的那条路径永不尝试解开。
//
// 不提供"改口令/生成/部署"：那些写动作在 manage.go，且都属"写用户目录"的显式动作。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
)

// KeyEntry 一把钥匙的台账。HasPrivate/HasPublic 分开表达，
// 因为"只有公钥"（孤儿 .pub）与"只有私钥"（缺配套公钥）是两种需要区别对待的状态。
type KeyEntry struct {
	Name        string `json:"name"` // 基名（不含 .pub 后缀），也是界面主标识
	HasPrivate  bool   `json:"hasPrivate"`
	HasPublic   bool   `json:"hasPublic"`
	PrivatePath string `json:"privatePath,omitempty"`
	PublicPath  string `json:"publicPath,omitempty"`

	Type        string `json:"type,omitempty"`        // ssh 算法名，判不出则空
	Fingerprint string `json:"fingerprint,omitempty"` // SHA256:…；拿不到则空
	Comment     string `json:"comment,omitempty"`     // 公钥注释（日后在名册里认钥匙全靠它）
	Bits        int    `json:"bits,omitempty"`

	Guarded    bool `json:"guarded"`
	GuardKnown bool `json:"guardKnown"` // false=判不出来，**绝不把未知当成无口令**

	MtimeUnix int64 `json:"mtimeUnix"`

	// UsedBy 是主机清单里引用这把钥匙的别名。**这条存在的理由是防误删**：
	// 私钥文件被删而清单还指着它，后果是"那条永远连不上而人看不出为什么"。
	UsedBy []string `json:"usedBy"`
	// UsedByKnown 说清 UsedBy 到底是"判过、没人用"还是**根本没判**。
	//
	// 不能只靠"空数组"表达：引用判定要靠私钥路径做 SameFile 比对，所以
	// ① 孤儿 .pub 行（没有私钥可比）、② 清单读不动时，都只能得到空名单。
	// 若前端把它渲染成"清单里没有主机用它"，就是**把未判说成没人用**——而这句
	// 正是删除决策现场最常被引用的一句，等于给误删发通行证。
	UsedByKnown bool `json:"usedByKnown"`
}

// ListKeys 列出本机钥匙台账，并把清单引用关系叠上去。
//
// 目录不存在（尚无 ~/.ssh）是**正常空态**而非错误——新机器首次用就这样，
// 报成错误会让人以为程序坏了。真正的 IO 失败如实上抛，不回空表假装成功。
func (s *SshKeysService) ListKeys() ([]KeyEntry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	dir, err := sshHomeDir()
	if err != nil {
		return nil, err
	}
	entries, err := scanKeyDir(dir)
	if err != nil {
		return nil, err
	}
	s.annotateUsedBy(entries)
	return entries, nil
}

// annotateUsedBy 用 os.SameFile 比对，不用字符串相等：真身路径与用户写法的字面差异
// （8.3 短名、大小写、`..`）会让字符串比较漏判，而漏判正好会放过一次误删。
// 清单读不动时静默跳过——这一层是尽力而为的护栏，硬边界在 keyops 的围栏与确认门。
func (s *SshKeysService) annotateUsedBy(entries []KeyEntry) {
	hosts, err := s.ledger.list()
	if err != nil {
		// 清单读不动：整表都是"未判"。UsedByKnown 保持 false，前端就不许出现
		// "没有主机用它"这种放行式措辞（宁可少一句提示，不可给一次误删开路）。
		return
	}
	for i := range entries {
		e := &entries[i]
		e.UsedBy = []string{}
		if e.PrivatePath == "" {
			continue // 仅公钥的行没有可比对的私钥路径 → 未判
		}
		pfi, statErr := os.Stat(e.PrivatePath)
		if statErr != nil {
			continue // 私钥文件都 stat 不到 → 同样未判，不猜
		}
		e.UsedByKnown = true // 判过了：下面的名单（可能为空）到此才是事实
		for _, h := range hosts {
			p := expandHome(strings.TrimSpace(h.KeyPath))
			if p == "" {
				continue
			}
			if hfi, hErr := os.Stat(p); hErr == nil && os.SameFile(pfi, hfi) {
				e.UsedBy = append(e.UsedBy, h.Alias)
			}
		}
	}
}

// userHomeDir 是**家目录的唯一接缝**，包级变量便于单测整体替换。
//
// 为什么必须收成一处：台账扫描用的是 `sshHomeDir`，而"这把钥匙被哪台主机在用"
// 要把清单里的 `~/.ssh/id_x` 展开后比对——两处各调各的 os.UserHomeDir，测试里只
// 替换一个就会出现"扫到钥匙、展开到别处、引用判成无"的**假阴性**。而这个字段的
// 全部意义就是拦住误删，假阴性比没有它更危险（会让人以为"没人用，放心删"）。
var userHomeDir = os.UserHomeDir

// sshHomeDir 定位用户目录下的 .ssh。走 userHomeDir 而不是 $HOME——后者在部分
// Windows shell 下为空。这里只读，不创建目录。
var sshHomeDir = func() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("定位不到用户主目录，无法列出 SSH 密钥: %w", err)
	}
	return filepath.Join(home, ".ssh"), nil
}

// scanKeyDir 扫描目录并按钥匙归组，按名排序保证输出确定（前端列表不抖动）。
//
// **"是不是密钥"按内容判、不按文件名猜**：文件名前缀那种启发式一旦列不全就会静默漏
// 钥匙，而"漏一把"在密钥界面里是最糟的失败形态——人会以为本机没有它，转去重新生成，
// 从此两台机器两套凭据。
func scanKeyDir(dir string) ([]KeyEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []KeyEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", dir, err)
	}

	byName := map[string]*KeyEntry{}
	order := []string{}
	ensure := func(base string) *KeyEntry {
		e, ok := byName[base]
		if !ok {
			e = &KeyEntry{Name: base}
			byName[base] = e
			order = append(order, base)
		}
		return e
	}

	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		path := filepath.Join(dir, name)
		info, iErr := de.Info()
		if iErr != nil {
			continue // 单个文件取不到元数据不拖垮整张台账
		}

		if strings.HasSuffix(name, ".pub") {
			base := strings.TrimSuffix(name, ".pub")
			e := ensure(base)
			e.HasPublic = true
			e.PublicPath = path
			if info.ModTime().Unix() > e.MtimeUnix {
				e.MtimeUnix = info.ModTime().Unix()
			}
			raw, rErr := os.ReadFile(path)
			if rErr != nil {
				// 读不到仍保留这一条："有个公钥文件但打不开"本身是要看见的信息。
				continue
			}
			if pub, comment, ok := parsePubBytes(raw); ok {
				applyPublicKey(e, pub, comment)
			}
			continue
		}

		head, rErr := readHead(path, keyHeadBytes)
		if rErr != nil || !looksLikePrivateKey(head) {
			continue // 不是私钥（config/known_hosts/README 等）
		}
		e := ensure(name)
		e.HasPrivate = true
		e.PrivatePath = path
		if info.ModTime().Unix() > e.MtimeUnix {
			e.MtimeUnix = info.ModTime().Unix()
		}
		e.Guarded, e.GuardKnown = detectGuard(head)
		if e.Fingerprint == "" {
			// 没有 .pub（或 .pub 在前一轮没解析出来）时，只有确认未加密才从私钥算。
			if !e.Guarded && e.GuardKnown {
				if signer, sErr := ssh.ParsePrivateKey(head); sErr == nil {
					applyPublicKey(e, signer.PublicKey(), "")
				}
			}
		}
	}

	// .pub 先出现时，它的 type/fingerprint 已填；私钥后出现也不会覆盖（applyPublicKey
	// 只在空时填）。反序同理。最后统一按名排序输出。
	out := make([]KeyEntry, 0, len(order))
	for _, base := range order {
		e := *byName[base]
		e.UsedBy = []string{}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// applyPublicKey 用公钥对象补齐展示字段，**不覆盖已有值**（.pub 给的注释优先于
// 私钥侧算出来的空串；两条路径谁先到都不影响结果）。
func applyPublicKey(e *KeyEntry, pub ssh.PublicKey, comment string) {
	if pub == nil {
		return
	}
	if e.Type == "" {
		e.Type = pub.Type()
	}
	if e.Fingerprint == "" {
		e.Fingerprint = ssh.FingerprintSHA256(pub)
	}
	if c := strings.TrimSpace(comment); c != "" && e.Comment == "" {
		e.Comment = c
	}
	if e.Bits == 0 {
		e.Bits = keyBits(pub)
	}
}

// looksLikePrivateKey 判内容头部是否私钥（PEM 标记）。认不出就当不是，不误报。
func looksLikePrivateKey(head []byte) bool {
	return strings.Contains(string(head), "PRIVATE KEY-----")
}
