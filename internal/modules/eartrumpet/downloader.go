package eartrumpet

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"hanxi/packages/go/netx"
)

// sharedClient 共享出网客户端（N25 收口）：走 netx 代理链；15 分钟为整请求
// 硬顶护栏（此前无 Timeout——挂死的连接只能靠调用方 context 收，属双重不合
// 规家族唯一漏网）；短操作的超时仍由调用方 context 精细控制，双层不互扰。
var sharedClient = netx.NewClient(15*time.Minute, nil)

// httpGet 拉取小体积文本资源（appinstaller 清单 / winget yaml）。
func httpGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := sharedClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, remoteFetchLimit))
}

// httpSave 流式下载大文件到目标路径。
func httpSave(ctx context.Context, rawURL, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := sharedClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
