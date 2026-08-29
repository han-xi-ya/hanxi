# 第三方软件告知

## MangoDisk

- 项目：MangoDisk
- 上游仓库：https://github.com/harry0703/MangoDisk
- 许可证：GPL-3.0-only
- 对应源码：每个下载版本可通过 `https://github.com/harry0703/MangoDisk/tree/<tag>` 获取，例如 `v1.0.7` 对应 `https://github.com/harry0703/MangoDisk/tree/v1.0.7`。

Hanxi 对 MangoDisk 采用用户侧按需下载和独立进程托管：版本文件直接来自上游 GitHub Releases，Hanxi 不修改、不静态链接、不内嵌 MangoDisk 源码或二进制。MangoDisk 的磁盘扫描、清理、卸载和系统设置功能均由其原版 GUI 提供。

当前 Hanxi 仓库和安装包不包含 MangoDisk EXE。若未来改为预装或随 Hanxi 二进制分发，发布流程必须另行落实 GPLv3 许可证文本、版权告知和对应源代码提供义务。

## NanaZip

- 项目：NanaZip
- 上游仓库：https://github.com/M2Team/NanaZip
- 官方许可证说明：https://github.com/M2Team/NanaZip/blob/main/License.md
- 发行方式：Hanxi 按需从上游 GitHub Releases 下载官方 stable MSIXBundle，并交由 Windows 为当前用户安装；Hanxi 不修改、不静态链接，也不在自身安装包中预置 NanaZip 二进制。

NanaZip 不是单一 MIT 许可证项目。其自有源代码使用 MIT License；应用文件关联图标使用 CC BY-ND 4.0；7-Zip 及派生代码包含 7-Zip License、GNU LGPL、GNU LGPL 加 unRAR restriction；LZFSE 等部分使用 BSD 3-Clause，其他第三方组件沿用各自原许可证。unRAR restriction 禁止使用相关代码重建 RAR 压缩算法。

Hanxi 可能在用户侧缓存原始官方 MSIXBundle 以支持安装和升级，但不会修改其内容。若未来改为随 Hanxi 安装包预置或再分发 NanaZip，发布流程必须重新核对并随附 NanaZip/7-Zip 及所有适用第三方许可证、版权告知和限制条款，不能仅标记为 MIT。
