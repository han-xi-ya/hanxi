# P0 批 3 正式修复计划(前端真相收口)

> 依据 [CODE_QUALITY_REVIEW_PROGRESS_2026-09-19](../CODE_QUALITY_REVIEW_PROGRESS_2026-09-19.md) §4.1–§4.6。审查写于 09-19,期间已被并行批次修掉一部分——本计划先逐条**现状复核**(2026-09-23 代码实地核),再定修复面,不重做审查、不重复修已收口的东西。

## 0. 现状复核结论(实地核过)

| 审查项 | 09-23 现状 | 批 3 是否要做 |
|---|---|---|
| 4.1 旧快照冒充实时 | ❌ 未修:`store.ts` refresh 失败仍静默保留旧 snap(注释"视图现状口径"即审查点名处) | ✅ 做 |
| 4.2 本地/远程加载竞态 | ⚠️ 半修:`loadManagedVersions` 已"本地优先、远程挂起不阻塞";但**统一 loading 由远程分支收尾**,本地未落地时面板 `installed.length===0` 照判首用空态——误显仍在 | ✅ 做(只补未收的后半) |
| 4.3 already-installed 清票 | ❌ 未修:`runDownload` settle 仍 `v.version === rel.version` 精确比较,不过 adapter 互认 | ✅ 做 |
| 4.4 旧响应覆盖 | ✅ 共享层已修(store `loadGeneration` 包住全部 setter);❌ VSCodeView/EverythingView 自定义加载无 generation(实地 grep 无 hits) | ✅ 做(仅自定义两视图) |
| 4.5 多份 busy 真相 | ❌ 未修:ddnsgo/papertodo/everything 专属动作各自持 busy,未统一进 store 互斥执行入口 | ✅ 做 |
| 4.6 可访问性 | ⚠️ 半修:MainTabNav 已有 tablist/tab/aria-selected/aria-controls;缺 roving tabindex + 方向键/Home/End。进度条 progressbar 语义、retry `<a @click>` 键盘可达——实地未见命中,开工时逐组件精确定位 | ✅ 做 |
| §5-1 cancellable 真实性 | ✅ 由批 2b 落定:install/update 观察面有真取消链;批 3 仅加一条断言测试锁"无 cancel 链的 kind 不得虚标 true" | 只核不做 |

## 1. 修复项

### B3-a 共享 store/面板核心(4.1 + 4.2 + 4.3)

- **stale 真相**(`components/managed/store.ts`):`refresh()` 失败不再只 console.warn——记 `statusError` + `lastStatusAt` + `stale`(连续失败计数或首次失败即 stale,采**首次失败即降级呈现**,恢复即清);`ManagedControlBar` 状态灯在 stale 时改中性灰 + 文案「状态暂不可确认(最后同步 HH:mm)」,停 live 呼吸动画;事件回流(`subscribeInstanceState`)自动清 stale。
- **本地未解析不判空**(`composables/loadManagedVersions.ts` + `ManagedVersionPanel.vue`):sources 增加 `setLocalResolved(true)`(local+active 两个 promise 全部落地后);面板首用空态与「已安装版本」区在 `!localResolved` 时显骨架/保持 loading 文案,不渲染"尚无安装"。
- **清票走互认**(`store.ts` runDownload settle):`installed.some(v => sameVersionLoose(v.version, rel.version))`,其中 `sameVersionLoose = adapter.versions.sameVersion ?? (a,b)=>a===b`;与 statusOf 的既有互认口径共用同一函数,防"远程判已装、清票判未装"分叉。

### B3-b 自定义视图 generation(4.4 残留)

- `VSCodeView.vue`、`EverythingView.vue` 的自定义刷新链(双形态快照/es 组件票据)各加 `loadGeneration` 计数:`++gen` 起步,所有异步回写点(状态 set、列表 set、loading 收尾、error set)以 `gen === current` 为闸门;下载事件触发的刷新与手动刷新同闸门。口径对齐 store.ts 现成实现,不发明第二套。

### B3-c 专属动作统一互斥(4.5)

- store 暴露 `runExclusive(fn)`(即现 runControl/runToggle 的互斥内核,复用 actionBusy 闩);DdnsGo 启停/端口写、PaperTodo variant 切换、Everything 控制台条专属钮逐一目视核对,一律改经 `store.runExclusive`;专属动作触发下载的(如 Everything"安装 es 组件")点击即挂 pending 票据(同 B3-a runDownload 首挂票据语义),封双击窗口。
- 不扩范围:留言板/热键等非托管专属动作不在批 3。

### B3-d 可访问性(4.6)

- `MainTabNav`:roving tabindex(选中项 0、其余 -1)+ `←/→` 循环换选 + `Home/End` 跳首尾,change-selects 模式(焦点即选中,与现有点击语义一致);
- 下载进度条补 `role="progressbar"` + `aria-valuenow/min/max`(无总量时 `aria-busy` 不定态);
- retry/刷新类 `<a @click>` 全数改 `<button type="button">`(视觉沿用 link 类),开工时 `grep '<a ' frontend/src/components/managed frontend/src/views` 精确定位;
- **390px/200% 真机 WebView2 验收**:代码不修,产出人工清单交机主(归 P1 真机债同场跑)。

### B3-e 观察面诚实性小项(§5-1 联动)

- 测试断言:hub 中非 install/update/rollback/remove/repair 五 kind 的记录 `cancellable=false` 恒定(现行为已如此,补锁);批 2b 后资产事务 cancellable=true 有真链,注释互引。

## 2. 测试口径

- vitest(deferred Promise 双编排):本地慢于远程不闪空态;远程先回不清本地 loading;stale 呈现与恢复;sameVersion 互认清票(beta tag 用例,借 recordly/paseo 的 coreOf 方言);generation 旧响应晚到不覆盖新态(两自定义视图各一);runExclusive 交叉点击串行;TabNav 键盘操作矩阵。
- 既有 17+ adapter 行为零变化的回归基线:全量 vitest + vue-tsc + eslint + production build 全绿为收批闸。

## 3. 批次顺序与提交纪律

B3-a →(绿后)B3-b → B3-c → B3-d → B3-e 并入各批测试;每子批一个原子提交(中文 conventional);**批 3 期间不动 Go 侧**(唯一例外:无)。P3 前端打磨批次一恢复若同窗进行,以本批为基线重出实施计划,避免两拨人改同文件。

## 4. 风险与不做清单

- stale 呈现改造会动 ManagedControlBar 视觉词表(新增"暂不可确认"态)——与 hanxi-workbench-ui 评审一次过,不私加色档;
- 面板空态判定改 `localResolved` 后,"确无安装"与"尚未扫描"两种空态文案要区分,不新造第三个空态形态(复用骨架行);
- **不做**:virtualization/性能优化、状态管理库引入(无 Pinia 纪律不动)、Go 侧 bindings 变更(本批零后端签名改动 → bindings 零漂移,verify:bindings 必绿)。
