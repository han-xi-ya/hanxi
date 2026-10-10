package wechat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 附件类别：注册来源决定预览 RPC 的信任边界——入站图片消息无文件名与长度字段，
// 注册时合成图片名（kindImage），PreviewInboundImage 仅放行该类别，杜绝借图片预览
// 通道下载解密任意大文件。
const (
	kindFile  = "file"
	kindImage = "image"
)

// 入站附件留存边界（堵注册表只增不减与明文散 Temp 两条常驻泄露）：
//   - attachmentTTL：注册项与其名下受管明文副本在入站 24h 后回收。微信媒体下载参数
//     本身有时效，留着死凭据不如如实回收——"附件不存在或已过期"话术自此有了真实机制。
//   - attachmentMaxItems：注册项条数上限。每条入站文件/图片消息注册一条（含
//     EncryptQueryParam 长串），高频刷屏在 24h 内也能堆出海量条目；超限按注册时刻
//     惰性淘汰最旧，常驻进程内存有界。
//   - maxOpenedFiles：受管"打开"目录中明文副本的上限。同一附件重复打开幂等复用，
//     正常用量远达不到；超限按落盘时刻淘汰最旧明文（注册项不动，下次打开重新下载）。
const (
	attachmentTTL      = 24 * time.Hour
	attachmentMaxItems = 2000
	maxOpenedFiles     = 16
)

type inboundAttachment struct {
	AccountID    string
	FileName     string
	FileSize     int64
	Media        InboundMedia
	Kind         string
	registeredAt time.Time
}

// openedFile 入站附件"打开"用受管明文副本：路径固定 <openDir>/<attachmentID>-<FileName>，
// 前缀附件 ID 杜绝同名撞车，保留原始扩展名以维持 Windows 关联程序打开语义。
type openedFile struct {
	path      string
	stampedAt time.Time
}

type attachmentStore struct {
	mu            sync.RWMutex
	items         map[string]inboundAttachment
	outgoingTemps map[string]struct{}
	// openDir 受管"打开"目录：首次落盘时惰性创建，进程内唯一。历史版本每次
	// OpenInboundFile 直接 os.MkdirTemp 且成功路径既不登记也不清理，每点一次
	// "打开"就在 %TEMP% 永久留一份聊天明文；现收口进本单一目录，明文副本与
	// 注册项同生命周期（TTL 过期/上限淘汰/deleteAccount/clear 均随项回收），
	// 目录本身在 clear（服务销毁）时整目录 RemoveAll。
	openDir string
	opened  map[string]openedFile
	// now 包内可注入时钟：TTL 与淘汰时序均由它推演，测试用假钟而不靠 sleep。
	now func() time.Time
}

func newAttachmentStore() *attachmentStore {
	return &attachmentStore{
		items:         make(map[string]inboundAttachment),
		outgoingTemps: make(map[string]struct{}),
		opened:        make(map[string]openedFile),
		now:           time.Now,
	}
}

func (s *attachmentStore) register(accountID string, payload InboundFilePayload) (string, error) {
	return s.registerMedia(accountID, sanitizeInboundFileName(payload.FileName), int64(payload.Len), payload.Media, kindFile)
}

// registerImage 注册入站图片附件：图片消息不携带文件名与长度，
// 统一使用调用方合成的命名并标记 kindImage。
func (s *attachmentStore) registerImage(accountID, fileName string, media InboundMedia) (string, error) {
	return s.registerMedia(accountID, fileName, 0, media, kindImage)
}

func (s *attachmentStore) registerMedia(accountID, fileName string, fileSize int64, media InboundMedia, kind string) (string, error) {
	if strings.TrimSpace(media.EncryptQueryParam) == "" {
		return "", fmt.Errorf("媒体消息缺少下载参数")
	}
	if strings.TrimSpace(media.AESKey) == "" {
		return "", fmt.Errorf("媒体消息缺少解密密钥")
	}

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return "", fmt.Errorf("生成附件 ID 失败: %w", err)
	}
	id := hex.EncodeToString(idBytes)

	now := s.now()
	s.mu.Lock()
	s.items[id] = inboundAttachment{
		AccountID:    accountID,
		FileName:     fileName,
		FileSize:     fileSize,
		Media:        media,
		Kind:         kind,
		registeredAt: now,
	}
	removals := s.pruneLocked(now)
	s.mu.Unlock()
	removeFiles(removals)

	// 只记录字段特征，绝不输出下载参数或 AES 密钥原文。
	slog.Info("wechat inbound attachment registered",
		"attachmentId", id,
		"accountId", accountID,
		"kind", kind,
		"fileName", fileName,
		"fileSize", fileSize,
		"encryptType", media.EncryptType,
		"queryLength", len(media.EncryptQueryParam),
		"aesKeyLength", len(media.AESKey),
	)
	return id, nil
}

// get 查询注册项。命中但已过 TTL 时按"不存在"口径返回并即刻惰性回收
// （含名下明文副本），不再等下一条注册顺带清扫：过期要么如实成立，要么别说。
func (s *attachmentStore) get(id string) (inboundAttachment, bool) {
	key := strings.TrimSpace(id)

	s.mu.RLock()
	item, ok := s.items[key]
	expired := ok && s.now().Sub(item.registeredAt) > attachmentTTL
	s.mu.RUnlock()

	if !ok {
		return inboundAttachment{}, false
	}
	if expired {
		s.drop(key)
		return inboundAttachment{}, false
	}
	return item, true
}

// deleteAccount 回收指定账号名下注册项及其受管明文副本。
func (s *attachmentStore) deleteAccount(accountID string) {
	s.mu.Lock()
	var removals []string
	for id, item := range s.items {
		if item.AccountID == accountID {
			removals = append(removals, s.dropLocked(id)...)
		}
	}
	s.mu.Unlock()
	removeFiles(removals)
}

// pruneLocked 注册后捎带的惰性清扫（须持写锁）：先剔 TTL 过期项，再压条数上限
// （按注册时刻淘汰最旧）。返回需删除的文件路径，由调用方锁外删除——与
// releaseOutgoingTemp/clear 同一锁纪律：绝不在持锁期间做文件 IO。
func (s *attachmentStore) pruneLocked(now time.Time) []string {
	var removals []string
	for id, item := range s.items {
		if now.Sub(item.registeredAt) <= attachmentTTL {
			continue
		}
		removals = append(removals, s.dropLocked(id)...)
	}
	for len(s.items) > attachmentMaxItems {
		oldestID := ""
		var oldestAt time.Time
		for id, item := range s.items {
			if oldestID == "" || item.registeredAt.Before(oldestAt) {
				oldestID, oldestAt = id, item.registeredAt
			}
		}
		removals = append(removals, s.dropLocked(oldestID)...)
	}
	return removals
}

// drop 回收单个注册项及其名下受管明文副本。
func (s *attachmentStore) drop(attachmentID string) {
	s.mu.Lock()
	removals := s.dropLocked(attachmentID)
	s.mu.Unlock()
	removeFiles(removals)
}

// dropLocked 须持写锁：删注册项并登记其明文副本为待删除。
func (s *attachmentStore) dropLocked(attachmentID string) []string {
	var removals []string
	delete(s.items, attachmentID)
	if of, ok := s.opened[attachmentID]; ok {
		removals = append(removals, of.path)
		delete(s.opened, attachmentID)
	}
	return removals
}

// openedPath 返回某附件当前的受管明文副本路径。条目缺失或文件已不在盘
// （被淘汰/系统 Temp 清理/用户手动删除）时一律报"无"——绝不把不存在的回执路径
// 交给调用方；调用方据此回到下载路径重新补齐。
func (s *attachmentStore) openedPath(attachmentID string) (string, bool) {
	s.mu.RLock()
	of, ok := s.opened[attachmentID]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	if info, err := os.Stat(of.path); err != nil || !info.Mode().IsRegular() {
		s.mu.Lock()
		if cur, exists := s.opened[attachmentID]; exists && cur.path == of.path {
			delete(s.opened, attachmentID)
		}
		s.mu.Unlock()
		return "", false
	}
	return of.path, true
}

// stageOpenedFile 把"打开"用明文写入受管目录并登记副本生命周期（随注册项被
// TTL/上限/deleteAccount/clear 回收）。同一附件重复落盘时覆盖旧条目并删除旧文件。
// attachmentID 必须是本 store 签发的 hex ID（get 命中后才可能走到这里），
// 拼入路径无穿越面；文件名再洗一遍属纵深防御。磁盘 IO 一律在锁外。
func (s *attachmentStore) stageOpenedFile(attachmentID, fileName string, data []byte) (string, error) {
	s.mu.Lock()
	dir, err := s.ensureOpenDirLocked()
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	target := filepath.Join(dir, attachmentID+"-"+sanitizeInboundFileName(fileName))
	var removals []string
	if old, ok := s.opened[attachmentID]; ok && old.path != target {
		removals = append(removals, old.path)
	}
	s.mu.Unlock()

	if err := writeFileAtomically(target, data); err != nil {
		return "", err
	}

	s.mu.Lock()
	s.opened[attachmentID] = openedFile{path: target, stampedAt: s.now()}
	removals = append(removals, s.pruneOpenedLocked(attachmentID)...)
	s.mu.Unlock()
	removeFiles(removals)
	return target, nil
}

// ensureOpenDirLocked 惰性创建进程内唯一受管"打开"目录（须持写锁）。
// 单次 mkdir 是本包"锁内不做 IO"纪律的唯一例外：并发首开必须只产出一个目录，
// 而 os.MkdirTemp 本身原子且极快。
func (s *attachmentStore) ensureOpenDirLocked() (string, error) {
	if s.openDir != "" {
		return s.openDir, nil
	}
	dir, err := os.MkdirTemp("", "hanxi-wechat-open-")
	if err != nil {
		return "", fmt.Errorf("创建受管打开目录失败: %w", err)
	}
	s.openDir = dir
	return dir, nil
}

// pruneOpenedLocked 须持写锁：明文副本超上限时按落盘时刻淘汰最旧；
// 刚写入的 keepID 免于淘汰（假钟同刻也不自删）。
func (s *attachmentStore) pruneOpenedLocked(keepID string) []string {
	var removals []string
	for len(s.opened) > maxOpenedFiles {
		oldestID := ""
		var oldestAt time.Time
		for id, of := range s.opened {
			if id == keepID {
				continue
			}
			if oldestID == "" || of.stampedAt.Before(oldestAt) {
				oldestID, oldestAt = id, of.stampedAt
			}
		}
		if oldestID == "" {
			break
		}
		removals = append(removals, s.opened[oldestID].path)
		delete(s.opened, oldestID)
	}
	return removals
}

func (s *attachmentStore) registerOutgoingTemp(path string) {
	s.mu.Lock()
	s.outgoingTemps[path] = struct{}{}
	s.mu.Unlock()
}

func (s *attachmentStore) releaseOutgoingTemp(path string) bool {
	s.mu.Lock()
	if _, ok := s.outgoingTemps[path]; !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.outgoingTemps, path)
	s.mu.Unlock()
	_ = os.Remove(path)
	return true
}

// clear 服务销毁时全量回收：出站临时文件逐个删，受管"打开"目录整目录
// RemoveAll（其中全部入站明文副本随目录消失），注册表清零。
func (s *attachmentStore) clear() {
	s.mu.Lock()
	temps := make([]string, 0, len(s.outgoingTemps))
	for path := range s.outgoingTemps {
		temps = append(temps, path)
	}
	dir := s.openDir
	s.items = make(map[string]inboundAttachment)
	s.outgoingTemps = make(map[string]struct{})
	s.opened = make(map[string]openedFile)
	s.openDir = ""
	s.mu.Unlock()
	for _, path := range temps {
		_ = os.Remove(path)
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// removeFiles 批量回收受管文件，错误一律忽略：每个路径都由本 store 登记、属主唯一，
// 删除失败只意味着它已不在盘（系统 Temp 清理/用户手动删除），无补偿可做。
func removeFiles(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

// inboundImageFileName 为入站图片合成文件名：微信图片消息只携带媒体凭据、
// 无原始文件名与长度，统一按入站时刻命名（与微信本地保存命名习惯对齐）。
func inboundImageFileName(t time.Time) string {
	return fmt.Sprintf("微信图片_%s.jpg", t.Format("20060102_150405"))
}

func sanitizeInboundFileName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "微信文件"
	}
	return name
}
