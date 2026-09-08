package wechat

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// 发送前必须可完整预览/校验图片；普通附件受现有整文件读入并加密实现约束，限制为 100 MB。
const (
	maxImagePreviewBytes       int64 = 16 << 20
	maxOutgoingAttachmentBytes int64 = 100 << 20
)

// imagePreviewExtensions 可预览/可"打开图片"的扩展名白名单（与 PickImageDialog 过滤器对齐）。
// "打开图片" RPC 借本白名单杜绝被借道执行任意本地文件（explorer/rundll32 文件语义教训）。
var imagePreviewExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
	".webp": true,
}

func isPreviewableImageName(name string) bool {
	return imagePreviewExtensions[strings.ToLower(filepath.Ext(name))]
}

// localImagePreview 校验并读取本地图片，返回可直接挂进 <img src> 的 Base64 Data URL。
// 出站图片气泡的内嵌缩略预览经这条路取字节——WebView 无法直读 file:// 本地路径。
func localImagePreview(filePath string) (string, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return "", fmt.Errorf("图片路径不能为空")
	}
	if !isPreviewableImageName(filePath) {
		return "", fmt.Errorf("不支持预览该类型文件")
	}
	return localImagePreviewByContent(filePath)
}

func localImagePreviewByContent(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("图片不存在或不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("目标不是普通文件")
	}
	if info.Size() > maxImagePreviewBytes {
		return "", fmt.Errorf("图片超过 %d MB，无法发送前预览", maxImagePreviewBytes>>20)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取图片失败: %w", err)
	}
	return buildImageDataURL(data)
}

func inspectOutgoingAttachment(filePath string) (OutgoingAttachmentDraft, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return OutgoingAttachmentDraft{}, fmt.Errorf("附件路径不能为空")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return OutgoingAttachmentDraft{}, fmt.Errorf("附件不存在或不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return OutgoingAttachmentDraft{}, fmt.Errorf("目标不是普通文件")
	}
	if info.Size() == 0 {
		return OutgoingAttachmentDraft{}, fmt.Errorf("附件内容为空")
	}
	if info.Size() > maxOutgoingAttachmentBytes {
		return OutgoingAttachmentDraft{}, fmt.Errorf("附件超过 %d MB，当前加密上传方式不支持", maxOutgoingAttachmentBytes>>20)
	}

	draft := OutgoingAttachmentDraft{
		Path:     filePath,
		FileName: filepath.Base(filePath),
		FileSize: info.Size(),
	}
	file, err := os.Open(filePath)
	if err != nil {
		return OutgoingAttachmentDraft{}, fmt.Errorf("读取附件失败: %w", err)
	}
	header := make([]byte, 512)
	n, readErr := file.Read(header)
	file.Close()
	if readErr != nil && n == 0 {
		return OutgoingAttachmentDraft{}, fmt.Errorf("读取附件失败: %w", readErr)
	}
	mime := http.DetectContentType(header[:n])
	if strings.HasPrefix(mime, "image/") {
		if info.Size() > maxImagePreviewBytes {
			return OutgoingAttachmentDraft{}, fmt.Errorf("图片超过 %d MB，无法发送前预览", maxImagePreviewBytes>>20)
		}
		preview, previewErr := localImagePreviewByContent(filePath)
		if previewErr != nil {
			return OutgoingAttachmentDraft{}, previewErr
		}
		draft.IsImage = true
		draft.PreviewURL = preview
	} else if isPreviewableImageName(filePath) {
		return OutgoingAttachmentDraft{}, fmt.Errorf("文件扩展名为图片，但内容不是支持的图片格式 (%s)", mime)
	}
	return draft, nil
}

func decodeClipboardDataURL(dataURL string) ([]byte, string, error) {
	const marker = ";base64,"
	comma := strings.Index(dataURL, marker)
	if comma < len("data:") || !strings.HasPrefix(dataURL, "data:") {
		return nil, "", fmt.Errorf("剪贴板附件数据格式无效")
	}
	data, err := base64.StdEncoding.DecodeString(dataURL[comma+len(marker):])
	if err != nil {
		return nil, "", fmt.Errorf("解析剪贴板附件失败: %w", err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("剪贴板附件内容为空")
	}
	return data, http.DetectContentType(data), nil
}

func imageExtensionForMIME(mime string) (string, bool) {
	switch mime {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/bmp":
		return ".bmp", true
	default:
		return "", false
	}
}

func decodeClipboardImageDataURL(dataURL string) ([]byte, string, error) {
	data, mime, err := decodeClipboardDataURL(dataURL)
	if err != nil {
		return nil, "", err
	}
	ext, ok := imageExtensionForMIME(mime)
	if !ok {
		return nil, "", fmt.Errorf("剪贴板内容不是支持的图片格式 (%s)", mime)
	}
	return data, ext, nil
}

// buildImageDataURL 按魔数嗅探真实图片类型构造 Data URL：
// 嗅探而非信扩展名，伪装成 .png 的非图片内容进不了 <img>。
func buildImageDataURL(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("图片内容为空")
	}
	switch mime := http.DetectContentType(data); mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	default:
		return "", fmt.Errorf("内容不是可预览的图片格式 (%s)", mime)
	}
}
