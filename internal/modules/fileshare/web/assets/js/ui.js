export function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, index)
  return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`
}

export function formatSpeed(bytesPerSecond) {
  return `${formatBytes(bytesPerSecond)}/s`
}

export function formatDate(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function showToast(message, type = 'info') {
  const container = document.getElementById('toastContainer')
  const toast = document.createElement('div')
  toast.className = `toast toast-${type}`
  toast.setAttribute('role', type === 'error' ? 'alert' : 'status')
  toast.textContent = message
  container.appendChild(toast)
  window.setTimeout(() => toast.remove(), 3400)
}

const buttonContents = new WeakMap()

export function setButtonBusy(button, busy, busyText) {
  if (busy) {
    buttonContents.set(button, Array.from(button.childNodes, node => node.cloneNode(true)))
    button.textContent = busyText
    button.disabled = true
    button.setAttribute('aria-busy', 'true')
    return
  }
  const content = buttonContents.get(button)
  if (content) button.replaceChildren(...content)
  buttonContents.delete(button)
  button.disabled = false
  button.removeAttribute('aria-busy')
}

export function renderState(container, { title, description = '', error = false, retryLabel = '', onRetry }) {
  container.replaceChildren()
  const state = document.createElement('div')
  state.className = 'state-message'

  if (!error) {
    const spinner = document.createElement('span')
    spinner.className = 'spinner'
    spinner.setAttribute('aria-hidden', 'true')
    state.appendChild(spinner)
  }

  const heading = document.createElement('strong')
  heading.textContent = title
  state.appendChild(heading)

  if (description) {
    const paragraph = document.createElement('p')
    paragraph.textContent = description
    state.appendChild(paragraph)
  }

  if (retryLabel && onRetry) {
    const retry = document.createElement('button')
    retry.type = 'button'
    retry.className = 'button button-secondary button-small'
    retry.textContent = retryLabel
    retry.addEventListener('click', onRetry)
    state.appendChild(retry)
  }

  container.appendChild(state)
}

export function createIcon(kind, folder = false) {
  const wrapper = document.createElement('span')
  wrapper.className = `file-icon${folder ? ' is-folder' : ''}`
  wrapper.setAttribute('aria-hidden', 'true')
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('viewBox', '0 0 24 24')
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path')
  const paths = {
    folder: 'M3 6h7l2 2h9v11H3z',
    image: 'M4 5h16v14H4zM8 14l3-3 5 5M15 9h.01',
    video: 'M4 6h12v12H4zM16 10l4-2v8l-4-2',
    audio: 'M9 18V6l9-2v12M6 18a3 2 0 1 0 3 0M15 16a3 2 0 1 0 3 0',
    archive: 'M5 4h14v16H5zM10 4v5h4V4M10 13h4',
    code: 'M9 8l-4 4 4 4M15 8l4 4-4 4',
    document: 'M6 3h8l4 4v14H6zM14 3v5h5M9 13h6M9 17h6',
  }
  path.setAttribute('d', paths[folder ? 'folder' : kind] || paths.document)
  svg.appendChild(path)
  wrapper.appendChild(svg)
  return wrapper
}

export function fileKind(extension) {
  const ext = String(extension || '').toLowerCase()
  if (['.png', '.jpg', '.jpeg', '.gif', '.svg', '.webp'].includes(ext)) return 'image'
  if (['.mp4', '.mkv', '.avi', '.mov', '.webm'].includes(ext)) return 'video'
  if (['.mp3', '.wav', '.flac', '.aac'].includes(ext)) return 'audio'
  if (['.zip', '.rar', '.7z', '.tar', '.gz'].includes(ext)) return 'archive'
  if (['.json', '.go', '.js', '.ts', '.vue', '.html', '.css', '.py', '.rs'].includes(ext)) return 'code'
  return 'document'
}
