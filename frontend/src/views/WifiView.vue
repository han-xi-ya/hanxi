<script setup lang="ts">
import { ref, shallowRef, onMounted } from 'vue'
import * as WifiAPI from '../../bindings/hubkit/internal/modules/wifi'
import type { Profile } from '../../bindings/hubkit/internal/modules/wifi/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

const { showToast } = useToast()

const loading = ref(false)
const profiles = shallowRef<Profile[]>([])
const errorMsg = ref('')

async function loadProfiles() {
  loading.value = true
  errorMsg.value = ''
  try {
    const list = await WifiAPI.WifiService.ListProfiles()
    profiles.value = list ?? []
  } catch (e: unknown) {
    errorMsg.value = `加载 Wi-Fi 列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

// 复制密码到剪贴板
async function copyPassword(p: Profile) {
  if (!p.password) return
  try {
    await navigator.clipboard.writeText(p.password)
    showToast(`已复制密码: ${p.ssid}`)
  } catch (e: unknown) {
    showToast(`复制失败: ${getErrorMessage(e)}`)
  }
}

onMounted(() => {
  loadProfiles()
})
</script>

<template>
  <section class="page wifi-page">
    <div class="header-row">
      <div>
        <h1>WiFi 密码</h1>
        <p class="subtitle">查看本机已保存的 Wi-Fi 密码</p>
      </div>
      <button class="btn btn-secondary btn-sm" :disabled="loading" @click="loadProfiles">
        {{ loading ? '刷新中…' : '刷新列表' }}
      </button>
    </div>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <!-- 网络密码列表 -->
    <div class="card list-card">
      <div class="card-header">
        <h3>已保存的 Wi-Fi 网络 ({{ profiles.length }})</h3>
      </div>
      <div class="table-wrap">
        <table class="tbl">
          <thead>
            <tr>
              <th>网络名称 (SSID)</th>
              <th>密码</th>
              <th style="width: 80px; text-align: center;">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in profiles" :key="p.ssid">
              <td><strong>{{ p.ssid }}</strong></td>
              <td>
                <span v-if="p.password" class="pw">{{ p.password }}</span>
                <span v-else class="text-muted">—</span>
              </td>
              <td style="text-align: center;">
                <button
                  v-if="p.password && p.password !== '未设置密码或无法读取'"
                  class="btn-icon"
                  title="复制密码"
                  @click="copyPassword(p)"
                >
                  📋
                </button>
              </td>
            </tr>
            <tr v-if="profiles.length === 0 && !loading">
              <td colspan="3" class="empty-hint">
                未发现已保存的 Wi-Fi 网络。<br />
                <span class="hint-sub">若本机无无线网卡或 WLAN 服务未启动，此列表为空属正常。</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.wifi-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.subtitle {
  color: var(--text-muted);
  font-size: 13px;
  margin: 4px 0 0;
}

.card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  overflow: hidden;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-color);
}
.card-header h3 {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
}

.table-wrap {
  overflow-x: auto;
}

.tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  text-align: left;
}
.tbl th {
  background: var(--bg-app);
  padding: 9px 14px;
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border-color);
}
.tbl td {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color);
  color: var(--text-main);
}
.tbl tr:last-child td {
  border-bottom: none;
}

/* 密码明文样式 */
.pw {
  font-family: Consolas, monospace;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-main);
  background: #f6f8fa;
  padding: 2px 10px;
  border-radius: 4px;
  border: 1px dashed var(--border-color);
  user-select: all;
}

.btn {
  padding: 6px 16px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border: 1px solid transparent;
  transition: all 0.15s ease;
}
.btn-sm {
  padding: 4px 10px;
  font-size: 12px;
}
.btn-secondary {
  background: #fff;
  border-color: var(--border-color);
  color: var(--text-main);
}
.btn-secondary:hover {
  background: var(--bg-hover);
}

.btn-icon {
  background: transparent;
  border: none;
  font-size: 14px;
  cursor: pointer;
  padding: 3px 5px;
  border-radius: 4px;
}
.btn-icon:hover {
  background: var(--bg-hover);
}

.error-box {
  padding: 10px 14px;
  background: #ffebe9;
  color: var(--danger);
  border: 1px solid rgba(207, 34, 46, 0.2);
  border-radius: 6px;
  font-size: 13px;
}

.text-muted {
  color: var(--text-subtle);
}

.empty-hint {
  text-align: center;
  padding: 32px 0;
  color: var(--text-subtle);
  line-height: 1.8;
}
.hint-sub {
  font-size: 12px;
  color: var(--text-subtle);
}
</style>
