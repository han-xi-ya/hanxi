package keyops

// DeletePair 的用例：回显确认、成对删除、孤儿公钥不算错误、围栏内外两侧。
// 这里每一条都额外盯一件事——**被拒的删除绝不能碰到目录外的文件**。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeletePairRemovesBoth(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_gone", Type: "ed25519", Passphrase: testPass1, Comment: "gone@test"})
	fp := res.Fingerprint

	got, err := DeletePair(dir, res.PrivatePath, "id_gone")
	if err != nil {
		t.Fatalf("正常删除失败: %v", err)
	}
	if fileExists(res.PrivatePath) || fileExists(res.PublicPath) {
		t.Errorf("两个文件都应消失：私钥在=%v 公钥在=%v", fileExists(res.PrivatePath), fileExists(res.PublicPath))
	}
	if len(listNames(t, dir)) != 0 {
		t.Errorf("删除后目录应空，实得 %v", listNames(t, dir))
	}
	// 回执：删掉的到底是哪一把，指纹得留下来（人在界面上要能核对"删的是它"）
	if got.Fingerprint != fp {
		t.Errorf("回执指纹应为 %s，实得 %s", fp, got.Fingerprint)
	}
	if !strings.Contains(got.Message, "id_gone") || !strings.Contains(got.Message, "id_gone.pub") {
		t.Errorf("回执文案应把两个文件都说清，实得 %q", got.Message)
	}
	if !strings.Contains(got.Message, fp) {
		t.Errorf("回执文案应含指纹，实得 %q", got.Message)
	}
	if got.PrivatePath == "" || got.PublicPath == "" {
		t.Errorf("回执应带上被删的两个路径，实得 %+v", got)
	}
	assertNoSecret(t, "删除成功", got, err, testPass1)

	// 删完再删一次：如实报不存在，而不是"删除成功"
	if _, err = DeletePair(dir, res.PrivatePath, "id_gone"); !errors.Is(err, ErrTargetMissing) {
		t.Errorf("重复删除应报目标不存在，实得: %v", err)
	}
}

func TestDeletePairWithoutPubIsNotError(t *testing.T) {
	// 安全设计第 6 条的另一半：只有私钥、没有公钥，是台账里的常见现状
	// （先前手动删过 .pub、或从别处拷来一把私钥），不该把删除卡住。
	dir := t.TempDir()
	fp := writeKeyFile(t, dir, "id_solo", "")

	got, err := DeletePair(dir, filepath.Join(dir, "id_solo"), "id_solo")
	if err != nil {
		t.Fatalf("配套公钥不存在不应算错误，实得: %v", err)
	}
	if fileExists(filepath.Join(dir, "id_solo")) {
		t.Error("私钥没删掉")
	}
	if !strings.Contains(got.Message, "本就不存在") {
		t.Errorf("文案应如实说明公钥本就不存在，实得 %q", got.Message)
	}
	if got.PublicPath != "" {
		t.Errorf("没动过公钥就不该回填公钥路径，实得 %q", got.PublicPath)
	}
	if got.Fingerprint != "" {
		t.Errorf("没有 .pub 时不该凭空给出指纹，实得 %q", got.Fingerprint)
	}
	_ = fp
}

func TestDeletePairConfirmGate(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_keep", Type: "ed25519"})
	before := readFileBytes(t, res.PrivatePath)

	cases := []struct {
		why     string
		confirm string
	}{
		{"空确认", ""},
		{"只给空格", "   "},
		{"确认名差一个字符", "id_kee"},
		{"确认了另一个文件", "id_keep.pub"},
		{"大小写不同的确认", "ID_KEEP"},
		{"带了目录前缀", filepath.Join(dir, "id_keep")},
		{"多打一个后缀", "id_keep "},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			got, err := DeletePair(dir, res.PrivatePath, c.confirm)
			if !errors.Is(err, ErrConfirmMismatch) {
				t.Fatalf("%s 应当拒绝，实得 got=%+v err=%v", c.why, got, err)
			}
			if !strings.Contains(err.Error(), "id_keep") {
				t.Errorf("拒绝文案应把期望的回显名说清楚，实得: %v", err)
			}
			// 每次拒绝之后，两个文件都必须在原地
			if string(readFileBytes(t, res.PrivatePath)) != string(before) || !fileExists(res.PublicPath) {
				t.Fatal("被拒的删除动到了文件")
			}
		})
	}
}

func TestDeletePairRejectsPubAndDirTargets(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_pair", Type: "ed25519"})
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatalf("建子目录失败: %v", err)
	}

	cases := []struct {
		why     string
		path    string
		confirm string
		want    error
	}{
		{"传公钥路径", res.PublicPath, "id_pair.pub", ErrInvalidInput},
		{"传目录", sub, "subdir", ErrNotRegularFile},
		{"传不存在的名字", filepath.Join(dir, "id_ghost"), "id_ghost", ErrTargetMissing},
		// 围栏根自己：形态判定（是目录，不是私钥文件）在越界判定之前先命中，
		// 两个哨兵都算拒得对，这里钉住的是"绝不静默成功"。
		{"传目录本身", dir, filepath.Base(dir), ErrNotRegularFile},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			got, err := DeletePair(dir, c.path, c.confirm)
			if err == nil || !errors.Is(err, c.want) {
				t.Fatalf("应报 %v，实得 got=%+v err=%v", c.want, got, err)
			}
			if !fileExists(res.PrivatePath) || !fileExists(res.PublicPath) {
				t.Fatal("无关的拒绝动作把既有钥匙对删掉了")
			}
		})
	}
}

// TestDeletePairRejectsOutsideDir 是全包最要紧的一条：path 来自前端/账本，
// 围栏一旦失效，这个 RPC 就是"任意文件删除"。断言分两半——
// 报什么错，以及**外面那个文件确实还在**。
func TestDeletePairRejectsOutsideDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	victims := []string{
		filepath.Join(outside, "id_victim"),
		filepath.Join(root, "id_root"),
		filepath.Join(outside, "..", "id_sibling"),
	}
	for _, v := range victims {
		writeKeyInto(t, v, "")
	}

	for _, victim := range victims {
		name := filepath.Base(victim)
		t.Run("拒绝_"+name, func(t *testing.T) {
			got, err := DeletePair(dir, victim, name)
			if !errors.Is(err, ErrOutsideDir) {
				t.Fatalf("应报越界，实得 got=%+v err=%v", got, err)
			}
			for _, v := range victims {
				if !fileExists(v) {
					t.Errorf("%s 被删掉了——杀伤半径失控", v)
				}
			}
			// 相对形式也要挡住：从 dir 出发用 .. 走出去
			rel := filepath.Join("..", "outside", name)
			if _, err = DeletePair(dir, filepath.Join(dir, rel), name); !errors.Is(err, ErrOutsideDir) {
				t.Errorf("相对形式 %q 应报越界，实得: %v", rel, err)
			}
		})
	}
}

// TestDeletePairDirArgumentEscape 验另一半：围栏根本身给了个带 `..` 的脏写法，
// 解析之后仍然指向同一个目录，那么合法路径照旧放行（不误拒），
// 而外部文件照旧拒绝（不漏放）。
func TestDeletePairDirArgumentEscape(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	inside := filepath.Join(dir, "id_in")
	writeKeyInto(t, inside, "")
	outside := filepath.Join(root, "id_out")
	writeKeyInto(t, outside, "")

	dirtyDir := filepath.Join(root, "keys", "..", "keys")
	if _, err := DeletePair(dirtyDir, outside, "id_out"); !errors.Is(err, ErrOutsideDir) {
		t.Errorf("脏围栏根之下，目录外目标仍应被拒，实得: %v", err)
	}
	if !fileExists(outside) {
		t.Fatal("目录外的钥匙被删了")
	}
	res, err := DeletePair(dirtyDir, inside, "id_in")
	if err != nil {
		t.Fatalf("脏围栏根不应误拒目录内的正当删除: %v", err)
	}
	if fileExists(inside) {
		t.Error("正当删除没删掉")
	}
	assertNoSecret(t, "脏围栏根", res, nil)
}

func TestDeletePairResultJSONIsDisplaySafe(t *testing.T) {
	// Result 会被整个塞进前端与日志。这里把删口令保护的钥匙后的回执序列化一遍，
	// 断言既搜不到口令，也搜不到私钥 PEM 标记。
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_json", Type: "ed25519", Passphrase: testPass1, Comment: "json@test"})
	got, err := DeletePair(dir, res.PrivatePath, "id_json")
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	b, jerr := json.Marshal(got)
	if jerr != nil {
		t.Fatalf("回执序列化失败: %v", jerr)
	}
	for _, frag := range []string{testPass1, "PRIVATE KEY", "openssh-key-v1"} {
		if strings.Contains(string(b), frag) {
			t.Errorf("回执 JSON 里出现了不该有的东西：%q", frag)
		}
	}
	// 公钥片段是**可以**出现的（本就可公开），这里只确认没有整段私钥
	if len(b) > 4096 {
		t.Errorf("回执异常大（%d 字节），疑似塞进了文件内容", len(b))
	}
}
