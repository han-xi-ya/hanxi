import { getErrorMessage } from '../utils/errors'

type ManagedVersionSources<Remote, Local> = {
  remote: () => PromiseLike<Remote[] | null | undefined>
  local: () => PromiseLike<Local[] | null | undefined>
  active: () => PromiseLike<string | null | undefined>
  setRemote: (items: Remote[]) => void
  setLocal: (items: Local[]) => void
  setActive: (version: string) => void
  setLoading: (loading: boolean) => void
  setError: (message: string) => void
}

/**
 * 起调数据源并把**同步抛出**（如非 Wails 环境绑定缺席）归一为 Promise 拒绝：
 * 裸调用在求值位抛出会穿透本函数，loading 永挂 true、两侧错误都不落账。
 * 非抛出路径经 Promise.resolve 原样透传。
 */
function attempt<T>(run: () => PromiseLike<T> | T): Promise<T> {
  try {
    return Promise.resolve(run())
  } catch (error) {
    return Promise.reject(error)
  }
}

/**
 * 托管版本页的本地优先加载：本地资产与远程清单各自完成即写回。
 * 远程请求挂起或失败时不会阻塞已安装版本和当前版本；任一分支失败均保留旧数据。
 */
export async function loadManagedVersions<Remote, Local>(sources: ManagedVersionSources<Remote, Local>): Promise<void> {
  sources.setLoading(true)
  sources.setError('')

  const localTask = attempt(() => Promise.all([sources.local(), sources.active()] as const))
    .then(([local, active]) => {
      sources.setLocal(local ?? [])
      sources.setActive(active ?? '')
    })
    .catch((error: unknown) => {
      sources.setError(`读取本地版本失败: ${getErrorMessage(error)}`)
    })

  void attempt(() => sources.remote())
    .then(remote => sources.setRemote(remote ?? []))
    .catch((error: unknown) => {
      sources.setError(`获取远程版本列表失败: ${getErrorMessage(error)}`)
    })
    .finally(() => sources.setLoading(false))

  // 保持“本地优先返回”：调用方可先呈现已装与 active；store 层以请求代次
  // 包住全部 setter，负责丢弃旧远程响应与旧请求的 loading 收尾。
  await localTask
}
