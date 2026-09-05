<script setup lang="ts">
// 接入点页签：服务停止时展示空态；运行中为每个局域网网卡渲染二维码访问卡。
// 纯展示壳——复制动作 emit 回视图编排层（剪贴板两级策略与 toast 在 useFileShareServer）。
import type { NetworkEndpoint } from '../../../bindings/hanxi/internal/modules/fileshare/models'

defineProps<{
  /** 原 status.isRunning。 */
  isRunning: boolean
  endpoints: NetworkEndpoint[]
  /** 端点二维码 SVG 缓存 (url -> svg)，原模板 v-html 注入点不变。 */
  qrMap: Record<string, string>
}>()

const emit = defineEmits<{ copy: [url: string] }>()
</script>

<template>
  <div class="tab-content">
    <div v-if="!isRunning" class="empty-state py-8">
      <div class="empty-icon">⏹</div>
      <h3>快传服务当前处于停止状态</h3>
      <p>点击右上角的「▶ 启动快传服务」后即可生成各局域网网卡的访问地址与专属二维码。</p>
    </div>

    <div v-else class="endpoint-grid">
      <div v-for="ep in endpoints" :key="ep.url" class="card endpoint-card">
        <div class="card-header flex-between">
          <div class="ep-name font-bold">
            {{ ep.interfaceName }}
            <span v-if="ep.isDefault" class="tag-pill tag-primary ml-2">主通信网卡</span>
          </div>
          <span class="ep-ip font-mono text-muted">{{ ep.ip }}</span>
        </div>

        <div class="ep-body flex-between">
          <div class="ep-qr" v-html="qrMap[ep.url] || '生成中...'"></div>
          <div class="ep-info">
            <div class="ep-url-box">
              <span class="url-text font-mono">{{ ep.url }}</span>
            </div>
            <p class="ep-tip">
              📱 手机/平板连接同局域网后，打开相机或浏览器扫码秒开共享站。
            </p>
            <div class="ep-actions flex gap-2">
              <button class="btn-secondary btn-sm" @click="emit('copy', ep.url)">
                📋 复制访问链接
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 以下样式自 FileShareView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动。
   §9.6-3 治理：与兄弟页签逐字同形的方言原子（flex-between/gap-2/ml-2/btn-sm/tag-pill/
   tag-blue）已上收 components.css；.btn-secondary 家族/.empty-state/.py-8/.text-muted/
   .empty-icon 因同名不同形或有未定义使用点，按裁决保留局部。 */
.endpoint-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(420px, 1fr));
  gap: 16px;
}

.endpoint-card {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  padding: 16px;
}

.ep-body {
  display: flex;
  gap: 16px;
  align-items: center;
  margin-top: 12px;
}

.ep-qr {
  background: var(--surface-panel);
  padding: 8px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 140px;
  height: 140px;
}

.ep-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ep-url-box {
  background: var(--surface-soft);
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid var(--color-border);
  word-break: break-all;
}

.url-text {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-primary);
}

.ep-tip {
  font-size: 12px;
  color: var(--color-text-muted);
  line-height: 1.4;
}

/* .flex-between/.gap-2/.ml-2 已上收 components.css（§9.6-3）；
   .py-8 留局部：MemoView 存在无定义同名使用点，裸收会经全局 .empty-state 互染 padding */
.py-8 { padding-top: 32px; padding-bottom: 32px; }

.btn-secondary {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 12px;
  cursor: pointer;
}

.btn-secondary:hover {
  background: var(--surface-soft);
}

/* .btn-sm/.tag-pill 已上收 components.css（§9.6-3） */
.tag-primary { background: var(--state-information-soft); color: var(--color-primary-hover); }

/* .text-muted 留局部：HomeView 同名副本色值不同形（muted vs subtle），待裁决 */
.text-muted { color: var(--color-text-subtle); }

.empty-state {
  text-align: center;
  color: var(--color-text-subtle);
}

.empty-icon {
  font-size: 36px;
  margin-bottom: 8px;
}

.workspace-body > .tab-content > .card,
.endpoint-card {
  border-radius: 12px;
  box-shadow: none;
}

.endpoint-grid {
  grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
  gap: 14px;
}

.endpoint-card {
  padding: 18px;
}

.ep-qr {
  min-width: 132px;
  width: 132px;
  height: 132px;
  box-shadow: inset 0 0 0 1px var(--color-border);
}

.btn-secondary {
  transition: transform 0.15s ease, border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.btn-secondary:hover:not(:disabled) {
  transform: translateY(-1px);
}

.btn-secondary:focus-visible {
  outline: 2px solid var(--color-primary-glow);
  outline-offset: 2px;
}

.empty-state {
  padding: 44px 20px;
  background: var(--surface-panel);
  border: 1px dashed var(--color-border);
  border-radius: 12px;
}

.empty-state h3 {
  margin: 0 0 7px;
  color: var(--color-text);
  font-size: 15px;
}

.empty-state p {
  max-width: 620px;
  margin: 0 auto;
  font-size: 12px;
  line-height: 1.6;
}

@media (max-width: 760px) {
  .ep-body {
    align-items: stretch;
    flex-direction: column;
  }

  .ep-qr {
    width: 100%;
  }
}
</style>
