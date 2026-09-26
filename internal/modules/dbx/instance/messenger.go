package instance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 DBX 实例充当单实例协议的"信使"：
// tauri-plugin-single-instance 捕获第二实例后，经隐藏消息窗（类 -sic / 名
// -siw）把首次启动参数转发给主实例，其回调无条件 show+focus 主窗口——
// 这就是"打开窗口"的可靠实现（DBX 无任何唤窗 CLI 参数，插件协议是唯一通道）。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 信使转发完毕即自退（主实例挂死场会接管成为新主实例，与上游单实例语义
//     一致）——刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗链路将被拖慢；
//   - 同样刻意不设 HideWindow 等窗口干预：DBX 是 GUI 子系统程序，
//     既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）；
//   - DBX_DATA_DIR 同源注入：正常场信使转发即退、env 无人消费；接管场
//     （信使原地转正为主实例）数据改道纪律不落空——这是"托管启动必注入"
//     裁决在唤窗链路上的闭合，勿删。
func spawnMessenger(exe, dataDir string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if dataDir != "" {
		cmd.Env = append(os.Environ(), EnvDataDirKey+"="+dataDir)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
