async function request(url, options) {
  const response = await fetch(url, options)
  if (response.status === 401) {
    // 会话过期/口令失效：广播事件让 app.js 重新弹出口令门禁
    window.dispatchEvent(new CustomEvent('fileshare:unauthorized'))
  }
  if (!response.ok) {
    const message = await response.text().catch(() => response.statusText)
    throw new Error(message || `HTTP ${response.status}`)
  }
  return response
}

export async function login(token) {
  return request('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  })
}

export async function getConfig() {
  return (await request('/api/config')).json()
}

export async function getStats() {
  return (await request('/api/stats')).json()
}

export async function listDirectory(path) {
  const params = new URLSearchParams({ path })
  return (await request(`/api/list?${params}`)).json()
}

export async function sendText(content, signal) {
  return request('/api/drop', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content }),
    signal,
  })
}

export function fileOpenURL(path) {
  return `/api/open?${new URLSearchParams({ path })}`
}

export function fileDownloadURL(path) {
  return `/api/download?${new URLSearchParams({ path })}`
}

export function streamUploadURL({ dir, name, size }) {
  return `/api/upload?${new URLSearchParams({ dir, name, size: String(size) })}`
}
