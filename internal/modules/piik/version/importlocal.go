package version

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
)

// ImportLocal 导入本地已解压的 Piik 安装（整套迁移，quicklook/everything 形制）。
// 探测锚点为主 exe piik-app.exe；与 ccswitch/DBX 的单 exe/白名单导入不同：
// Piik 的 runtime 双 exe 按 exe 所在目录兄弟路径相对解析，目录结构不可拆，
// 只搬主 exe 等于搬个必炸的半成品——故整套迁移（含许可文本与 runtime 子树；
// 用户就地产生的其他文件一概不碰不搬，托管运行数据由 instance 引擎经
// --config/--log-dir 改道托管目录，与源目录无涉）。
// 源必须满足与下载链同一布局不变式（checkPiikLayout 门口即拒，宁可导入
// 失败也不收进残缺安装）。版本探测：主 exe PE FileVersion，非 x.y.z 形状
// 或探测失败（非 Windows/资源缺失）时时间戳兜底，与家族 ImportLocal 同构。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (PiikVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return PiikVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}
	if err := checkPiikLayout(srcDir); err != nil {
		return PiikVersionInfo{}, fmt.Errorf("源目录不是完整的 Piik 便携安装：%w", err)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc/DBX ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return PiikVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	stagingID := "import-" + version + "-" + time.Now().Format("150405")
	staging, discard, err := m.tree.StageDir(stagingID)
	if err != nil {
		return PiikVersionInfo{}, err
	}
	defer discard()

	if err := copyTree(srcDir, staging); err != nil {
		return PiikVersionInfo{}, err
	}
	// 落位前复核（staging 形状即最终形状，防御拷贝半途结构异常）
	if err := checkPiikLayout(staging); err != nil {
		return PiikVersionInfo{}, err
	}

	// 主 exe 实测摘要入账（漂移护栏锚点）；导入链无官方摘要参照，
	// verifiedHash 如实记 false。REVISION 存在则同步记账。
	assetSHA, err := fileSHA256(filepath.Join(staging, exeName))
	if err != nil {
		return PiikVersionInfo{}, fmt.Errorf("计算导入摘要失败: %w", err)
	}
	if err := writeModuleMeta(staging, moduleMeta{
		IsImport: true,
		Source:   srcDir,
		Copied:   "<整个目录（平铺根 + runtime 子树）>",
		Revision: readRevision(staging),
	}); err != nil {
		return PiikVersionInfo{}, err
	}
	meta := artifact.Meta{Entry: exeName, AssetSHA256: assetSHA, Source: artifact.SourceImported}
	if err := m.tree.Commit(staging, version, meta); err != nil {
		return PiikVersionInfo{}, err
	}

	return PiikVersionInfo{
		Version:      "v" + version,
		ExePath:      filepath.Join(targetDir, exeName),
		Dir:          targetDir,
		Size:         dirSize(targetDir, fi.Size()),
		InstalledAt:  time.Now().Format("2006-01-02 15:04:05"),
		IsImport:     true,
		Source:       srcDir,
		SHA256:       assetSHA,
		VerifiedHash: false,
		Revision:     readRevision(targetDir),
	}, nil
}

// copyTree 把 src 目录内容整体复制到 dst（保留相对子目录结构；拒绝符号
// 链接/重解析点防环，everything/quicklook 导入链同纪律）。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == src {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, ierr := d.Info()
		if ierr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil // 符号链接不跟（防环/防越界）
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFileTo(p, target)
	})
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
