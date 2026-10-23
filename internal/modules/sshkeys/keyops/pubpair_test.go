package keyops

// 公钥补位与孤儿公钥删除的测试。重点验一条容易做假的事：
// **带口令的私钥也能算出公钥**（OpenSSH 新格式只加密私钥段，pubblob 在外层明文里），
// 而且这件事全程不接触口令、也不改私钥一个字节。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestExportPubFromPlainKey(t *testing.T) {
	dir := t.TempDir()
	fp := writeKeyFile(t, dir, "id_plain", "")

	res, err := ExportPub(dir, filepath.Join(dir, "id_plain"), "laptop-2026")
	if err != nil {
		t.Fatalf("补公钥失败: %v", err)
	}
	if res.Fingerprint != fp {
		t.Errorf("指纹应与密钥本体一致，实得 %q 期望 %q", res.Fingerprint, fp)
	}
	if res.Type != "ssh-ed25519" {
		t.Errorf("算法应为 ssh-ed25519，实得 %q", res.Type)
	}
	line := strings.TrimSpace(string(readFileBytes(t, res.PublicPath)))
	if !strings.HasPrefix(line, "ssh-ed25519 ") {
		t.Errorf("公钥行形态不对: %q", line)
	}
	if !strings.HasSuffix(line, " laptop-2026") {
		t.Errorf("注释应落在行尾: %q", line)
	}
	// 写出来的东西必须能被 ssh 认（不是只有我们自己读得懂的私有格式）。
	if _, _, _, _, pErr := ssh.ParseAuthorizedKey([]byte(line + "\n")); pErr != nil {
		t.Fatalf("补出的公钥 ssh 认不了: %v", pErr)
	}
	assertNoSecret(t, "ExportPub", res, nil, "PRIVATE KEY")
}

// TestExportPubWorksOnEncryptedKey 是这个功能成立的关键前提：需要口令的私钥
// 往往正是拿不到 .pub 的那批，若这里只能处理明文钥匙，按钮就形同虚设。
func TestExportPubWorksOnEncryptedKey(t *testing.T) {
	dir := t.TempDir()
	const pass = "correct-horse"
	fp := writeKeyFile(t, dir, "id_enc", pass)
	privPath := filepath.Join(dir, "id_enc")
	before := readFileBytes(t, privPath)
	mustPassphraseMissing(t, privPath) // 前置事实：x/crypto 确实解不动它

	res, err := ExportPub(dir, privPath, "")
	if err != nil {
		t.Fatalf("加密私钥应能补出公钥: %v", err)
	}
	if res.Fingerprint != fp {
		t.Errorf("指纹不符，实得 %q 期望 %q", res.Fingerprint, fp)
	}
	if string(before) != string(readFileBytes(t, privPath)) {
		t.Error("补公钥不得改动私钥文件")
	}
	// 加密私钥的注释在被加密的段里，取不到就**留空**，不拿文件名编一个。
	line := strings.TrimSpace(string(readFileBytes(t, res.PublicPath)))
	if strings.Count(line, " ") != 1 {
		t.Errorf("取不到注释时该行应只有算法与 base64 两段，实得 %q", line)
	}
	assertNoSecret(t, "ExportPub(encrypted)", res, nil, pass)
}

func TestExportPubRefusals(t *testing.T) {
	t.Run("已有 .pub 不覆盖", func(t *testing.T) {
		d := t.TempDir()
		writeKeyFile(t, d, "id_a", "")
		if _, err := ExportPub(d, filepath.Join(d, "id_a"), "c"); err != nil {
			t.Fatalf("首次补出应成功: %v", err)
		}
		if _, err := ExportPub(d, filepath.Join(d, "id_a"), "other"); !errors.Is(err, ErrExists) {
			t.Errorf("已存在应报 ErrExists，实得 %v", err)
		}
		if line := string(readFileBytes(t, filepath.Join(d, "id_a.pub"))); !strings.HasSuffix(strings.TrimSpace(line), " c") {
			t.Error("已存在的公钥不该被重写")
		}
	})

	t.Run("给它公钥路径应当拒", func(t *testing.T) {
		d := t.TempDir()
		writeKeyFile(t, d, "id_b", "")
		if _, err := ExportPub(d, filepath.Join(d, "id_b.pub"), ""); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("应拒 .pub 输入，实得 %v", err)
		}
	})

	t.Run("目录外的路径应当拒", func(t *testing.T) {
		out := t.TempDir()
		writeKeyFile(t, out, "id_out", "")
		inner := t.TempDir()
		if _, err := ExportPub(inner, filepath.Join(out, "id_out"), ""); !errors.Is(err, ErrOutsideDir) {
			t.Errorf("应拒目录外目标，实得 %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(out, "id_out.pub")); !errors.Is(statErr, os.ErrNotExist) {
			t.Error("被拒之后不该在目录外留下公钥")
		}
	})

	t.Run("非私钥内容不猜", func(t *testing.T) {
		d := t.TempDir()
		p := filepath.Join(d, "id_junk")
		if err := os.WriteFile(p, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n这不是base64!!\n-----END OPENSSH PRIVATE KEY-----\n"), 0o600); err != nil {
			t.Fatalf("写伪私钥失败: %v", err)
		}
		if _, err := ExportPub(d, p, ""); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("伪私钥应报 ErrInvalidInput，实得 %v", err)
		}
		if _, statErr := os.Stat(p + ".pub"); !errors.Is(statErr, os.ErrNotExist) {
			t.Error("失败不得留下半截公钥")
		}
	})
}

// TestExportPubWriteFailureLeavesNoFile 用包内故障注入接缝掐断写盘：
// 留一个 ssh 认不出的 .pub 比没有更糟（台账会显示"有配套公钥"而它是坏的）。
func TestExportPubWriteFailureLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "id_f", "")
	withFailingWrites(t, 1)
	if _, err := ExportPub(dir, filepath.Join(dir, "id_f"), "c"); err == nil {
		t.Fatal("写盘失败应报错")
	}
	for _, n := range listNames(t, dir) {
		if n != "id_f" {
			t.Errorf("失败后目录里多出残留 %q", n)
		}
	}
}

func TestDeletePubOnly(t *testing.T) {
	t.Run("孤儿公钥可删且要回显", func(t *testing.T) {
		dir := t.TempDir()
		writeKeyFile(t, dir, "id_g", "")
		if _, err := ExportPub(dir, filepath.Join(dir, "id_g"), "c"); err != nil {
			t.Fatalf("补公钥失败: %v", err)
		}
		if err := os.Remove(filepath.Join(dir, "id_g")); err != nil {
			t.Fatalf("删私钥制造孤儿失败: %v", err)
		}
		pubPath := filepath.Join(dir, "id_g.pub")
		// 先验"回显不符必须拒"，再验"回显相符才放行"——顺序反了会把文件删掉，
		// 后一条断言就变成永远成立的空炮。
		if _, err := DeletePubOnly(dir, pubPath, "id_g.pu"); !errors.Is(err, ErrConfirmMismatch) {
			t.Errorf("回显不符应拒，实得 %v", err)
		}
		if _, err := DeletePubOnly(dir, pubPath, "  "); !errors.Is(err, ErrConfirmMismatch) {
			t.Errorf("空白回显应拒，实得 %v", err)
		}
		if _, err := os.Stat(pubPath); err != nil {
			t.Fatalf("拒绝时不该动文件: %v", err)
		}
		res, err := DeletePubOnly(dir, pubPath, "id_g.pub")
		if err != nil {
			t.Fatalf("删除失败: %v", err)
		}
		if _, statErr := os.Stat(pubPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Error("应已删除")
		}
		if !strings.Contains(res.Message, "不会被回收") {
			t.Error("回执要如实说明远端授权不受影响")
		}
	})

	t.Run("有配套私钥时坚决拒绝", func(t *testing.T) {
		dir := t.TempDir()
		writeKeyFile(t, dir, "id_h", "")
		pubPath := filepath.Join(dir, "id_h.pub")
		if err := os.WriteFile(pubPath, []byte("ssh-ed25519 AAAA c\n"), 0o644); err != nil {
			t.Fatalf("写公钥失败: %v", err)
		}
		_, err := DeletePubOnly(dir, pubPath, "id_h.pub")
		if err == nil || !strings.Contains(err.Error(), "id_h") {
			t.Fatalf("应拒绝并点名是哪把私钥，实得 %v", err)
		}
		if _, statErr := os.Stat(pubPath); statErr != nil {
			t.Error("拒绝时不该删掉公钥")
		}
	})

	t.Run("私钥路径不该走这个入口", func(t *testing.T) {
		dir := t.TempDir()
		writeKeyFile(t, dir, "id_i", "")
		if _, err := DeletePubOnly(dir, filepath.Join(dir, "id_i"), "id_i"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("只接受 .pub，实得 %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "id_i")); statErr != nil {
			t.Error("拒绝时不该删私钥")
		}
	})
}
