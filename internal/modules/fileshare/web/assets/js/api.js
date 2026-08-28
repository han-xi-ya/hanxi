async function request(url, options) {
  const response = await fetch(url, options)
  if (!response.ok) {
    const message = await response.text().catch(() => response.statusText)
    throw new Error(message || `HTTP ${response.status}`)
  }
  return response
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
