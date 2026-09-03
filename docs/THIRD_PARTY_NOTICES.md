# 第三方软件告知

## MangoDisk

- 项目：MangoDisk
- 上游仓库：https://github.com/harry0703/MangoDisk
- 许可证：GPL-3.0-only
- 对应源码：每个下载版本可通过 `https://github.com/harry0703/MangoDisk/tree/<tag>` 获取，例如 `v1.0.7` 对应 `https://github.com/harry0703/MangoDisk/tree/v1.0.7`。

Hanxi 对 MangoDisk 采用用户侧按需下载和独立进程托管：版本文件直接来自上游 GitHub Releases，Hanxi 不修改、不静态链接、不内嵌 MangoDisk 源码或二进制。MangoDisk 的磁盘扫描、清理、卸载和系统设置功能均由其原版 GUI 提供。

当前 Hanxi 仓库和安装包不包含 MangoDisk EXE。若未来改为预装或随 Hanxi 二进制分发，发布流程必须另行落实 GPLv3 许可证文本、版权告知和对应源代码提供义务。

## EarTrumpet

- 项目：EarTrumpet
- 上游仓库：https://github.com/File-New-Project/EarTrumpet
- 官方许可证说明：仓库 LICENSE 为 MIT License，但文件开头附带 Excluded Entities 条款，明确排除 Yellow Elephant Productions、Tidal Media Inc.、Articent Group LLC 三家公司行使许可权利（GitHub API 因此标记为 NOASSERTION 而非 MIT）。
- 发行方式：Microsoft Store（产品 ID 9NBLGGH516XP）与上游自托管官方直装渠道（https://install.eartrumpet.app 的 AppInstaller 包，Azure Code Signing 签名，版本与商店同步构建）双渠道发行。

Hanxi 对 EarTrumpet 只纳管官方直装渠道（用户拍板不代管商店渠道）：查询直装版注册状态与运行态、经 AUMID 激活启动、终止进程退出（上游无优雅退出通道，退出为指纹复核后的直接终止，配置文件为事务性写入故强杀安全）、为当前用户卸载，安装/更新通过解析官方 appinstaller 清单（钉死包名/发布者/主机 + winget-pkgs 清单 SHA-256 交叉比对，Windows 部署期校验包签名）后交给 Add-AppxPackage。商店渠道仅保留注册检测用于并存警告（两渠道共享单实例互斥体），不提供任何商店操作。Hanxi 不修改上游二进制、不缓存不再分发安装包（安装用临时文件，装完即删），不修改其配置与托盘行为，不绑定 JobObject。卸载前会明确提示包 LocalSettings 中的设置将随包删除。若未来改为捆绑或代管分发，发布流程必须随附 MIT 许可证文本（含 Excluded Entities 条款原文）并另行评估分发合规性。

## NanaZip

- 项目：NanaZip
- 上游仓库：https://github.com/M2Team/NanaZip
- 官方许可证说明：https://github.com/M2Team/NanaZip/blob/main/License.md
- 发行方式：Hanxi 按需从上游 GitHub Releases 下载官方 stable MSIXBundle，并交由 Windows 为当前用户安装；Hanxi 不修改、不静态链接，也不在自身安装包中预置 NanaZip 二进制。

NanaZip 不是单一 MIT 许可证项目。其自有源代码使用 MIT License；应用文件关联图标使用 CC BY-ND 4.0；7-Zip 及派生代码包含 7-Zip License、GNU LGPL、GNU LGPL 加 unRAR restriction；LZFSE 等部分使用 BSD 3-Clause，其他第三方组件沿用各自原许可证。unRAR restriction 禁止使用相关代码重建 RAR 压缩算法。

Hanxi 可能在用户侧缓存原始官方 MSIXBundle 以支持安装和升级，但不会修改其内容。若未来改为随 Hanxi 安装包预置或再分发 NanaZip，发布流程必须重新核对并随附 NanaZip/7-Zip 及所有适用第三方许可证、版权告知和限制条款，不能仅标记为 MIT。
