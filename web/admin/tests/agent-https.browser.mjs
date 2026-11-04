// Local Vite + mocked API regression; no production backend or external calls.
// ADMIN_TEST_URL, PLAYWRIGHT_MODULE and CHROME_EXECUTABLE may be supplied.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
const base = process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5668/admin/'
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname))
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const en = JSON.parse(await readFile(new URL('../src/i18n/en.json', import.meta.url)))
const tr = key => key.split('.').reduce((v, k) => v[k], en)
const defaults = { enabled: true, url: 'https://panel.example.com:2096', listen: ':2096', certificate_id: 1, revision: 'revision-1' }
async function setup(locale = 'en', width = 1440, role = 'admin') {
  const context = await browser.newContext({ viewport: { width, height: 1100 }, serviceWorkers: 'block' })
  await context.addInitScript(language => { localStorage.setItem('i18nextLng', language) }, locale)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { settings: { ...defaults }, reads: 0, puts: [], failRead: false, conflict: false, errors: [], unexpected: [] }
  const view = () => ({ settings: state.settings, ready: true, base_url: 'https://panel.example.com', in_container: true, status: { state: 'listening', active_url: state.settings.url, active_listen: state.settings.listen, certificate_id: 1, certificate_not_after: '2030-01-01T00:00:00Z' } })
  await context.route('**/*', async route => {
    const r = route.request(), u = new URL(r.url())
    if (u.origin !== new URL(base).origin) return route.abort()
    if (!u.pathname.startsWith('/api/')) return route.continue()
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
    if (u.pathname === '/api/admin/settings/agent-https') {
      if (r.method() === 'GET') { state.reads++; return state.failRead ? send({ error: 'Fixture read failure' }, 503) : send(view()) }
      if (r.method() === 'PUT') {
        const body = r.postDataJSON(); state.puts.push(body)
        if (state.conflict) return send({ error: 'Fixture conflict' }, 409)
        state.settings = { ...body, revision: 'revision-2' }; return send(view())
      }
    }
    if (r.method() !== 'GET') { state.unexpected.push(`${r.method()} ${u.pathname}`); return send({}, 500) }
    switch (u.pathname) {
      case '/api/admin/dashboard': return send({})
      case '/api/admin/me': return send({ id: 1, email: 'admin@example.com', role, version: 'v1.54.0' })
      case '/api/site': return send({ name: 'Captain', branding: {}, theme: {} })
      case '/api/admin/system/update': return send({ captain: { current: 'v1.54.0' } })
      case '/api/admin/certificates': return send({ certificates: [{ id: 1, name: 'Panel certificate', names: ['panel.example.com'], not_after: '2030-01-01T00:00:00Z' }] })
      case '/api/admin/settings/acme': return send({ email: '', has_cloudflare_token: true })
      case '/api/admin/settings/connlog': return send({ enabled: false, retention_days: 7, max_per_user: 100 })
      case '/api/admin/settings/audit': return send({ enabled: false })
      case '/api/admin/settings/dynlimit': return send({ enabled: false, rules: [] })
      case '/api/admin/audit-rules': case '/api/admin/audit-log': case '/api/admin/nodes': return send([])
      case '/api/admin/nodes/domain-check': return send({ domain: '', valid: true, unchanged: true, conflicts: [] })
      case '/api/admin/nodes/layout': return send({ revision: 'layout-1', groups: [], nodes: [] })
      case '/api/admin/nodes/agent-https': return send({ enabled: true, url: state.settings.url, ready: true })
      case '/api/admin/domains': return send({ domains: [], can_issue: true, global_token: true })
      default: state.unexpected.push(`${r.method()} ${u.pathname}`); return send({ error: 'Unexpected fixture request' }, 500)
    }
  })
  const page = await context.newPage(); page.setDefaultTimeout(10000)
  page.on('pageerror', error => state.errors.push(error.message))
  return { context, page, state }
}
async function finish(run) { assert.deepEqual(run.state.errors, []); assert.deepEqual(run.state.unexpected, []); await run.context.close() }
try {
  const run = await setup(), { page, state } = run
  await page.goto(`${base}settings/category/proxy#agent-https`)
  const card = page.locator('#agent-https'), address = card.getByLabel(tr('agentHTTPS.url'), { exact: true })
  await address.waitFor(); assert.equal(await address.inputValue(), defaults.url)
  await card.getByText(tr('agentHTTPS.status.listening'), { exact: true }).waitFor()
  assert.ok((await card.textContent()).includes('--agent-https-port 2096'))
  await address.fill('https://panel.example.com:2087')
  const reads = state.reads
  await page.waitForFunction(() => !!document.querySelector('input'))
  await new Promise(resolve => setTimeout(resolve, 5500))
  assert.ok(state.reads > reads)
  assert.equal(await address.inputValue(), 'https://panel.example.com:2087', 'poll must preserve draft')
  assert.ok((await card.textContent()).includes('--agent-https-port 2096'), 'commands must use saved port')
  state.conflict = true
  await card.getByRole('button', { name: 'Save Agent HTTPS access', exact: true }).click()
  await card.getByText(tr('agentHTTPS.conflict')).waitFor()
  assert.equal(await address.inputValue(), 'https://panel.example.com:2087')
  assert.equal(state.puts[0].revision, 'revision-1')
  await card.getByRole('button', { name: tr('agentHTTPS.reload') }).click()
  await page.waitForFunction(() => [...document.querySelectorAll('#agent-https input')].some(el => el.value === 'https://panel.example.com:2096'))
  assert.equal(await address.inputValue(), defaults.url)
  state.conflict = false
  await address.fill('https://panel.example.com:2087')
  await card.locator('summary').click()
  await card.getByLabel(tr('agentHTTPS.listen'), { exact: true }).fill(':2087')
  await card.getByRole('button', { name: 'Save Agent HTTPS access', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('#agent-https')?.textContent.includes('--agent-https-port 2087'))
  assert.equal(state.settings.revision, 'revision-2')
  assert.equal(state.settings.listen, ':2087')
  state.failRead = true
  await card.getByText(tr('agentHTTPS.status.unknown'), { exact: true }).waitFor()
  state.failRead = false
  await card.getByText(tr('agentHTTPS.status.listening'), { exact: true }).waitFor()
  await run.context.setOffline(true)
  await card.getByText(tr('agentHTTPS.status.unknown'), { exact: true }).waitFor()
  await run.context.setOffline(false)
  await finish(run)

  const failed = await setup()
  failed.state.failRead = true
  await failed.page.goto(`${base}settings/category/proxy#agent-https`)
  await failed.page.locator('#agent-https').getByText(tr('workspace.loadError')).waitFor()
  assert.equal(await failed.page.locator('#agent-https input').count(), 0, 'Failed initial read must not render editable defaults')
  await finish(failed)

  for (const locale of ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko']) {
    const run = await setup(locale, locale === 'en' ? 1440 : 390)
    await run.page.goto(`${base}settings/category/proxy#agent-https`)
    const card = run.page.locator('#agent-https')
    await card.locator('input').first().waitFor()
    await run.page.waitForFunction(() => [...document.querySelectorAll('#agent-https input')].some(el => el.value.includes('example.com')))
    const widths = await card.evaluate(el => ({ scroll: el.scrollWidth, client: el.clientWidth, inputs: [...el.querySelectorAll('input')].filter(x => x.type === 'text').map(x => x.getBoundingClientRect().width) }))
    assert.ok(widths.scroll <= widths.client + 2, `${locale} horizontal overflow`)
    assert.ok(widths.inputs.every(w => w > 180), `${locale} inputs too narrow`)
    if (process.env.AGENT_HTTPS_SCREENSHOTS && ['en', 'zh-CN'].includes(locale)) await card.screenshot({ path: `${process.env.AGENT_HTTPS_SCREENSHOTS}/${locale}.png` })
    await finish(run)
  }

  const node = await setup()
  await node.page.goto(`${base}nodes`)
  await node.page.getByRole('button', { name: tr('nodes.create'), exact: true }).click()
  const dialog = node.page.getByRole('dialog')
  await dialog.getByRole('button', { name: tr('agentHTTPS.useManaged') }).click()
  assert.equal(await dialog.getByLabel(tr('agentConnection.url'), { exact: true }).inputValue(), defaults.url)
  assert.equal(await dialog.getByLabel(tr('agentConnection.source'), { exact: true }).inputValue(), tr('agentConnection.captain'))
  await dialog.getByLabel(tr('agentConnection.url'), { exact: true }).fill('https://other.example.com:2087')
  node.state.settings.url = 'https://changed.example.com:2096'
  await new Promise(resolve => setTimeout(resolve, 10500))
  assert.equal(await dialog.getByLabel(tr('agentConnection.url'), { exact: true }).inputValue(), 'https://other.example.com:2087')
  await finish(node)

  const forbidden = await setup('en', 1440, 'operator')
  await forbidden.page.goto(`${base}settings/category/proxy`)
  await forbidden.page.waitForURL(url => url.pathname === '/admin' || url.pathname === '/admin/')
  assert.equal(forbidden.state.reads, 0)
  await finish(forbidden)
  console.log('Agent HTTPS browser regression passed: six locales, desktop/mobile, drafts, conflict, stale/offline, node choice, permissions')
} finally { await browser.close() }
