<script setup lang="ts">
// 联动与辅助设置卡（Wave 5 · 批 0）：随 Hanxi 关闭勾选 + 桌面快捷方式 +
// 数据目录直达 + 仓库地址行（复制/浏览器打开）。四条目全部可选：
// adapter.extras 缺项自动隐藏，整卡无任何条目时由父级（Shell）不渲染本组件。
// 首屏 GetFollowOnExit/RepositoryURL 并发拉取（视图原 loadExtras 语义收编）；
// 勾选失败回滚到后端真实值（ref 变化驱动 checkbox 复位，现状口径）。
import { onMounted, ref } from 'vue'
import type { ManagedModuleAdapter } from './adapter'
import { useToast } from '../../composables/useToast'
import { useClipboard } from '../../composables/useClipboard'
import { getErrorMessage } from '../../utils/errors'

const props = defineProps<{
  adapter: ManagedModuleAdapter
}>()

const { showToast } = useToast()
const { copyWithToast } = useClipboard()

const followOnExit = ref(false)
const repoUrl = ref('')

async function loadExtras() {
  const jobs: Promise<unknown>[] = []
  const follow = props.adapter.extras?.followOnExit
  const repo = props.adapter.extras?.repo
  if (follow) {
    jobs.push(
      Promise.resolve(follow.get()).then((v) => {
        followOnExit.value = v
      }),
    )
  }
  if (repo) {
    jobs.push(
      Promise.resolve(repo.url()).then((u) => {
        repoUrl.value = u
      }),
    )
  }
  try {
    await Promise.all(jobs)
  } catch (e) {
    console.warn('[managed] loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  const spec = props.adapter.extras?.followOnExit
  if (!spec) return
  const next = !followOnExit.value
  followOnExit.value = next // 用户点击已将勾选框翻转，ref 同步跟进，保持绑定状态一致
  try {
    const res = await spec.set(next)
    if (res?.message !== undefined) showToast(res.message)
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function createShortcut() {
  const spec = props.adapter.extras?.shortcut
  if (!spec) return
  try {
    const res = await spec.create()
    if (res?.message !== undefined) showToast(res.message)
  } catch (e) {
    showToast(`创建快捷方式失败: ${getErrorMessage(e)}`)
  }
}

async function openDataDir() {
  const spec = props.adapter.extras?.dataDir
  if (!spec) return
  try {
    const res = await spec.open()
    if (res?.message !== undefined) showToast(res.message)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function copyRepo() {
  if (!repoUrl.value) return
  // 剪贴板两级策略已收编进 useClipboard
  await copyWithToast(repoUrl.value, props.adapter.extras?.repo?.copyToast ?? '仓库地址已复制')
}

async function openRepo() {
  const spec = props.adapter.extras?.repo
  if (!spec) return
  try {
    const res = await spec.open()
    if (res?.message !== undefined) showToast(res.message)
  } catch (e) {
    showToast(`打开失败: ${getErrorMessage(e)}`)
  }
}

onMounted(() => {
  void loadExtras()
})
</script>

<template>
  <div class="extras-card">
    <div class="extras-row">
      <label v-if="adapter.extras?.followOnExit" class="toggle-label">
        <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
        <span>{{ adapter.extras.followOnExit.label ?? '随 Hanxi 一起关闭' }}
          <span class="hint-dim">{{ adapter.extras.followOnExit.note ?? '（关闭后 Hanxi 退出完全不影响该工具）' }}</span>
        </span>
      </label>
      <button v-if="adapter.extras?.shortcut" class="btn btn-secondary btn-small" @click="createShortcut">
        {{ adapter.extras.shortcut.label ?? '🖥 创建桌面快捷方式' }}
      </button>
      <button
        v-if="adapter.extras?.dataDir"
        class="btn btn-secondary btn-small"
        :title="adapter.extras.dataDir.title"
        @click="openDataDir"
      >{{ adapter.extras.dataDir.label }}</button>
      <slot name="extras-action" />
    </div>
    <div v-if="adapter.extras?.repo" class="repo-row">
      <span class="k">{{ adapter.extras.repo.label ?? 'GitHub 仓库' }}</span>
      <code class="mono repo-addr">{{ repoUrl }}</code>
      <button class="link-button" @click="copyRepo">复制</button>
      <button class="link-button" @click="openRepo">浏览器打开</button>
    </div>
  </div>
</template>
