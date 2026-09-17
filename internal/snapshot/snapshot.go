// Package snapshot 是数据自动版本快照的平台底座（对外文案统一叫"历史版本"）。
//
// 概念撞名预警：仓内 "Snapshot" 一词已被托管实例运行态事件载荷占用
// （instance.Snapshot 二十余处），因此本包导出的类型/方法一律用
// Checkpoint / Revision / History 语义命名，包名 snapshot 仅保留用户口径。
//
// 机制（docs/plans/PLAN_SNAPSHOT.md）：白名单（config.json + state/** + memo/**，
// 见 whitelist.go）内的用户数据，在窗口失焦/隐藏/最小化、mtime 空闲巡检、
// 退出前三类触发点自动留版本（见 service.go 三源一闸）。git 可用时落
// `<数据根>/.snapshots/repo.git` 分离仓库（--git-dir + --work-tree，数据面零污染，
// 见 git.go）；git 缺失（含 Microsoft Store 假存根，见 detect.go）降级
// `.snapshots/backup/` 影子拷贝滚动 30 份（见 backup.go）。
//
// 红线：永不自动 push（全部命令只动本地仓库，代码面不存在 remote）；
// 全程静默，失败只记日志 + 首次一条通知。它是平台底座不是 extapi 模块
// （先例：settings 也走 AppService 暴露面），由装配根 app.New 构造并以
// application.NewService 出绑定。
package snapshot
