// Package main 离线冒烟探针：真实探测本机工具链并打印 JSON 结果。
// 用法：go run ./internal/modules/envcheck/probe_tmp
// 独立于 GUI 运行，用于快速排查探测逻辑问题（如 PATH 变更后消失的工具）。
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"hubkit/internal/modules/envcheck/detect"
)

func main() {
	rs := detect.RunAll(context.Background())
	for _, r := range rs {
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
	}
}