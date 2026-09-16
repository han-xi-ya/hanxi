package ocr

// ---------- 图片工具函数（纯函数，可单测） ----------

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

var imgExtOK = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".bmp": true,
	".webp": true, ".gif": true, ".tif": true, ".tiff": true,
}

func mimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".tif", ".tiff":
		return "image/tiff"
	}
	return "application/octet-stream"
}

func imageExtForMIME(mime string) (string, bool) {
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
	case "image/tiff":
		return ".tif", true
	}
	return "", false
}

// decodeDataURL 解析 "data:<mime>;base64,<payload>"。
func decodeDataURL(u string) ([]byte, string, error) {
	if !strings.HasPrefix(u, "data:") {
		return nil, "", fmt.Errorf("不是有效的 Data URL")
	}
	comma := strings.IndexByte(u, ',')
	if comma < 0 {
		return nil, "", fmt.Errorf("Data URL 缺少数据分隔符")
	}
	head := u[len("data:"):comma]
	payload := u[comma+1:]
	mime := head
	if i := strings.LastIndex(head, ";"); i >= 0 {
		mime = head[:i]
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", fmt.Errorf("Data URL 解码失败: %w", err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("图片内容为空")
	}
	return data, mime, nil
}

func buildDataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// sanitizeImageName 只保留文件名成分并去控制字符（中文名保留）。
func sanitizeImageName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if len(name) > 48 {
		name = name[:48]
	}
	return strings.TrimSpace(name)
}
