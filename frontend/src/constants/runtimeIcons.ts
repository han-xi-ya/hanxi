// 红线图标第三来源（N27 尾巴）：`rt:<moduleId>` 的运行期本机提取通道前端侧。
//
// 后端集中服务（internal/app runtimeicon_service.go IconPNG）从机主本机已装
// 官方 exe 就地提取最大画幅 PNG（许可红线：不入库、不分发、结果只存进程与
// 本页内存）；本模块负责"每模块取一次、成败各缓存、永不抛扰渲染面"：
//   - resolved：moduleID → data URL（Wails 把 Go []byte 出货为 base64 字符串）；
//     用 shallowRef 整包替换保证 AppIcon computed 链反应式升级；
//   - failed：任何失败（未安装 / 停用门拒绝 / 解析报错 / 无头浏览器无桥）
//     都静默记负缓存，消费面据此维持 `rt:|i:` 声明的矢量回落，零观感损失；
//   - inflight：同 ID 并发去重（侧栏/首页/轮盘多挂载点只撞一次 RPC）。
//
// 通道注记：调用走生成绑定 RuntimeIconService.IconPNG（方法 ID 由 wails
// 生成器锁定，不自造 ByName 串）；但用**动态 import** 而非静态——本模块被
// wheelIconBudget 等公共层引用，静态 import 会把 @wailsio/runtime 的浏览器
// 副作用图拉进所有测试环境；动态形态 vi.mock 同样可拦截，生产图里 bindings
// 早已被 useModuleCatalog 等静态引用，不新增 chunk。
import { shallowRef } from 'vue'

const resolved = shallowRef<Readonly<Record<string, string>>>({})
const failed = new Set<string>()
const inflight = new Map<string, Promise<void>>()

/** 已提取成功的 data URL；未成功（在途/失败/未发起）恒 undefined。 */
export function runtimeIconUrl(moduleID: string): string | undefined {
  return resolved.value[moduleID]
}

/** 该模块是否已坐实"提取成功"（轮盘位图轨判定：未坐实前按矢量轨着色）。 */
export function runtimeIconReady(moduleID: string): boolean {
  return resolved.value[moduleID] !== undefined
}

/** 该模块是否已坐实失败（诊断/测试用；渲染面不依赖本态，回落天然成立）。 */
export function runtimeIconFailed(moduleID: string): boolean {
  return failed.has(moduleID)
}

/**
 * 幂等发起一次提取：成功写入 data URL（引用替换触发 <img> 升级），失败入负
 * 缓存不再重试（载荷重装/换版本由应用重启自然恢复）。返回 Promise 仅供
 * 测试与诊断 await，渲染面不感知。
 */
export function ensureRuntimeIcon(moduleID: string): Promise<void> {
  if (!moduleID || runtimeIconReady(moduleID) || failed.has(moduleID)) {
    return Promise.resolve()
  }
  const busy = inflight.get(moduleID)
  if (busy) return busy
  const p = (async () => {
    try {
      const { RuntimeIconService } = await import('../../bindings/hanxi/internal/app')
      const b64 = await RuntimeIconService.IconPNG(moduleID)
      if (typeof b64 === 'string' && b64.length > 0) {
        resolved.value = { ...resolved.value, [moduleID]: `data:image/png;base64,${b64}` }
        return
      }
      failed.add(moduleID)
    } catch {
      failed.add(moduleID)
    } finally {
      inflight.delete(moduleID)
    }
  })()
  inflight.set(moduleID, p)
  return p
}

/** 测试复位钩子：清三态缓存，防跨用例串扰。 */
export function __resetRuntimeIconsForTest(): void {
  resolved.value = {}
  failed.clear()
  inflight.clear()
}
