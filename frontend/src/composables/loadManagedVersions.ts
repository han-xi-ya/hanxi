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
 * 托管版本页的本地优先加载：本地资产与远程清单各自完成即写回。
 * 远程请求挂起或失败时不会阻塞已安装版本和当前版本；任一分支失败均保留旧数据。
 */
export async function loadManagedVersions<Remote, Local>(sources: ManagedVersionSources<Remote, Local>): Promise<void> {
  sources.setLoading(true)
  sources.setError('')

  const localTask = Promise.all([sources.local(), sources.active()])
    .then(([local, active]) => {
      sources.setLocal(local ?? [])
      sources.setActive(active ?? '')
    })
    .catch((error: unknown) => {
      sources.setError(`读取本地版本失败: ${getErrorMessage(error)}`)
    })

  void Promise.resolve(sources.remote())
    .then(remote => sources.setRemote(remote ?? []))
    .catch((error: unknown) => {
      sources.setError(`获取远程版本列表失败: ${getErrorMessage(error)}`)
    })
    .finally(() => sources.setLoading(false))

  await localTask
}
