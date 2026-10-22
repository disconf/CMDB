const state = { records: [] }
const loginView = document.querySelector('#loginView')
const appView = document.querySelector('#appView')
const loginError = document.querySelector('#loginError')
const issueError = document.querySelector('#issueError')

async function api(path, options = {}) {
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
  })
  if (response.status === 401 && !path.endsWith('/login')) {
    showLogin()
  }
  const data = response.status === 204 ? null : await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(data?.message || `请求失败 (${response.status})`)
  return data
}

function showLogin() {
  appView.classList.add('hidden')
  loginView.classList.remove('hidden')
}

function showApp(username) {
  loginView.classList.add('hidden')
  appView.classList.remove('hidden')
  document.querySelector('#adminName').textContent = username
}

function escapeHTML(value) {
  return String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char])
}

function formatTime(value) {
  if (!value) return '永久'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function toast(message) {
  const element = document.querySelector('#toast')
  element.textContent = message
  element.classList.add('show')
  window.setTimeout(() => element.classList.remove('show'), 2200)
}

function isExpired(record) {
  return record.expiresAt && new Date(record.expiresAt).getTime() <= Date.now()
}

function renderRecords() {
  const body = document.querySelector('#recordsBody')
  document.querySelector('#emptyRecords').classList.toggle('hidden', state.records.length > 0)
  body.innerHTML = state.records.map(record => `
    <div class="record-row" data-id="${escapeHTML(record.id)}">
      <div class="record-main"><strong>${escapeHTML(record.id)}</strong><span>${escapeHTML(record.customer)}</span></div>
      <code title="${escapeHTML(record.deploymentId)}">${escapeHTML(record.deploymentId)}</code>
      <strong class="capacity">${Number(record.maxAssets).toLocaleString('zh-CN')}</strong>
      <time>${escapeHTML(formatTime(record.expiresAt))}<br><span class="status-pill ${isExpired(record) ? 'expired' : ''}">${isExpired(record) ? '已到期' : record.expiresAt ? '有效' : '永久'}</span></time>
      <span>${escapeHTML(record.issuedBy)}</span>
      <div class="actions"><button data-action="copy">复制</button><a data-action="download" href="/api/licenses/${encodeURIComponent(record.id)}/download">下载</a></div>
    </div>
  `).join('')
  body.querySelectorAll('[data-action="copy"]').forEach(button => {
    button.addEventListener('click', async () => {
      const id = button.closest('.record-row').dataset.id
      const record = state.records.find(item => item.id === id)
      if (record) await copyLicense(record.licenseKey)
    })
  })
}

async function loadDashboard() {
  const [records, stats] = await Promise.all([api('/api/licenses'), api('/api/stats')])
  state.records = records
  document.querySelector('#statTotal').textContent = stats.totalLicenses
  document.querySelector('#statActive').textContent = stats.activeLicenses
  document.querySelector('#statExpiring').textContent = stats.expiringSoon
  document.querySelector('#statCapacity').textContent = Number(stats.issuedCapacity).toLocaleString('zh-CN')
  renderRecords()
}

async function copyLicense(value) {
  await navigator.clipboard.writeText(value)
  toast('许可证已复制')
}

async function initialize() {
  try {
    const session = await api('/api/session')
    if (!session.authenticated) throw new Error('not signed in')
    showApp(session.username)
    await loadDashboard()
  } catch {
    showLogin()
  }
}

document.querySelector('#loginForm').addEventListener('submit', async event => {
  event.preventDefault()
  loginError.textContent = ''
  const button = event.currentTarget.querySelector('button')
  button.disabled = true
  try {
    const session = await api('/api/login', {
      method: 'POST',
      body: JSON.stringify({
        username: document.querySelector('#loginUsername').value,
        password: document.querySelector('#loginPassword').value,
      }),
    })
    showApp(session.username)
    await loadDashboard()
  } catch (reason) {
    loginError.textContent = reason.message
  } finally {
    button.disabled = false
  }
})

document.querySelector('#issueForm').addEventListener('submit', async event => {
  event.preventDefault()
  issueError.textContent = ''
  const form = event.currentTarget
  const button = form.querySelector('button[type="submit"]')
  button.disabled = true
  try {
    const record = await api('/api/licenses', {
      method: 'POST',
      body: JSON.stringify({
        customer: document.querySelector('#customer').value,
        deploymentId: document.querySelector('#deploymentId').value,
        edition: document.querySelector('#edition').value,
        maxAssets: Number(document.querySelector('#maxAssets').value),
        validityDays: Number(document.querySelector('#validityDays').value),
        features: document.querySelector('#features').value.split(','),
        notes: document.querySelector('#notes').value,
      }),
    })
    showResult(record)
    form.reset()
    await loadDashboard()
    toast('许可证已签发')
  } catch (reason) {
    issueError.textContent = reason.message
  } finally {
    button.disabled = false
  }
})

function showResult(record) {
  document.querySelector('#resultId').textContent = record.id
  document.querySelector('#resultCustomer').textContent = record.customer
  document.querySelector('#resultAssets').textContent = Number(record.maxAssets).toLocaleString('zh-CN')
  document.querySelector('#resultKey').value = record.licenseKey
  document.querySelector('#downloadResult').href = `/api/licenses/${encodeURIComponent(record.id)}/download`
  document.querySelector('#resultModal').classList.remove('hidden')
}

document.querySelector('#copyResult').addEventListener('click', async () => {
  await copyLicense(document.querySelector('#resultKey').value)
})

document.querySelector('#closeResult').addEventListener('click', () => {
  document.querySelector('#resultModal').classList.add('hidden')
})

document.querySelector('#refreshButton').addEventListener('click', async () => {
  try {
    await loadDashboard()
    toast('记录已刷新')
  } catch (reason) {
    toast(reason.message)
  }
})

document.querySelector('#logoutButton').addEventListener('click', async () => {
  try { await api('/api/logout', { method: 'POST' }) } catch {}
  showLogin()
})

initialize()
