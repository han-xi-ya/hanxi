package wechat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type inboundAttachment struct {
	AccountID string
	FileName  string
	FileSize  int64
	Media     InboundMedia
}

type attachmentStore struct {
	mu    sync.RWMutex
	items map[string]inboundAttachment
}

func newAttachmentStore() *attachmentStore {
	return &attachmentStore{items: make(map[string]inboundAttachment)}
}

func (s *attachmentStore) register(accountID string, payload InboundFilePayload) (string, error) {
	fileName := sanitizeInboundFileName(payload.FileName)
	if strings.TrimSpace(payload.Media.EncryptQueryParam) == "" {
		return "", fmt.Errorf("文件消息缺少下载参数")
	}
	if strings.TrimSpace(payload.Media.AESKey) == "" {
		return "", fmt.Errorf("文件消息缺少解密密钥")
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
		FileSize:  int64(payload.Len),
		Media:     payload.Media,
	}
	s.mu.Unlock()

	// 只记录字段特征，绝不输出下载参数或 AES 密钥原文。
	slog.Info("wechat inbound file registered",
		"attachmentId", id,
		"accountId", accountID,
		"fileName", fileName,
		"fileSize", int64(payload.Len),
		"encryptType", payload.Media.EncryptType,
		"queryLength", len(payload.Media.EncryptQueryParam),
		"aesKeyLength", len(payload.Media.AESKey),
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

func (s *attachmentStore) clear() {
	s.mu.Lock()
	s.items = make(map[string]inboundAttachment)
	s.mu.Unlock()
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
