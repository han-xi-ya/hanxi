package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/logging"
	"hanxi/internal/modules/ocr"
)

// Recognizer 是 hanxi_ocr_recognize 的后端能力面（真 = *ocr.OcrService；单测注入假件）。
// 只此一个方法——对话框/服务启停等能力面刻意不出缝（headless 红线，PLAN_MCP §2.4）。
type Recognizer interface {
	RecognizeImage(path string) (ocr.OcrOutcome, error)
}

// maxImageFileBytes 输入图片体积闸门：对齐上游 hanxi-ocr 的 64MB 硬上限，
// 超限在 MCP 侧先行拒绝（C4 验收：绝对路径/尺寸上限校验，不外浪请求）。
const maxImageFileBytes = 64 << 20

// statFileFn 单测可替换的文件信息探针。
var statFileFn = os.Stat

// buildOcrTool 本地 hanxi-ocr 服务识图（只读：不写文件、不拉服务，PLAN_MCP §2.2 ocr 行）。
func buildOcrTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolOCR,
		mcp.WithDescription("用本机 hanxi-ocr 服务对一张本地图片做 OCR 文字识别（只读：不改写文件、"+
			"不代为启动服务——服务未运行时报错给指引）。path 必须是本机绝对路径且指向存在的图片文件，"+
			"上限 64MB。识别文本过敏感信息脱敏后返回（text/elapsedMs，超 1MB 截断置 truncated）；"+
			"识别结果可能包含图片内隐私内容，调用前用户应知晓该图片将进入云端模型上下文。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("本机图片绝对路径（如 D:\\shots\\err.png）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.OCR == nil {
			return mcp.NewToolResultError("ocr 后端未装配：请确认 hanxi 已启用「hanxi-ocr」模块"), nil
		}
		p, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError("缺少参数 path（图片绝对路径不能为空）"), nil
		}
		if !filepath.IsAbs(p) {
			return mcp.NewToolResultError("path 必须是本机绝对路径（无头进程不猜测相对路径的基准目录）"), nil
		}
		fi, err := statFileFn(p)
		if err != nil || fi.IsDir() {
			return mcp.NewToolResultError(fmt.Sprintf("图片不存在或不可访问: %s", p)), nil
		}
		if fi.Size() > maxImageFileBytes {
			return mcp.NewToolResultError(fmt.Sprintf("图片过大（%d 字节 > %d 上限），拒绝提交", fi.Size(), int64(maxImageFileBytes))), nil
		}
		outcome, err := deps.OCR.RecognizeImage(p)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("识图调用失败: %v", err)), nil
		}
		// service 契约：业务失败（服务离线/上游报错）折进 outcome.Error，中文指引。
		if !outcome.Ok {
			guidance := outcome.Error
			if guidance == "" {
				guidance = "识别失败（上游未给出原因）"
			}
			return mcp.NewToolResultError(guidance), nil
		}
		// 64KB 余量给 JSON 转义与信封字段，保证整包 ≤1MB 预算不再触发信封级二次截断。
		text, truncated := truncateUTF8(outcome.Text, maxPayloadBytes-64*1024)
		return textResult(resultPayload{
			"ok":        true,
			"text":      logging.Redact(text),
			"elapsedMs": outcome.ElapsedMs,
			"truncated": truncated,
		})
	}
	return tool, handler
}
