# OCR 双引擎计划：微信版（私发）+ PP-OCRv6 公开版并存

> **版本**：v1.0（2026-09-16 定稿分析，本文档为执行计划）
> **决策依据**：识别能力维持"上游 sidecar、Hanxi 只做探活+转发+生命周期"的既有边界
> （`internal/modules/ocr/service.go` 定位注释）；开源引擎不嵌入主程序、不走 cgo 进主包。

---

## 1. 结论与定位

| 组件 | 引擎 | 分发 | 形态 | 体积 |
|---|---|---|---|---|
| **hanxi-ocr 微信版** | wcocr.dll（腾讯专有，逆向提取） | 私发，**永不进公开渠道** | 单文件 exe（维持现状） | ~48MB |
| **hanxi-ocr 公开版** | PP-OCRv6（ONNX + onnxruntime + RapidOCR C API） | 开源仓库 + 自建下载源 | zip 目录版（exe + 2 dll + 模型） | ~40-45MB |

- 两者实现**同一套 HTTP 契约**（现 v0.3 契约 + 本轮放宽项），对 Hanxi 是"同一上游的两个型号"。
- 并存语义为**单端口、单活引擎**：切换 = 停当前托管实例、拉起另一件；snip 截屏、悬浮卡、
  轮盘命令、自动复制全链路复用，零感知。
- cgo 只存在于公开版组件自己的构建里；**Hanxi 主程序保持纯 Go 零 cgo**。
- 工期口径：M0 闸门 2-3 天；M1 双引擎并存可用 ~1 周；M2 发布质量 ~2 周（全职）。

## 2. 仓库与交付物切分

```
hanxi-ocr 仓库（独立，公开主线）
├── engine/               引擎后端抽象（本项目核心增量）
│   ├── engine.go         统一接口：Recognize(img) → lines/text/elapsed
│   ├── paddle/           PP-OCRv6 后端（cgo 直链 RapidOCR C API + onnxruntime）
│   └── wechat/           wcocr 后端（仅私发分支携带，不进公开主线）
├── server/               HTTP 契约壳（现有代码，name/engine 字段放宽）
└── scripts/              CI：Windows 构建 + zip 打包 + manifest 生成

hanxi 主仓（本仓）
└── internal/modules/ocr/ 并存改造（§5 清单）
```

公开版随件资产清单（zip 内）：`hanxi-ocr.exe`、`onnxruntime.dll`、`rapidocr.dll`、
`models/*.onnx`（det/rec small，v5 同构模型为回退档）、`manifest.json`（组件版本+文件校验）。

## 3. 契约规格（v0.4 增量，向后兼容 v0.3）

1. `/api/status` 的 `name` 固定 `"hanxi-ocr"` 不变（Hanxi 探活校验放宽为契约名集合）；
   `engine` 字段取值扩展：`wechat-4.0` | `pp-ocrv6-small` | `pp-ocrv6-medium` | `pp-ocrv5`（回退档）。
2. `/api/ocr`、`/api/ocr/upload`、`/api/shutdown` 请求响应结构不变——**公开版必须逐字段兼容**，
   这决定 Hanxi 转发与 snip 链路零改动。
3. 新增约定（仅公开版需要）：目录布局按 `manifest.json` 自检启动，找不到模型即
   `engine_running:false` + 中文 error，不崩不退端口。
4. 微信版 v0.3 件在放宽后的 Hanxi 上照常工作（导入校验按引擎分流，见 §5.3）。

## 4. 里程碑与步骤

### M0 可行性闸门（2-3 天）⚠️ 不通则整案重估，止损点

- [ ] RapidOCR C API 的 Windows 预编译产物摸底（有无现成 DLL/导入库；无则按其源码自编，多 1 天）
- [ ] Go cgo 胶水最小闭环：加载 v6 small ONNX → 识别一张真实截屏 → 输出行文本
- [ ] 快速对照：同一张图跑微信版 hanxi-ocr，肉眼初判差距
- **验收**：命令行 demo 识图正确率符合预期；cgo 构建链在开发机跑通
- **退出条件**：若 v6 small 明显拉胯且 medium 延迟 > snip 可接受线 → 回到
  "公开包不带 OCR、只引导导入"的重估会议

### M1 公开版组件成型 + Hanxi 并存可用（~4-5 天）

组件侧：
- [ ] `engine/` 后端抽象落地：现有 wcocr 直调代码收编进 `wechat` 后端（行为零变化）
- [ ] `paddle` 后端完整化：det/cls(可选)/rec 全管线、大图降采样、并发防护、30s 上游超时
- [ ] `/api/status` 的 engine 字段如实上报；`manifest.json` 自检
- [ ] Windows CI 构建 + zip 打包（含模型），版本号 v0.4.0-alpha

Hanxi 侧（改造清单详见 §5）：
- [ ] 探活契约名放宽 + engine 字段驱动状态展示
- [ ] store 多引擎注册表 + 老配置迁移
- [ ] 导入按引擎分流：微信件走原校验；Paddle 件认"目录 + manifest"
- [ ] 设置页引擎选择 + 切换重启（OcrView 增补，无新页面）
- **验收**：同一 Hanxi 构建下，导入微信件与解压 Paddle 件均可完成 启动→snip→悬浮卡→自动复制 全链

### M2 盲测与发布质量（~4 天）

- [ ] 盲测集：`RuntimeDir()/ocr` 历史 snip 语料 + 手写/拍照/低对比度/竖排/emoji 补充 ≥100 张
- [ ] v6 small vs medium 双档跑分（精度 + P50/P95 延迟 + 内存峰值），定默认档
- [ ] 与微信件对比结论沉淀进本文档 §8（诚实记录，不粉饰）
- [ ] 边界回归：64MB 图、无微信干净虚拟机、DPI 125%/150%、端口占用、磁盘只读
- [ ] `docs/THIRD_PARTY_NOTICES.md` 登记：PP-OCRv6 模型、onnxruntime、RapidOCR 许可证链
- **验收**：中文截屏精度不显著劣于微信件（允许小幅互有胜负）、snip 端到端 P95 < 2s

### M3 自动分发（3-5 天，可后置不阻塞发布）

- [ ] 你侧服务器/CDN：`manifest.json` + zip + sha256；版本清单含引擎字段
- [ ] Hanxi 下载器：首用引导"下载 PP-OCR 引擎(~45MB)"、断点续传、sha256 校验、
      解压到 `DataDir()/ocr-engines/pp-ocrv6/` 并自动登记
- [ ] 更新检测：探活对比 manifest.version → 设置页"引擎可更新"角标 → 一键升级
- **验收**：全新机器（无微信件）从零到"截图即识"全程无需浏览器搜索下载

### M4 收尾

- [ ] 组件 README + 本计划回写实际结果；`docs/TROUBLESHOOTING.md` 沉淀 cgo/打包踩坑
- [ ] hanxi-ocr 模块版本号 0.1.0 → 0.2.0，`Info().Description` 改口径
      （"离线识别：内置开源 PP-OCR 引擎可选下载，支持导入微信引擎组件增强"）

## 5. Hanxi 侧改造详细清单（代码锚点）

| # | 位置 | 改动 | 量级 |
|---|---|---|---|
| 5.1 | `service.go` `probeStatus`（`v.Name == "hanxi-ocr"` 校验处） | 放宽为契约名集合；`engine` 入 probeCache 并透传前端 | 小 |
| 5.2 | `store.go` | `exePath string` → `engines` 注册表（wechat/paddle 各 {path,version} + active），`ocr.json` 老字段迁移兼容 | 中 |
| 5.3 | `service.go` `ImportServiceExe` + `models.go` `resolveServiceExe` | 按引擎分流校验；自动发现锚点扩为 同级 `../hanxi-ocr/` + `DataDir()/ocr-engines/` | 中 |
| 5.4 | `service.go` `StartService`/`StopService`/`ensureOnlineForSnip` | 拉起目标改为 active 引擎路径；切换引擎=先停后起（复用 instance.Engine，无新生命周期代码） | 小 |
| 5.5 | `frontend/src/views/OcrView.vue`（549 行） | 组件区升级"引擎列表"两行卡：当前引擎徽标、导入/下载动作、切换按钮（带确认）；文案去除"微信 4.0 引擎"独占口径 | 中 |
| 5.6 | `module.go` 模块描述与导航 | 仅文案；`TrayCommands`/`Nav` 结构不动 | 微 |
| 5.7 | `service_test.go`/`store_test.go` | 迁移用例、分流校验用例、active 切换用例 | 中 |

不动区（明确零改动）：`instance/`、`snip/`、`snipcard.go`、`card_*.go`、识别转发
`RecognizeImage`、悬浮卡与轮盘链路。

## 6. 分发与升级链

```
服务器:  /ocr-engine/index.json         ← 总版本清单（定名 index.json 与包内 manifest 消歧，
                                          规格见 hanxi-ocr-dev\publicdist\docs\release-manifest-spec.md）
         /ocr-engine/hanxi-ocr-paddle-<ver>.zip
Hanxi:   启动后低频探测(或进 OCR 页时) → 发现新于 active 版本 → 提示一键更新
         更新 = 下载→校验→解压到新版本目录→登记→(运行中则)询问后切换重启
回退:    旧版本目录保留一版；manifest 可随时回指旧版
```

组件升级三层（模型文件级 / 运行时级 / v7 换代级）对 Hanxi 均为 0 提交——契约不含模型语义。

## 7. 红线与合规

1. wcocr.dll 及微信版件：不进公开仓、不进 Hanxi 构建脚本、不进下载源；私发口径维持现状。
2. 公开版依赖许可证登记：PP-OCRv6/v5 模型（Apache-2.0）、onnxruntime（MIT）、
   RapidOCR（Apache-2.0）→ `THIRD_PARTY_NOTICES.md`。
3. 自动下载必须**用户明示触发**（首用确认 + 设置页可关），静默拉 45MB 不可接受。
4. Hanxi 主包体积零增长（公开版件按需下载，不进安装包）。

## 8. 风险登记与实测记录

| 风险 | 应对 | 状态 |
|---|---|---|
| RapidOCR C API Windows 产物不给力 | M0 首日摸底；自编兜底 +1 天 | 待验 |
| v6 small 截屏中文劣于微信件明显 | M0 快判 + M2 定量；medium 换档或回退"不内置"决策 | 待验 |
| cgo 单文件化成本（DLL 必须落盘） | 已决策：公开版目录 zip，放弃单文件洁癖 | 已消化 |
| snip 冷启动延迟（模型加载 1-3s） | 托管常驻 + `随退出关闭` 开关现成；量化后进 §8 | 待验 |
| 内存峰值（大图） | 组件侧降采样；上限对齐 64MB | M2 验 |

> M2 完成后在此追加：盲测集规模、v6 small/medium vs 微信件的精度与延迟实测表。

## 9. 待拍板决策（进入 M0 前确认）

1. **默认档**：v6 small 起步、medium 作可替换后手（本计划默认）——是否同意？
2. **M3 自动下载**：随公开版首发就上，还是先"手动导入 Paddle 件"跑一两个版本？（本计划按可后置排期）
3. **公开版命名**：仍叫 `hanxi-ocr.exe`（契约 name 不变）还是改名区分？（建议不改名，减少分发面）
4. cls 方向分类器：截屏场景默认关闭省 ~10MB（拍照场景有倾斜，M2 盲测定夺）。
5. 公开组件自有代码 LICENSE：建议 Apache-2.0（与依赖链一致），待拍板——合规检查单卡此项。

### 决策回写（2026-09-17）

- 包内 manifest 身份字段统一为 `name`（=HTTP 契约名）：初版组件按 `component` 起草、打包线定稿
  `name`，两处已收敛（组件自检 `service\engine\paddle\manifest.go` 现强校验 `name=="hanxi-ocr"`）；
  权威规格 `publicdist\docs\manifest-spec.md`。
- 服务器侧总清单采纳打包线定名 `index.json`（本文件 §6 已同步）。
- **微信引擎基线已建立**（`hanxi-ocr-dev\testkit\`，60 张七类语料）：冷启动 837ms、热路径
  P50 478ms / P95 779ms / max 1200ms；已知失败面=低对比印刷体(5 空)+LED 点阵(5 空)+竖排碎裂。
  M2 的 v6 跑分与对比直接用 `testkit\bin\runner.exe/score.exe`；对比基准件口径改为
  `dist\hanxi-ocr.exe` v0.3.0 内嵌版（`service\` 下 9/15 开发构建因布局演进启动报缺 wxocr.dll，
  属陈旧产物，M1 集成时用 build.sh 重建后回归）。labels\ 60 份需人工校对后方可作精度参考。
- **M0 闸门结论：过（有条件）**（`hanxi-ocr-dev\spike\REPORT.md`）。路线修正：
  **公开版零 cgo**——RapidOCR clib 停更不支撑 v6、本机无 C 编译器，实际落地为
  "纯 Go + 动态装载 onnxruntime.dll + 手写 DB 前后处理"（spike\recognize ~1200 行，已端到端跑通）。
  关键数字：冷启动 178-395ms；snip 典型框选 P50 0.5-1.0s（det 须配 limit_type=max/960）；
  v6 small 模型 ~30MB；内存：snip 169MB、整屏大图 1.8GB（**M1 必须做缓冲治理**）。
  新增硬性打包要求：msvcp140 系列 VC 运行时 14.40+ **app-local 随件**（ORT 1.30 在旧运行时
  CreateEnv 直接 AV 崩，干净机器必炸）——pack_public 清单与 manifest 需增补这三个 DLL。
  快判：1:1 框选与微信件打平互有胜负，整屏小字弱于微信件但不在 snip 链路，退出条件不触发；
  medium 档与精度定量留给 M2 盲测。
- **M1 已收官（2026-09-17）**：spike 管线合入 `service\engine\paddle\`（6 文件，纯 Go，双模式
  vet/build 绿），内存治理达标（整屏 1.8GB→329MB 封顶、snip 132MB、72MP 硬拒）；收编时修掉
  spike 两个真 bug（ORT 输出悬垂切片、uintptr 跨函数转发的栈逃逸崩溃，已沉淀主仓
  TROUBLESHOOTING #54）。首个真实引擎包 44MB：`hanxi-ocr-dev\dist\hanxi-ocr-paddle\` 与
  同级自动发现锚点 `工具\hanxi-ocr-paddle\` 均已落位；rapidocr.dll 出局的 manifest/pack
  修正已同步。M2 待办不变：labels 人工校对、真手写/拍照补料、medium 档量化、
  运行中切换分支人工核验（真件在 Hanxi 内启停）。
