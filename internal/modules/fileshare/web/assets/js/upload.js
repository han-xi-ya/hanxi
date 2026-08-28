import { streamUploadURL } from './api.js'
import { formatBytes, showToast } from './ui.js'

class UploadCancelledError extends Error {
  constructor() {
    super('已取消')
    this.name = 'UploadCancelledError'
  }
}

export function createUploader({ elements, getCurrentPath, onComplete, getConfig }) {
  const { dropzone, fileInput, task, fileName, percent, progress, progressFill, status, summary, cancelButton, limit } = elements
  const context = { active: false, cancelled: false, xhr: null }

  function setProgress(value) {
    const normalized = Math.max(0, Math.min(100, value || 0))
    progressFill.style.width = `${normalized.toFixed(1)}%`
    progress.setAttribute('aria-valuenow', normalized.toFixed(1))
    percent.textContent = `${Math.round(normalized)}%`
  }

  function setBusy(busy) {
    context.active = busy
    dropzone.classList.toggle('is-disabled', busy)
    fileInput.disabled = busy
    cancelButton.classList.toggle('hidden', !busy)
    task.classList.toggle('hidden', !busy && !status.textContent)
  }

  function cancel() {
    if (!context.active || context.cancelled) return
    context.cancelled = true
    status.textContent = '正在取消上传…'
    context.xhr?.abort()
  }

  function uploadFile(file, targetPath, onProgress) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      context.xhr = xhr
      xhr.open('POST', streamUploadURL({ dir: targetPath, name: file.name, size: file.size }), true)
      xhr.setRequestHeader('Content-Type', 'application/octet-stream')

      let lastLoaded = 0
      let lastAt = Date.now()
      let smoothedRate = 0
      let lastPaintAt = 0
      xhr.upload.onprogress = event => {
        const now = Date.now()
        const elapsed = (now - lastAt) / 1000
        if (elapsed > 0 && event.loaded >= lastLoaded) {
          const currentRate = (event.loaded - lastLoaded) / elapsed
          smoothedRate = smoothedRate > 0 ? smoothedRate * 0.65 + currentRate * 0.35 : currentRate
          lastLoaded = event.loaded
          lastAt = now
        }
        if (now - lastPaintAt < 100 && event.loaded < file.size) return
        lastPaintAt = now
        const total = event.lengthComputable && event.total > 0 ? event.total : file.size
        onProgress(event.loaded, total, smoothedRate)
      }

      const clearCurrentXHR = () => {
        if (context.xhr === xhr) context.xhr = null
      }
      xhr.onload = () => {
        clearCurrentXHR()
        if (context.cancelled) return reject(new UploadCancelledError())
        if (xhr.status >= 200 && xhr.status < 300) return resolve()
        reject(new Error(xhr.responseText || `HTTP ${xhr.status}`))
      }
      xhr.onerror = () => {
        clearCurrentXHR()
        reject(new Error('网络传输中断，单次流式上传需要从头重试'))
      }
      xhr.onabort = () => {
        clearCurrentXHR()
        reject(context.cancelled ? new UploadCancelledError() : new Error('上传请求已中止'))
      }
      xhr.timeout = 60000
      xhr.ontimeout = () => {
        clearCurrentXHR()
        reject(new Error('上传连接超时，请检查网络后重试'))
      }
      xhr.send(file)
    })
  }

  async function uploadFiles(fileList) {
    if (context.active || !fileList?.length) return
    const files = Array.from(fileList)
    const targetPath = getCurrentPath()
    const config = getConfig()
    let succeeded = 0
    let failed = 0

    context.cancelled = false
    task.classList.remove('hidden')
    status.textContent = '正在准备上传'
    summary.textContent = `0 / ${files.length}`
    setProgress(0)
    setBusy(true)

    try {
      for (let index = 0; index < files.length; index += 1) {
        if (context.cancelled) break
        const file = files[index]
        fileName.textContent = `${index + 1}/${files.length} · ${file.name}`
        status.textContent = '正在建立传输连接…'
        setProgress(0)
        try {
          if (file.size <= 0) throw new Error('文件为空')
          if (config.maxUploadSizeMB > 0 && file.size > config.maxUploadSizeMB * 1024 * 1024) {
            throw new Error(`文件超过 ${config.maxUploadSizeMB} MB 上传限制`)
          }
          await uploadFile(file, targetPath, (loaded, total, rate) => {
            setProgress(total > 0 ? loaded / total * 100 : 0)
            status.textContent = `${formatBytes(loaded)} / ${formatBytes(file.size)} · ${formatBytes(rate)}/s`
          })
          succeeded += 1
          setProgress(100)
          status.textContent = `${file.name} 上传完成`
        } catch (error) {
          if (error instanceof UploadCancelledError) {
            status.textContent = `${file.name} 已取消，服务端会清理未完成文件`
            break
          }
          failed += 1
          status.textContent = `${file.name} 上传失败：${error.message || error}`
          showToast(`上传失败：${file.name} · ${error.message || error}`, 'error')
        }
        summary.textContent = `成功 ${succeeded} · 失败 ${failed} · 共 ${files.length}`
      }
    } finally {
      context.xhr = null
      setBusy(false)
      fileInput.value = ''
      summary.textContent = `成功 ${succeeded} · 失败 ${failed} · 共 ${files.length}`
      if (!context.cancelled) {
        status.textContent = failed ? '本批次上传已结束，请检查失败项目' : '全部文件上传完成'
        if (!failed) showToast(`已上传 ${succeeded} 个文件`, 'success')
      }
      await onComplete(targetPath)
    }
  }

  ;['dragenter', 'dragover'].forEach(name => dropzone.addEventListener(name, event => {
    event.preventDefault()
    if (!context.active) dropzone.classList.add('dragover')
  }))
  ;['dragleave', 'drop'].forEach(name => dropzone.addEventListener(name, event => {
    event.preventDefault()
    dropzone.classList.remove('dragover')
  }))
  dropzone.addEventListener('drop', event => uploadFiles(event.dataTransfer.files))
  fileInput.addEventListener('change', event => uploadFiles(event.target.files))
  cancelButton.addEventListener('click', cancel)

  return {
    updateConfig(config) {
      limit.textContent = config.maxUploadSizeMB > 0
        ? `单文件上限 ${config.maxUploadSizeMB} MB · 支持批量上传`
        : '支持批量与超大文件流式上传'
    },
  }
}
