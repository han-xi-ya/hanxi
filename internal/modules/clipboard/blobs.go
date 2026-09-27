package clipboard

// A6 存储底座(一):内容寻址图片库。契约见 docs/plans/2026-09-26-clipboard-contract.md
// §2(数据布局)与 §10(冻结签名)。目录形态 <DataDir>/clipboard/blobs,平铺无子目录,
// 文件名即 <sha256(PNG 字节)>.png。与 Entry/A2 零耦合:只收 image.Image、只回 rel。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// blobNameRe rel 白名单:64 位小写 hex + ".png",整串锚定。
// 白名单而非黑名单——`[^\\/.]` 之类的排除式挡不住 C:\、URL 编码、控制字符等
// 花样输入;能匹配本正则的字符串不含任何路径分隔语义,filepath.Join 后必然
// 落在 dir 内,穿越从根上不可能。大写 hex、缺 .png、多段路径一律拒。
var blobNameRe = regexp.MustCompile(`^[0-9a-f]{64}\.png$`)

// BlobStore 内容寻址 PNG 库。dir 由调用方给定(<DataDir>/clipboard/blobs),
// 懒建目录:NewBlobStore 不碰磁盘,首次 SaveImage 才 MkdirAll。
//
// 并发安全设计(多路 Save/Delete/TotalBytes 同时调用成立):
//  1. PNG 编码与 sha256 为纯函数,不在锁内,不互相干扰;
//  2. "存在则跳过 → 写临时文件 → rename"整段由 mu 串行化,同内容的并发 Save
//     只有一个真正落盘,其余走幂等早退,不留多余临时文件;
//  3. 落盘只经原子 rename(与 jsonstore/memo 同谱),读侧在最终名上永远只会
//     看到完整文件,不存在半截 PNG;
//  4. Load/Delete 不加锁:Load 依赖上一条(内容寻址意味着"名字定则字节定");
//     Delete 与 Save 竞态最坏情形是"已删 blob 被并发在途的同内容 Save 复活",
//     不产生损坏,空间回收本就是调用方(A2 淘汰)的职责,可接受。
type BlobStore struct {
	dir string
	mu  sync.Mutex // 仅护"查重+临时文件+rename"与 TotalBytes 扫描,不含编码阶段
}

// NewBlobStore 绑定 blobs 目录,不建目录(懒)。
func NewBlobStore(dir string) *BlobStore {
	return &BlobStore{dir: dir}
}

// SaveImage 编码 PNG → sha256 命名 → 原子落盘,返回 rel(纯文件名)、宽高。
// 同图同 hash:幂等——已存在则不重写、不报错,直接返回同一 rel。
func (b *BlobStore) SaveImage(img image.Image) (string, int, int, error) {
	if img == nil {
		return "", 0, 0, errors.New("clipboard/blobs: 图片不能为 nil")
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// 编码在锁外:CPU 密集且与磁盘状态无关。
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", 0, 0, fmt.Errorf("clipboard/blobs: PNG 编码失败: %w", err)
	}
	data := buf.Bytes()
	rel := blobName(data)

	b.mu.Lock()
	defer b.mu.Unlock()

	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return "", 0, 0, fmt.Errorf("clipboard/blobs: 建目录失败: %w", err)
	}
	path := filepath.Join(b.dir, rel)
	if _, err := os.Stat(path); err == nil {
		return rel, width, height, nil // 幂等早退:内容寻址,存在即同字节
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", 0, 0, fmt.Errorf("clipboard/blobs: 探测 %s 失败: %w", rel, err)
	}
	if err := writeBlobAtomic(path, data); err != nil {
		return "", 0, 0, fmt.Errorf("clipboard/blobs: 落盘 %s 失败: %w", rel, err)
	}
	return rel, width, height, nil
}

// Load 按 rel 读回 PNG 原始字节。rel 必经白名单硬闸(见 blobNameRe),
// 不合法输入在触盘前即拒,绝不参与路径拼接。
func (b *BlobStore) Load(rel string) ([]byte, error) {
	path, err := b.safePath(rel)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("clipboard/blobs: 读取 %s 失败: %w", rel, err)
	}
	return data, nil
}

// Delete 删除 blob;不存在 = 幂等成功(nil)。同样先过白名单硬闸。
func (b *BlobStore) Delete(rel string) error {
	path, err := b.safePath(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clipboard/blobs: 删除 %s 失败: %w", rel, err)
	}
	return nil
}

// TotalBytes blobs 目录内合规 blob 的字节总量。目录未建(懒)视作 0。
// 子目录与非 <hex64>.png 形态的条目(残留临时文件、人为投放)忽略但计入 warn,
// 不报错——账目只认真 blob,脏东西留给收口/淘汰逻辑处置。
func (b *BlobStore) TotalBytes() (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entries, err := os.ReadDir(b.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("clipboard/blobs: 扫描目录失败: %w", err)
	}
	var total int64
	var ignored []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !blobNameRe.MatchString(name) {
			ignored = append(ignored, name)
			continue
		}
		info, err := e.Info()
		if err != nil {
			return 0, fmt.Errorf("clipboard/blobs: 取 %s 元数据失败: %w", name, err)
		}
		total += info.Size()
	}
	if len(ignored) > 0 {
		slog.Warn("clipboard/blobs: 目录内存在非内容寻址条目,已忽略",
			"dir", b.dir, "count", len(ignored), "sample", blobSample(ignored, 5))
	}
	return total, nil
}

// safePath 白名单硬闸 + 拼路径。所有对外接受 rel 的入口必须先行经过它。
func (b *BlobStore) safePath(rel string) (string, error) {
	if !blobNameRe.MatchString(rel) {
		return "", fmt.Errorf("clipboard/blobs: 非法 blob 路径 %q(仅接受 64 位小写 hex + .png 的文件名)", redactRel(rel))
	}
	return filepath.Join(b.dir, rel), nil
}

// blobName 内容寻址命名:sha256 十六进制 + .png。
func blobName(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + ".png"
}

// writeBlobAtomic 临时文件 + fsync + rename 原子落盘(memo/jsonstore 同谱)。
// Windows 上 rename 撞"目标已被外部占用"会失败;此时目标必然已是同 hash 字节,
// 按幂等成功吞掉,仅清理临时文件。
func writeBlobAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".blob-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		if _, serr := os.Stat(path); serr == nil {
			cleanup() // 目标已在:内容寻址保证同字节,视作并发落子成功
			return nil
		}
		cleanup()
		return err
	}
	return nil
}

// blobSample 取前 n 个名字用于日志(防超长刷屏)。
func blobSample(names []string, n int) []string {
	if len(names) <= n {
		return names
	}
	return names[:n]
}

// redactRel 报错回显里掐掉超长输入(白名单外的怪输入可能带大段内容)。
func redactRel(rel string) string {
	const max = 80
	if len(rel) <= max {
		return rel
	}
	return rel[:max] + "…"
}
