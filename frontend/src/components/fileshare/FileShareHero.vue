<script setup lang="ts">
// 快传 hero 面板：标题、运行状态灯、端口徽标与启停主按钮。
// 纯展示壳——启停动作 emit 回视图编排层（业务调用集中在 useFileShareServer）。
import type { ServerStatus } from '../../../bindings/hanxi/internal/modules/fileshare/models'

defineProps<{
  status: ServerStatus
  /** 服务未运行时展示的兜底端口（原模板 configForm.port）。 */
  fallbackPort: number
  loading: boolean
}>()

const emit = defineEmits<{ toggle: [] }>()
</script>

<template>
  <header class="hero-panel">
    <div class="hero-copy">
      <div class="eyebrow">LOCAL TRANSFER HUB</div>
      <h1 class="page-title">
        <span class="title-icon" aria-hidden="true">⇄</span>
        局域网文件快传
      </h1>
      <p class="page-desc">
        零客户端依赖的局域网极速文件/文本分享站。手机电脑同一 Wi-Fi 扫码即用，支持大文件单次流式拖拽上传。
      </p>
      <div class="hero-meta">
        <span class="status-pill" :class="{ online: status.isRunning }">
          <span class="status-indicator" :class="{ online: status.isRunning }"></span>
          {{ status.isRunning ? '服务运行中' : '服务已停止' }}
        </span>
        <span class="hero-port font-mono">端口 :{{ status.port || fallbackPort }}</span>
      </div>
    </div>
    <button
      type="button"
      class="btn btn-primary hero-action"
      :class="{ 'btn-danger': status.isRunning }"
      :disabled="loading"
      @click="emit('toggle')"
    >
      <span v-if="loading">处理中...</span>
      <span v-else-if="status.isRunning">■ 停止快传服务</span>
      <span v-else>▶ 启动快传服务</span>
    </button>
  </header>
</template>

<style scoped>
/* 原私有 .btn-primary 全形（含 :disabled 降透明）与全局 :where(.btn)/.btn-primary 同语义，
   模板补挂 .btn 基座后 scoped 副本删净落回；hero 仅保留 CTA 差异档（.hero-action）与
   "运行中变红"覆盖档（.btn-danger，hover 保持红底不吃全局 primary-hover）。 */
.btn-danger {
  background: var(--state-danger);
}
.btn-danger:hover:not(:disabled) {
  background: var(--state-danger);
}

.status-indicator {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--state-danger);
}

.status-indicator.online {
  background: var(--state-positive);
  box-shadow: 0 0 8px var(--state-positive-glow);
}

.hero-panel {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 28px;
  padding: 26px 28px;
  margin-bottom: 18px;
  overflow: hidden;
  background:
    radial-gradient(circle at 85% 0%, var(--state-information-soft), transparent 36%),
    linear-gradient(135deg, var(--surface-panel), var(--surface-soft));
  border: 1px solid var(--color-border);
  border-radius: 16px;
  box-shadow: var(--shadow-panel);
}

.hero-copy {
  min-width: 0;
}

.eyebrow {
  margin-bottom: 7px;
  color: var(--color-primary);
  font-size: var(--text-micro);
  font-weight: 800;
  letter-spacing: 0.16em;
}

/* hero 标题（原两代级联 22px→27px 死重声明已并为生效终值；27→--text-3xl 阶梯顶档） */
.page-title {
  margin: 0 0 8px;
  gap: 10px;
  font-size: var(--text-3xl);
  font-weight: 700;
  color: var(--color-text);
  display: flex;
  align-items: center;
  letter-spacing: -0.02em;
}

.title-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  color: var(--color-on-primary);
  font-size: var(--text-2xl);
  background: linear-gradient(135deg, var(--color-primary), var(--state-information));
  border-radius: 10px;
  box-shadow: 0 7px 16px var(--color-primary-glow);
}

.page-desc {
  max-width: 760px;
  margin: 0;
  font-size: var(--text-base);
  color: var(--color-text-muted);
  line-height: 1.65;
}

.hero-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 15px;
}

.status-pill,
.hero-port {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 5px 9px;
  color: var(--color-text-muted);
  font-size: var(--text-xs);
  font-weight: 600;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 999px;
}

.status-pill.online {
  color: var(--state-positive);
  background: var(--state-positive-soft);
  border-color: var(--state-positive-soft);
}

/* hero CTA 差异档：全局 .btn 基座之上的大尺寸与悬浮微抬（radius 9→全局 8 落回） */
.hero-action {
  min-width: 150px;
  padding: 10px 17px;
  box-shadow: 0 7px 16px var(--color-primary-soft);
  transition: transform 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.hero-action:hover:not(:disabled) {
  transform: translateY(-1px);
}

.hero-action:focus-visible {
  outline: 2px solid var(--color-primary-glow);
  outline-offset: 2px;
}

@media (max-width: 760px) {
  .hero-panel {
    align-items: stretch;
    flex-direction: column;
  }

  .hero-action {
    width: 100%;
  }
}

@media (max-width: 460px) {
  .hero-meta {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
