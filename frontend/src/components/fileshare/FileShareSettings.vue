<script setup lang="ts">
// 共享服务设置卡：共享位置 / 网络与容量 / 访问权限三面板 + 保存动作入口。
// 注：form 是视图层 configForm 同一响应式对象的引用——面板字段深绑定（v-model）与原
// 单文件实现「模板直接改写 configForm.value.*」逐字等价，保存/选目录/打开目录等
// 业务动作 emit 回视图编排层（useFileShareServer）执行。
import type { ShareConfig } from '../../../bindings/hanxi/internal/modules/fileshare/models'

const props = defineProps<{
  form: ShareConfig
  /** 服务运行中（原 status.isRunning，热应用提示位）。 */
  isRunning: boolean
  /** 打开目录门禁（原 canOpenShareDirectory：与已保存路径一致才可用）。 */
  canOpen: boolean
  /** 「当前路径尚未保存」提示位（原 normalizedSharePath && !canOpenShareDirectory）。 */
  unsaved: boolean
}>()

const emit = defineEmits<{ save: []; choose: []; open: [] }>()

function setQuickPort(port: number) {
  props.form.port = port
}
</script>

<template>
  <section class="config-card section-gap">
    <div class="section-header">
      <div>
        <div class="section-kicker">SHARING SETTINGS</div>
        <h2 class="section-title">共享服务设置</h2>
        <p class="section-desc">配置对外共享的位置、网络入口与访问权限。</p>
      </div>
      <div class="section-actions">
        <span v-if="isRunning" class="hot-apply-tip">● 保存后实时生效</span>
        <button type="button" class="btn-secondary" @click="emit('save')">保存共享规则</button>
      </div>
    </div>

    <div class="settings-layout">
      <div class="setting-panel location-panel">
        <div class="setting-panel-title">
          <span class="setting-icon" aria-hidden="true">⌂</span>
          <div>
            <h3>共享位置</h3>
            <p>局域网设备只能访问此目录中的内容</p>
          </div>
        </div>
        <label class="field-label" for="share-path">PC 本地物理路径</label>
        <div class="path-input-group">
          <input
            id="share-path"
            v-model="form.sharePath"
            type="text"
            class="input-control font-mono"
            placeholder="请选择或输入要共享的物理文件夹路径..."
          />
          <button type="button" class="btn-secondary path-action" @click="emit('choose')">
            选择目录
          </button>
          <button
            type="button"
            class="btn-secondary path-action open-action"
            :disabled="!canOpen"
            :title="canOpen ? '在系统资源管理器中打开共享目录' : '请先保存当前共享目录'"
            @click="emit('open')"
          >
            打开目录
          </button>
        </div>
        <div class="path-hints">
          <span>🔒 安全沙箱已开启，外部访客无法越界访问。</span>
          <span v-if="unsaved" class="unsaved-hint">当前路径尚未保存</span>
        </div>
      </div>

      <div class="setting-panel network-panel">
        <div class="setting-panel-title">
          <span class="setting-icon" aria-hidden="true">⌁</span>
          <div>
            <h3>网络与容量</h3>
            <p>控制访问端口与单文件大小</p>
          </div>
        </div>
        <div class="network-fields">
          <div class="form-group">
            <div class="field-row">
              <label class="field-label" for="share-port">监听端口</label>
              <div class="quick-ports">
                <button
                  v-for="port in [80, 8080, 8888]"
                  :key="port"
                  type="button"
                  class="quick-port-chip"
                  :class="{ active: form.port === port }"
                  @click="setQuickPort(port)"
                >
                  {{ port }}
                </button>
              </div>
            </div>
            <input id="share-port" v-model.number="form.port" type="number" class="input-control font-mono" min="1" max="65535" />
            <span class="form-hint">端口 80 可直接通过 IP 访问</span>
          </div>
          <div class="form-group">
            <label class="field-label" for="upload-limit">单文件上传上限 (MB)</label>
            <input id="upload-limit" v-model.number="form.maxUploadSizeMB" type="number" class="input-control font-mono" min="0" step="1" placeholder="0" />
            <span class="form-hint">0 表示不限制上传大小</span>
          </div>
        </div>
      </div>

      <div class="setting-panel permissions-panel">
        <div class="setting-panel-title permissions-title">
          <span class="setting-icon" aria-hidden="true">✓</span>
          <div>
            <h3>访问权限</h3>
            <p>按需开放局域网交互能力</p>
          </div>
        </div>
        <div class="permission-list">
          <label class="permission-item">
            <span><strong>允许上传文件</strong><small>支持大文件单次流式上传</small></span>
            <input v-model="form.allowUpload" type="checkbox" />
          </label>
          <label class="permission-item">
            <span><strong>允许文本投递</strong><small>接收手机发送的文本与链接</small></span>
            <input v-model="form.allowTextDrop" type="checkbox" />
          </label>
          <label class="permission-item">
            <span><strong>同步到极客随手记</strong><small>自动保存移动端投递内容</small></span>
            <input v-model="form.autoSaveToMemo" type="checkbox" />
          </label>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 以下样式自 FileShareView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
.config-card {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  padding: 16px 20px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-group label {
  font-size: 13px;
  font-weight: 500;
  color: var(--color-text);
}

.path-input-group {
  display: flex;
  gap: 8px;
  align-items: center;
}

.quick-ports {
  align-items: center;
}

.quick-port-chip {
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  color: var(--color-text-muted);
  font-size: 11px;
  font-family: monospace;
  padding: 1px 6px;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.15s;
}

.quick-port-chip:hover {
  background: var(--color-border);
  color: var(--color-text);
}

.quick-port-chip.active {
  background: var(--color-primary);
  color: var(--color-on-primary);
  border-color: var(--color-primary);
}

.input-control {
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  padding: 8px 12px;
  font-size: 13px;
  color: var(--color-text);
  outline: none;
  transition: border-color 0.2s;
}

.input-control:focus {
  border-color: var(--state-information);
  box-shadow: 0 0 0 2px var(--state-information-soft);
}

.form-hint {
  font-size: 11px;
  color: var(--color-text-subtle);
}

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

.setting-icon {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  color: var(--color-primary);
  font-size: 19px;
  font-weight: 700;
  background: var(--color-primary-soft);
  border-radius: 11px;
}

.section-kicker {
  margin-bottom: 7px;
  color: var(--color-primary);
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.16em;
}

.config-card {
  padding: 0;
  overflow: hidden;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 16px;
  box-shadow: 0 10px 28px var(--shadow-panel);
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 20px 22px;
  border-bottom: 1px solid var(--color-border);
}

.section-title {
  margin: 0;
  color: var(--color-text);
  font-size: 18px;
  font-weight: 750;
}

.section-desc {
  margin: 5px 0 0;
  color: var(--color-text-muted);
  font-size: 12px;
}

.section-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.hot-apply-tip {
  color: var(--state-positive);
  font-size: 11px;
  font-weight: 600;
}

.settings-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.45fr) minmax(320px, 0.9fr);
  gap: 14px;
  padding: 16px;
  background: var(--surface-soft);
}

.setting-panel {
  min-width: 0;
  padding: 18px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 12px;
}

.location-panel { grid-column: 1; }
.network-panel { grid-column: 1; }
.permissions-panel { grid-column: 2; grid-row: 1 / span 2; }

.setting-panel-title {
  display: flex;
  align-items: center;
  gap: 11px;
  margin-bottom: 17px;
}

.setting-panel-title h3 {
  margin: 0 0 3px;
  color: var(--color-text);
  font-size: 14px;
}

.setting-panel-title p {
  margin: 0;
  color: var(--color-text-subtle);
  font-size: 11px;
}

.setting-icon {
  width: 34px;
  height: 34px;
  font-size: 15px;
  border-radius: 9px;
}

.field-label {
  color: var(--color-text);
  font-size: 12px;
  font-weight: 650;
}

.path-input-group {
  align-items: stretch;
  margin-top: 7px;
}

.path-input-group .input-control {
  flex: 1;
  min-width: 100px;
}

.path-action {
  flex: 0 0 auto;
  padding: 8px 12px;
  white-space: nowrap;
}

.open-action {
  color: var(--color-primary-hover);
  border-color: var(--color-primary-glow);
}

.path-hints {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  margin-top: 8px;
  color: var(--color-text-subtle);
  font-size: 11px;
}

.unsaved-hint {
  flex: 0 0 auto;
  color: var(--state-warning);
  font-weight: 600;
}

.network-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}

.field-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.quick-ports {
  display: flex;
  align-items: center;
  gap: 4px;
}

.quick-port-chip {
  padding: 2px 7px;
  border-radius: 999px;
}

.permission-list {
  display: flex;
  flex-direction: column;
  gap: 9px;
}

.permission-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 13px;
  cursor: pointer;
  background: var(--surface-soft);
  border: 1px solid transparent;
  border-radius: 10px;
  transition: border-color 0.18s, background 0.18s;
}

.permission-item:hover {
  border-color: var(--state-information-soft);
}

.permission-item span {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.permission-item strong {
  color: var(--color-text);
  font-size: 12px;
  font-weight: 650;
}

.permission-item small {
  color: var(--color-text-subtle);
  font-size: 10px;
  line-height: 1.4;
}

.permission-item input {
  width: 16px;
  height: 16px;
  accent-color: var(--color-primary);
}

.btn-secondary,
.quick-port-chip {
  transition: transform 0.15s ease, border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.btn-secondary:hover:not(:disabled) {
  transform: translateY(-1px);
}

.btn-secondary:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.btn-secondary:focus-visible,
.quick-port-chip:focus-visible,
.input-control:focus-visible {
  outline: 2px solid var(--color-primary-glow);
  outline-offset: 2px;
}

@media (max-width: 1100px) {
  .settings-layout {
    grid-template-columns: 1fr;
  }

  .location-panel,
  .network-panel,
  .permissions-panel {
    grid-column: 1;
    grid-row: auto;
  }

  .permission-list {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
  }

  .permission-item {
    align-items: flex-start;
  }
}

@media (max-width: 760px) {
  .section-header {
    align-items: stretch;
    flex-direction: column;
  }

  .network-fields,
  .permission-list {
    grid-template-columns: 1fr;
  }

  .section-actions {
    justify-content: space-between;
  }

  .path-input-group {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }

  .path-input-group .input-control {
    grid-column: 1 / -1;
    width: 100%;
  }

  .path-hints {
    align-items: flex-start;
    flex-direction: column;
  }
}

@media (max-width: 460px) {
  .section-actions {
    align-items: flex-start;
    flex-direction: column;
  }

  .section-actions .btn-secondary {
    width: 100%;
  }
}
</style>
