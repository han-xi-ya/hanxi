<script setup lang="ts">
// 接入点页签：服务停止时展示空态；运行中为每个局域网网卡渲染二维码访问卡。
// 纯展示壳——复制动作 emit 回视图编排层（剪贴板两级策略与 toast 在 useFileShareServer）。
import type { NetworkEndpoint } from '../../../bindings/hanxi/internal/modules/fileshare/models'

defineProps<{
  /** 快传服务是否在运行（停止态页签只出空态）。 */
  isRunning: boolean
  endpoints: NetworkEndpoint[]
  /** 端点二维码 SVG 缓存 (url -> svg)，模板经 v-html 注入。 */
  qrMap: Record<string, string>
}>()

const emit = defineEmits<{ copy: [url: string] }>()
</script>

<template>
  <div class="tab-content">
    <div v-if="!isRunning" class="empty-state">
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
          <span class="ep-ip font-mono text-subtle">{{ ep.ip }}</span>
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
              <button class="btn btn-secondary btn-small" @click="emit('copy', ep.url)">
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
/* 原子归一在册（§9.6-3/§9.6-10）：与兄弟页签逐字同形的方言原子
   （flex-between/gap-2/ml-2/btn-sm/tag-pill/tag-blue）走全局 components.css；
   .empty-state/.empty-icon 系同名不同形或有未定义使用点，按裁决保留局部。 */
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
  border-radius: var(--radius-chip);
  border: 1px solid var(--color-border);
  word-break: break-all;
}

.url-text {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--color-primary);
}

.ep-tip {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  line-height: 1.4;
}

/* .flex-between/.gap-2/.ml-2 走全局 components.css 原子（§9.6-3）；
   .py-8 副本曾被后位 .empty-state 的 padding 级联压死（32→44px，生效即死码），挂点已删
   ——§9.6-10 的 .py-8 冻结项在 FileShare 侧清零 */

/* 行内钮走全局 .btn .btn-secondary .btn-small 标准三件套，不挂 scoped 全形副本
   （radius 6→8、悬浮微抬与自绘焦点环弃用、hover 落回 surface-hover，§9.6-10 冻结项
   FileShare 侧已解除；全库冻结标记待 MemoView 等外组同形收编后整删）。 */
.tag-primary { background: var(--state-information-soft); color: var(--color-primary-hover); }

/* subtle 档走全局 .text-subtle（§9.6-10 名实裁决：原局部 .text-muted 色值实为 subtle 档），
   本文件不挂副本 */

.empty-icon {
  font-size: var(--text-3xl);
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

.empty-state {
  padding: 44px 20px;
  background: var(--surface-panel);
  border: 1px dashed var(--color-border);
  border-radius: 12px;
}

.empty-state h3 {
  margin: 0 0 7px;
  color: var(--color-text);
  font-size: var(--text-md);
}

.empty-state p {
  max-width: 620px;
  margin: 0 auto;
  font-size: var(--text-sm);
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
