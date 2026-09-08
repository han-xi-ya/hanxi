package wechat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
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

type inboundAttachment struct {
	AccountID string
	FileName  string
	FileSize  int64
	Media     InboundMedia
	Kind      string
}

type attachmentStore struct {
	mu            sync.RWMutex
	items         map[string]inboundAttachment
	outgoingTemps map[string]struct{}
}

func newAttachmentStore() *attachmentStore {
	return &attachmentStore{
		items:         make(map[string]inboundAttachment),
		outgoingTemps: make(map[string]struct{}),
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

	s.mu.Lock()
	s.items[id] = inboundAttachment{
		AccountID: accountID,
		FileName:  fileName,
		FileSize:  fileSize,
		Media:     media,
		Kind:      kind,
	}
	s.mu.Unlock()

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

func (s *attachmentStore) get(id string) (inboundAttachment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[strings.TrimSpace(id)]
	return item, ok
}

func (s *attachmentStore) deleteAccount(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, item := range s.items {
		if item.AccountID == accountID {
			delete(s.items, id)
		}
	}
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

func (s *attachmentStore) clear() {
	s.mu.Lock()
	temps := make([]string, 0, len(s.outgoingTemps))
	for path := range s.outgoingTemps {
		temps = append(temps, path)
	}
	s.items = make(map[string]inboundAttachment)
	s.outgoingTemps = make(map[string]struct{})
	s.mu.Unlock()
	for _, path := range temps {
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
