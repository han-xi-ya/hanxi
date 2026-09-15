# WSL 模块改造进度存档

最近更新：2026-09-14
状态：**改造已大规模落地**（本文首版存档于 09-08，当时"调研中断、未动代码"的状态早已被后续提交超越；本版为收敛后的实况记录）。

## 1. 任务与范围回顾

对比本地参考项目 `E:\System\桌面\工具\wsl-dashboard`（Rust + Slint，**GPL-3.0-only——只借鉴思路，不引入任何代码**），按用户确认的"界面与核心功能一起改"重整 Hanxi WSL 工作台，补齐实用管理能力，不完整照搬。

## 2. 已落地能力（模块版本 0.2.0，提交线 dev）

| 能力 | 实现位置 | 要点 |
|---|---|---|
| 十项流式体检 + 提权白名单 + 正规卸载 | `service.go`/`ops.go`/`download.go` | 09-08 前既有；`GetReadiness` 死绑定已摘除 |
| 发行版六操作（唤终端/设默认/终止/导出/迁移/删除） | `distro.go` | 09-09 落地；导出补 tar/tar.gz 双格式与全量工件登记（`ListDistroExports`） |
| 只读取证抽屉 | `forensics.go` | VHDX 逻辑/实占（`GetCompressedFileSizeW`）+稀疏、guest df、IPv4；**停止实例绝不进 guest**（防顺手开机，见 TROUBLESHOOTING #39） |
| 克隆（快路径） | `clone.go` | 停源→8MB 流式拷 VHDX（`wsl:clone` 进度事件）→`--import --vhd`；WSL 2.7.3+ 门控；失败保留拷贝盘、半成品实例尽力注销；源+目标双单飞闸 |
| 导入面 | `clone.go` | `wsl --import` tar 落成新增发行版；名称防撞/扩展名白名单/目标目录复用迁移校验 |
| /etc/wsl.conf 编辑器 | `wslconf.go` | 语法闸门→`[user] default` 经 `id -u` 求证→root 备份 `.bak`→stdin `tee` 覆盖→读回复验；CRLF 归一；版本门控警示不硬拦 |
| VHDX 三级瘦身 | `compact.go` | 空间预检（源卷+导出卷 实占+2GB）→强制 tar 备份→fstrim+停机确认→Tier1 `Optimize-VHD`（Hyper-V 模块探测在位才弹 UAC）→不足 100MB 转 Tier2 注销重导入（稀疏尽力恢复）；失败路径一律点名备份位置；VHDX 路径只从注册表推导（提权注入红线） |
| NAT 端口转发账本 | `portproxy.go` | 规则持久化 `<DataDir>/wsl-portproxy.json`；netsh 现态对照（生效/漂移/待应用/未运行）；批量应用单 UAC 先删后加逐条传播退出码；防火墙 `Hanxi WSL <port>` 联动；外部转发只展示不触碰；清理托管 |
| 前端 | `WSLView.vue`（单文件 ~1500 行） | 第三 Tab「端口转发」；行内 10 操作钮 + 克隆/导入/wsl.conf/瘦身/取证五种展开表单；事件驱动进度（readiness/msi-download/clone/compact） |

许可证合规：参考项目 GPL 代码零引入；借鉴的均为 wsl.exe/netsh 命令用法与"备份先行/代次过滤/白名单"类工程思路，实现全部自研且有离线单测锁命令面。

**明确不借鉴的反模式**：编译期过期 kill-switch、调试构建禁 TLS 校验、`curl | sh` root 远程脚本。

## 3. 验证状态（2026-09-14）

- 通过：`gofmt`、`go vet ./...`、`go test ./...`（全仓含 wsl 新增 45+ 用例）、前端 `typecheck`、生产构建、77 文件 653 测试、ESLint（观察基线）、`verify:tidy`、`verify:bindings`（重生成零差异）。
- 未通过/未执行（如实记录）：`-race`（本机无 GCC/CGO，历史已知）；克隆/瘦身/导入/netsh 应用等**真机端到端验证未执行**——本轮全部结论基于离线桩单测与命令面断言，未对真实发行版做过启停/克隆/压缩/防火墙变更。首次真机使用建议先在小发行版上试导入导出闭环。

## 4. 优化批次（09-14 二轮）与遗留

已补：克隆目标卷空间预检；取消通道（下载/克隆全程、瘦身备份与停机段，动盘段拒绝）；wsl.conf 开机探针+test -f 口径（堵"停止实例跳过备份直接 tee"丢数据路径）；瘦身备份目录可指空闲卷；端口转发默认 127.0.0.1+0.0.0.0 曝光警示、IP 并行解析、应用/清理与重操作共闸、账本损坏逃生口；wsl.conf 保存后终止引导。

**网络模式批次（09-14 三轮，`hostconf.go`）**：`~/.wslconfig` networkingMode 检测（nat/bridged/mirrored/unknown，缺省归一 nat）出现在取证抽屉与端口转发视图载荷；mirrored 时转发页明示 localhost 直通、端口proxy 属遗产；.wslconfig 全文编辑器复用 INI 语法闸门+备份+原子写复核，另加 networkingMode 值白名单（写错=所有发行版集体失联，落盘前拦截）；`ShutdownWsl`（wsl --shutdown，重操作共闸）+ 保存后生效引导。前端 32/32 spec（含保存两段确认、镜像横幅、全停链）。

遗留与后续候选（未排期，需另行确认）：

- 参考项目能力中本期未纳入：usbipd USB 透传、`wsl --mount` 磁盘挂载、schtasks 计划任务 GUI、单发行版 WSL1↔2 转换（`--manage --set-version`，补上可解锁 WSL1 克隆前置）、镜像测速多源下载。
- 瘦身动盘段（optimize/reimport）不可取消属刻意设计；`--import --vhd` 之外的 tar 克隆兜底未实现（现指路人工导出→导入）。
- 瘦身后 `/etc/wsl.conf` 默认用户恢复等 Tier2 边角语义按"如实提示"处理，未做自动化。
- 参考项目进度文档（本文 09-08 版）遗留的 `C:\Users\Administrator\.claude\plans\floating-tumbling-candle.md` 草稿已过时，可删。
