import { fileDownloadURL, fileOpenURL, listDirectory } from './api.js'
import { createIcon, fileKind, formatDate, renderState, showToast } from './ui.js'

export function createFileBrowser({ list, breadcrumb, count, uploadTarget }) {
  let currentPath = ''

  function setCurrentPath(path) {
    currentPath = path || ''
    uploadTarget.textContent = `目标：${currentPath || '根目录'}`
    renderBreadcrumb()
  }

  function renderBreadcrumb() {
    breadcrumb.replaceChildren()
    const entries = [{ label: '根目录', path: '' }]
    let accumulated = ''
    for (const part of currentPath.split('/').filter(Boolean)) {
      accumulated = accumulated ? `${accumulated}/${part}` : part
      entries.push({ label: part, path: accumulated })
    }

    entries.forEach((entry, index) => {
      if (index > 0) {
        const separator = document.createElement('span')
        separator.className = 'breadcrumb-separator'
        separator.textContent = '/'
        separator.setAttribute('aria-hidden', 'true')
        breadcrumb.appendChild(separator)
      }
      const button = document.createElement('button')
      button.type = 'button'
      button.className = 'breadcrumb-button'
      button.dataset.action = 'navigate'
      button.dataset.path = entry.path
      button.textContent = entry.label
      breadcrumb.appendChild(button)
    })
  }

  function renderEntry(entry) {
    const row = document.createElement('article')
    row.className = 'file-row'

    const primary = document.createElement('button')
    primary.type = 'button'
    primary.className = 'file-action'
    primary.dataset.action = entry.isDir ? 'navigate' : 'preview'
    primary.dataset.path = entry.path
    primary.title = entry.isDir ? '进入目录' : '在新标签页中预览'
    primary.appendChild(createIcon(fileKind(entry.ext), entry.isDir))

    const copy = document.createElement('span')
    copy.className = 'file-copy'
    const name = document.createElement('span')
    name.className = 'file-name'
    name.textContent = entry.name
    const description = document.createElement('span')
    description.className = 'file-description'
    const modified = formatDate(entry.modTime)
    description.textContent = entry.isDir ? `文件夹${modified ? ` · ${modified}` : ''}` : `${entry.sizeHuman || '0 B'}${modified ? ` · ${modified}` : ''}`
    copy.append(name, description)
    primary.appendChild(copy)

    const meta = document.createElement('div')
    meta.className = 'file-row-meta'
    const kind = document.createElement('span')
    kind.textContent = entry.isDir ? '目录' : (entry.ext || '文件').replace(/^\./, '').toUpperCase()
    meta.appendChild(kind)

    if (!entry.isDir) {
      const download = document.createElement('a')
      download.className = 'button button-secondary button-small'
      download.href = fileDownloadURL(entry.path)
      download.download = entry.name
      download.textContent = '下载'
      download.title = `下载 ${entry.name}`
      meta.appendChild(download)
    }

    row.append(primary, meta)
    return row
  }

  async function load(path = currentPath) {
    setCurrentPath(path)
    count.textContent = '加载中'
    renderState(list, { title: '正在加载文件' })
    try {
      const entries = await listDirectory(currentPath)
      list.replaceChildren()
      count.textContent = `${entries?.length || 0} 项`
      if (!entries?.length) {
        renderState(list, { title: '当前目录为空', description: '可以将文件拖放到上方上传区域。' })
        return
      }
      const fragment = document.createDocumentFragment()
      entries.forEach(entry => fragment.appendChild(renderEntry(entry)))
      list.appendChild(fragment)
    } catch (error) {
      count.textContent = '加载失败'
      renderState(list, {
        title: '无法加载当前目录',
        description: error.message || String(error),
        error: true,
        retryLabel: '重新加载',
        onRetry: () => load(currentPath),
      })
      showToast(`加载目录失败：${error.message || error}`, 'error')
    }
  }

  function handleAction(event) {
    const target = event.target.closest('[data-action]')
    if (!target) return
    const path = target.dataset.path || ''
    if (target.dataset.action === 'navigate') load(path)
    if (target.dataset.action === 'preview') window.open(fileOpenURL(path), '_blank', 'noopener')
  }

  breadcrumb.addEventListener('click', handleAction)
  list.addEventListener('click', handleAction)

  return {
    load,
    refresh: () => load(currentPath),
    getCurrentPath: () => currentPath,
  }
}
