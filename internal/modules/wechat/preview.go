package wechat

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxImagePreviewBytes 图片预览字节上限——Base64 后经 RPC 通道回传 WebView，
// 超大原图膨胀 4/3 倍会拖垮消息桥；超限放弃预览回退占位卡片，不阻断收发主流程。
const maxImagePreviewBytes int64 = 16 << 20

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
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("图片不存在或不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("目标不是普通文件")
	}
	if info.Size() > maxImagePreviewBytes {
		return "", fmt.Errorf("图片超过 %d MB，已跳过预览", maxImagePreviewBytes>>20)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取图片失败: %w", err)
	}
	return buildImageDataURL(data)
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
