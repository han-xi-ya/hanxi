package detect

import "regexp"

// goGRPCVersionRe 匹配 `protoc-gen-go-grpc --version` 输出中的版本号。
// 样本：protoc-gen-go-grpc 1.5.1
var goGRPCVersionRe = regexp.MustCompile(`(?i)\bprotoc-gen-go-grpc\s+v?(\d+(?:\.\d+){1,2})\b`)

// goGRPCDetector 实现 Detector，探测 Go gRPC 的 protoc 代码生成插件。
// 注意：它不是 gRPC 运行库（运行时依赖由各项目的 go.mod 独立管理），MissingHint 专门澄清此点。
type goGRPCDetector struct{}

func (goGRPCDetector) Name() string          { return "protoc-gen-go-grpc" }
func (goGRPCDetector) Display() string       { return "gRPC for Go（代码生成插件）" }
func (goGRPCDetector) VersionArgs() []string { return []string{"--version"} }
func (goGRPCDetector) MissingHint() string {
	return "未在 PATH 中找到 protoc-gen-go-grpc。它是 Go gRPC 的 protobuf 服务代码生成插件，可执行 go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest 安装；项目运行时使用的 gRPC 库版本由各项目 go.mod 管理"
}
func (goGRPCDetector) Parse(out string) string {
	if m := goGRPCVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func init() { Register(goGRPCDetector{}) }
