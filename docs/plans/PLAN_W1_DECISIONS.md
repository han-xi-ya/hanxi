# W1 决策与前置调研(2026-09-20)

> 排期见 [PLAN_REMAINING_WORK § P4 执行波次排序](PLAN_REMAINING_WORK.md)之 W1。本文四节:§1 为 N3 策略裁决(需机主拍板),§2–§4 为三项前置调研结论(不依赖拍板,证据化留档)。

---

## 1. N3 决策:hanxi 对"外部实例"(非本会话启动的进程)的控制边界

### 现状事实(代码锚点,2026-09-20 复核)

- 管理边界现统一为"本会话启动的进程树":supervisor 引擎挂 JobObject,强杀原语 `TerminateProcess`(`internal/platform/windows/process.go:134`)仅对自家树可用;
- 外部探测参差:everything 有 external 探针(窗口类/互斥体)但拿不到 PID,`Quit()` 只回指引(`internal/modules/everything/service.go:293`);snipaste 挂 `noExternalProbe` 恒报"不在运行"(`internal/modules/snipaste/instance/instance.go:90-95`),且无 OpenWindow 动词;
- 唤窗两族并存:单实例信使(ccswitch/everything)与按 PID EnumWindows(rustdesk/litemonitor/subnetdesk/rufus/flclash/bcu/guoheview);已知两隐患——①`focusWindowsByPIDs` 以 `IsWindowVisible` 判恢复,最小化窗带 Visible 标志→不 SW_RESTORE;②置前台族内多裸用 `SetForegroundWindow`,而强制特权版 `SetForegroundForce` 现成(`internal/platform/windows/foreground.go:29`);
- 优雅退出通道已有抽象位:supervisor `SetQuitHook`(管道/WM_CLOSE/HTTP shutdown 由模块注入,`packages/go/supervisor/supervisor.go:20`)。

### 三案

| 案 | 内容 | 评估 |
|---|---|---|
| A 保守 | 外部实例只可"看状态 + 唤窗",退出一律指引 | 维持现状语义,N1/N2 两个真机痛点都不解决 |
| **B 优雅越权、强杀不越权(推荐)** | 探测统一化(进程名→PID→窗口枚举);唤窗对所有在跑实例放开;退出对**外部实例只允许优雅信号**(官方 `-quit` 类参数 / WM_CLOSE / 信使退出),**强杀始终仅限本会话 JobObject 树**;优雅不生效(挂死)时报"退出未响应"+指引,不升级强杀 | 解决 N1/N2;与 N7 联动天然安全(正在同步的飞牛实例永不被强杀);与"数据风险"最小化一致(托盘/后台应用的未保存态不被暴力打断) |
| C 全越权 | 外部实例也可强杀 | 不推荐:snipaste 有钉图未保存态、everything 索引/设置写入中,强杀=数据风险;且 JobObject 管不住非入组进程,强杀旁路了受控回收路径,监管语义双标 |

### 裁决落地实施面(= W2 工作分解,已按 C+分档终稿更新)

1. **平台层公共件**:外部 PID 取得助手(按进程名枚举)+ **分档退出执行器**(优雅信号→验证→按 `force_free`/`confirm_force` 决定强杀或弹窗确认,前端确认流走共享契约)+ 标准唤窗实现(IsIconic→SW_RESTORE→SetForegroundForce,吸收 guoheview 标杆),供 N1/N2 与后续族推广复用;
2. **N2 everything**(`force_free` 档):按名取 PID(主窗口有固定窗口类)→ 试优雅信号(`-quit` 参数与 WM_CLOSE 的取舍实施时查官方参数定)→ 验证 → 仍存活直接强杀不弹窗;
3. **N1 snipaste**(依 §2 调研定稿,`force_free` 档):external 探测=按进程名 `Snipaste.exe` 取 PID;模块"唤窗"语义重定为**官方显隐贴图**——OpenWindow 动词映射 `Snipaste.exe toggle-images`(免费、走单实例转发,外部实例同样有效);Quit:免费版无官方退出命令 → 直接按档强杀;PRO 可先 `Snipaste.exe exit` 验证、强杀兜底;先做提权检测(elevated 目标被 UIPI 拦,优雅与强杀均不可达 → 降级仅指引,如实标注);
4. **契约登记**:catalog/共享契约新增"外部实例退出档位"字段(`force_free`/`confirm_force`,**未声明默认 `confirm_force` 保守档**);`SetQuitHook` 语义文档改为"external 态按档位政策执行,不再是仅指引"。

### 裁决结果(机主 2026-09-21 拍板:**C 采纳,带风险分档护栏**)

上表 B 案推荐被裁决取代。最终政策=外部实例退出能力全放开,但按模块风险分档,**对"正在干活"类永不闷头强杀**:

| 档 | 适用 | 外部实例 Quit 行为 |
|---|---|---|
| `force_free`(低风险) | snipaste、everything(数据可秒重建/有自动备份,强杀损失≈0) | 有优雅信号先试(`-quit`/信使/WM_CLOSE)→ 探针验证 → 仍存活 → **直接强杀(TerminateProcess by PID),不弹窗** |
| `confirm_force`(打扰风险) | rufus(写入中)、fnas 同步(传输中)、WindTerm/Termora(会话中)等所有"中断有实际损失"的,**含未来新托管的默认档** | 优雅信号 → 验证 → 仍存活 → **UI 明示"正在同步/写入,强杀可能丢一半"确认后杀**;取消=回指引 |
| 降级 | 目标以管理员运行(UIPI 拦截,见 §2) | 优雅通道与强杀均不可达 → 仅指引,如实标注状态 |

实现注记:外部进程不在本会话 JobObject 内,强杀=按 PID 的 `TerminateProcess`(单进程;含子进程树的外部进程如需整树回收,实施时按"快照枚举子进程→逐杀"补,snipaste/everything 皆单 exe,首版不涉及)。

---

## 2. N15 Snipaste×OCR 结合可行性调研 ✅ **结论:可行,免费路径即可落地**

信源:`docs.snipaste.com`(官方文档,GitHub wiki 镜像 `Snipaste/feedback` 可抓 raw)+ 官方 changelog + 开发者本人 issue 回复。版本事实:Windows 稳定版 v2.11.3(2026-01-18),**Snipaste 3 未发布**;1.x 永久冻结于 1.16.2。⚠️ 附带发现:`getsnip.com` 当前已跳转域名出售停放页,权威域名以 `www.snipaste.com` + `docs.snipaste.com` 为准(仓内文档凡引 getsnip 处需校正)。

**核心机制**:Snipaste 是 Qt 单实例应用——**再次启动 `Snipaste.exe <参数>` 时,新进程会把命令转发给已运行实例后自动退出**(`docs.snipaste.com/command-line-options`)。即"进程自启动即命令通道",hanxi 无需任何逆向,普通进程调用就能驱动**包括外部实例在内**的 Snipaste,且多数基础命令免费。

关键接口分级(免费 ✅ / PRO ⚠️):

| 能力 | 命令 | 档级 |
|---|---|---|
| 截图到指定文件(可被目录监听接收) | `Snipaste.exe snip -o "…\shot.png"` / `snip --full -o …` | ✅ 免费 |
| 截图进剪贴板 | `snip -o clipboard` | ✅ 免费 |
| 显隐全部贴图("唤窗"的官方等价物) | `show-images` / `hide-images` / `toggle-images` | ✅ 免费 |
| 优雅退出 | `Snipaste.exe exit` | ⚠️ **PRO** |
| 截图后自动送外部程序/内置 OCR/等待完成 | `-o …;exec(…)` / `ocr_clipboard` / `--block` | ⚠️ PRO(v2.11+) |
| 全局热键 F1/F3 注入触发 | SendInput(开发者在 issue #506 官方背书) | 适用但脆弱,仅兜底 |

**N15 立项判定**:
- **主通道(免费可跑)**:hanxi 调 `snip -o <hanxi 数据根固定文件>` 触发用户框选 → `ReadDirectoryChangesW` 监听落盘(容忍 v2.0.1+ 异步写盘与同秒重命名)→ 投喂现有 `ocr` 模块。与自截链路(`ocr/snip`)并行成"借 Snipaste 选区,hanxi 识字"。轮盘/托盘加一键"截图+识别"顺理成章;
- **PRO 用户增强**(检测后启用):`snip …;exec(hanxi-cli …)` 或 `--block`,免监听直推;
- **风险边界**:目标 Snipaste 若以**管理员**运行,CLI 转发/SendInput/taskkill 全被 UIPI 拦(issue #240 先例)→ 接线前先做提权检测,提权态实例降级为"仅指引"。
- 免费版的 Snipaste 不存在官方外部退出命令、对隐藏 Qt 窗口发 WM_CLOSE 不可靠(Qt 关窗=隐藏到托盘,类名随版本漂移)——此结论已回填 §1 实施面。

## 3. N5 starpie 参考项目核实 ✅

## 3. N5 starpie 参考项目核实 ✅

**结论:登记时的 "starpie" 即 [Star-Pie/StarPie](https://github.com/Star-Pie/StarPie),置信度高**(名称逐字吻合、形态精确对口、无第二候选;2026-08-23 新建、至今高频迭代(v1.6.8、PR 已到 #154),登记日检索不中系新仓库索引盲区,时间线自洽)。

- 形态:C#/.NET 8 WPF,Windows 鼠标轮盘手势系统,852★,topics 含 pie/windows-10-11/lightweight;
- ⚠️ **许可备忘**:README 自述"MIT 附加非商业限制",但 LICENSE 文件为**纯标准 MIT 文本、无非商业条款**——按借鉴交互设计(非搬运代码)口径以 LICENSE 为准;此表述不一致留档,勿在文档中复述"非商业限制"为真实约束。

**对 quickmenu 改进最值得借鉴的四条(对应"实在是不好用"的痛点)**:

1. **拖动阈值触发取代纯长按定时**:按下后小幅移动超阈值立即出盘(静止长按仍作兜底),消除"等它弹"的迟滞;轻点未达阈值→回放为原生右键,日常右键菜单不受影响;
2. **向外甩 = 安全取消**(轮盘转半透明确认态):取消成本从"拉回圆心"变为"顺势一甩",Fitts 定律友好,判断是对症第一条;
3. **停留展开二级环**:扇区内 hover-dwell→外环弹性展开二级扇区,外滑即触发——hanxi 轮盘的二级分组(`settings.TrayItemGroup`,已入库 `internal/settings/store.go:60`)正可对齐此连贯模型,替代弹出式子菜单;
4. **per-app 配置档 + 扇区数 4/8/12 可调**:按前台应用切动作集。

另有触发键可配置(右键/中键/侧键/键盘组合)、中心核可绑动作、独立轨迹手势等外围设计,实施计划阶段再取舍。**N5 实施计划**以 StarPie v1.6.8 的 README/Releases/changelog 为交互输入,连同前后端几何同源改造(踩坑 #50)一并出。

## 4. N8 WindTerm 发布形态与 7z 解包选型 ✅ **结论:前提反转——WindTerm 根本不需要 7z**

- **重大更正**:登记时"WindTerm release 为 7z 绿色包"有误。GitHub API 全量核对 kingToolbox/WindTerm 32 个 release 资产:**Windows 资产全部是 `*_Windows_Portable_x86_64.zip`**(2026-09-20 主会话独立复核一致:86 zip;仅 2020 年 v1.1 附带过一只历史 7z)。hanxi 现有 zip 安全管线**零改动**即可收编 WindTerm,W1 设想的"7z 解包门槛"对本批不存在。
- 版本通道更新:稳定版已是 **2.7.0**(2025-03),2.7.0 后仍有 Prerelease 线(登记时"2.6.1 稳定/2.7.0 beta"表述已过期);channel 适配位按 paseo/recordly 先例,稳定/预发布双通道素材现成。
- 附带风险提示:①`windterm.com` 403、**`windterm.cn` 是第三方站**(下载指向网盘),自动化一律走 GitHub releases;②官方声明 Apache-2.0 但**仓库根目录无 LICENSE 文件**——集成文档留一句备忘。
- **7z 通用能力(与本批解耦,归档备用)**:未来遇 7z-only 工具(如 mpv winbuild 的 `7z a -m0=lzma2` 产物),正式选型定为 **`github.com/bodgit/sevenzip`**(pin v1.6.5):纯 Go 无 CGO、BSD-3、算法覆盖 LZMA/LZMA2/zstd/AES 等、实测解最新真实世界 7z 发行包零错误;接入方式=artifact 解包处分发一个 .7z 分支、路径清洗与 zip-bomb 闸门复用到其 `File.Name`/`UncompressedSize`(库不代劳清洗,与 archive/zip 同风格)。外挂 7za.exe 仅作 documented fallback 不实现(写盘绕过自家闸门=安全回退)。触发时机:首个 7z-only 集成需求立项时再实施,当前不预建。
