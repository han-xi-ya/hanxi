<script setup lang="ts">
import { ref, shallowRef, onMounted } from 'vue'
import QRCode from 'qrcode'
import * as WifiAPI from '../../bindings/hubkit/internal/modules/wifi'
import type { Profile } from '../../bindings/hubkit/internal/modules/wifi/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

const { showToast } = useToast()

const loading = ref(false)
const profiles = shallowRef<Profile[]>([])
const errorMsg = ref('')

// 选中的二维码弹窗数据
const qrModal = ref<{
  visible: boolean
  ssid: string
  password: string
  svg: string
}>({
  visible: false,
  ssid: '',
  password: '',
  svg: '',
})

// 生成标准 WiFi 二维码字符串协议: WIFI:T:WPA;S:ssid;P:password;;
function escapeWifiString(str: string): string {
  return str.replace(/([\\;,:"])/g, '\\$1')
}

function buildWifiQRString(ssid: string, password: string): string {
  const encSSID = escapeWifiString(ssid)
  const isNoPassword = !password || password === '未设置密码或无法读取'
  if (isNoPassword) {
    return `WIFI:T:nopass;S:${encSSID};;`
  }
  const encPwd = escapeWifiString(password)
  return `WIFI:T:WPA;S:${encSSID};P:${encPwd};;`
}

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

// 展示二维码弹窗
async function showQRCode(p: Profile) {
  try {
    const qrText = buildWifiQRString(p.ssid, p.password)
    const svg = await QRCode.toString(qrText, {
      type: 'svg',
      margin: 1,
      width: 220,
      color: {
        dark: '#1f2328',
        light: '#ffffff',
      },
    })
    qrModal.value = {
      visible: true,
      ssid: p.ssid,
      password: p.password,
      svg,
    }
  } catch (e: unknown) {
    showToast(`生成二维码失败: ${getErrorMessage(e)}`)
  }
}

function closeQRModal() {
  qrModal.value.visible = false
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
        <p class="subtitle">查看本机已保存的 Wi-Fi 密码，支持手机扫码秒连</p>
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
              <th style="width: 120px; text-align: center;">操作</th>
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
                <div class="actions">
                  <button
                    class="btn-icon"
                    title="扫码连 WiFi"
                    @click="showQRCode(p)"
                  >
                    📱
                  </button>
                  <button
                    v-if="p.password && p.password !== '未设置密码或无法读取'"
                    class="btn-icon"
                    title="复制密码"
                    @click="copyPassword(p)"
                  >
                    📋
                  </button>
                </div>
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

    <!-- 扫码连 WiFi 二维码弹窗 -->
    <div v-if="qrModal.visible" class="modal-overlay" @click.self="closeQRModal">
      <div class="modal-card">
        <div class="modal-header">
          <h3>📱 手机扫码连接 Wi-Fi</h3>
          <button class="btn-close" @click="closeQRModal">✕</button>
        </div>
        <div class="modal-body">
          <div class="qr-container" v-html="qrModal.svg"></div>
          <div class="qr-info">
            <div class="info-row">
              <span class="label">Wi-Fi (SSID)：</span>
              <strong class="val">{{ qrModal.ssid }}</strong>
            </div>
            <div class="info-row">
              <span class="label">密码：</span>
              <span class="val pw">{{ qrModal.password || '无密码' }}</span>
            </div>
          </div>
          <p class="qr-tip">支持 iOS / Android 相机、微信扫一扫直接接入网络</p>
        </div>
        <div class="modal-footer">
          <button class="btn btn-secondary btn-sm" @click="closeQRModal">关闭</button>
        </div>
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

.actions {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
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

/* 二维码弹窗 */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  backdrop-filter: blur(2px);
}

.modal-card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  width: 320px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.2);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  animation: popIn 0.18s ease-out;
}

@keyframes popIn {
  from {
    opacity: 0;
    transform: scale(0.95);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-color);
}
.modal-header h3 {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
}

.btn-close {
  background: transparent;
  border: none;
  font-size: 14px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 2px 6px;
  border-radius: 4px;
}
.btn-close:hover {
  background: var(--bg-hover);
  color: var(--text-main);
}

.modal-body {
  padding: 20px 16px 12px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}

.qr-container {
  background: #ffffff;
  padding: 8px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
  display: flex;
  justify-content: center;
  align-items: center;
}

.qr-info {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 13px;
  background: var(--bg-app);
  padding: 8px 12px;
  border-radius: 6px;
}

.info-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  word-break: break-all;
}

.info-row .label {
  color: var(--text-muted);
  font-size: 12px;
  flex-shrink: 0;
}

.qr-tip {
  font-size: 11px;
  color: var(--text-muted);
  margin: 0;
  text-align: center;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  padding: 8px 16px;
  border-top: 1px solid var(--border-color);
  background: var(--bg-app);
}
</style>
