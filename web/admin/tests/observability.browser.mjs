// Browser fault/edge regression. API responses are synthetic; external network
// is blocked. Real-backend evidence is collected separately and labeled as such.
import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const base = new URL(process.env.OBS_UI_URL || 'http://127.0.0.1:5681/admin/')
assert.ok(['127.0.0.1', 'localhost'].includes(base.hostname))
const artifacts = process.env.OBS_UI_ARTIFACTS || '/tmp/captain-observability-ui'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const dictionaries = Object.fromEntries(await Promise.all(['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru'].map(async lang => [lang, JSON.parse(await readFile(new URL(`../src/i18n/${lang}.json`, import.meta.url), 'utf8'))])))
let currentPage
const checks = [], iso = value => new Date(value).toISOString()
const historyFixture = () => {
  const ended = Date.now() - 3600000, started = ended - 60000, received = Date.now() - 5000
  const source = { core: 'xray', counter: 'users', started_at: iso(started), ended_at: iso(ended), duration_ms: 60000, quality: 'observed', segments: 1, reasons: [] }
  return { retention: { days: 7, max_rows: 12000, global_max_rows: 100000 }, items: ['partial', 'observed', 'legacy', 'invalid', 'unknown'].map((quality, i) => ({ id: 10 - i, epoch: 'a'.repeat(32), sequence: String(1793232000000000001n + BigInt(i)), received_at: iso(received - i * 1000), accounting_day: iso(received).slice(0, 10), up_bytes: 1024, down_bytes: 2048, totals_known: i !== 3, discarded_samples: i === 0 ? 2 : 0, metadata_status: ['legacy', 'invalid'].includes(quality) ? quality : 'valid', ...(!['legacy', 'invalid'].includes(quality) ? { sample: { version: 1, collection_started_at: iso(ended - 10), collection_ended_at: iso(ended), segments: 1, quality, reasons: quality === 'partial' ? ['counter_error'] : quality === 'unknown' ? ['no_running_sources'] : [], sources: quality === 'unknown' ? null : [{ ...source, quality, reasons: quality === 'partial' ? ['counter_error'] : [] }] } } : {}) })), delivery: { sampled_at: iso(received), received_at: iso(received + 1000), fresh_for_ms: 1000, pending_bytes: 3072, buffered_bytes: 1024, journal_bytes: 8192 } }
}
async function setup(lang = 'en', width = 1440) {
  const context = await browser.newContext({ viewport: { width, height: 1000 }, serviceWorkers: 'block' })
  await context.addInitScript(lang => localStorage.setItem('i18nextLng', lang), lang)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { requests: [], errors: [], external: [], unexpected: [], leases: new Set(), tombstones: new Set(), next: 0, createError: 0, readError: 0, historyError: 0, interfacesNull: false, history: historyFixture() }
  const dict = dictionaries[lang], t = key => key.split('.').reduce((v, k) => v[k], dict)
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    state.requests.push({ method, path })
    const send = (value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
    if (path === '/api/admin/me') return send({ id: 1, email: 'admin@example.com', role: 'admin' })
    if (path === '/api/site') return send({ name: 'Captain local fixture', branding: {}, theme: {} })
    if (path === '/api/admin/settings/notice') return send({ enabled: false, title: '', body: '' })
    if (path === '/api/admin/system/update') return send({ captain: { current: 'v1.51.0', latest: 'v1.51.0', has_update: false } })
    if (path === '/api/admin/nodes/1') return send({ node: { id: 1, name: 'Example diagnostic server', hostname: 'server.example.com', version: 'v0.86.0', paired: true, online: true, proxy_allowed: true, mode: 'full' }, status: { cores: {}, certs: [] }, inbounds: [], ingresses: [], traffic: {} })
    if (path === '/api/admin/nodes/1/traffic-history') return state.historyError ? send({ error: 'synthetic history failure' }, state.historyError) : send(state.history)
    if (path.startsWith('/api/admin/node-diagnostics/1')) {
      const id = method === 'POST' ? request.postDataJSON().request_id : path.split('/leases/')[1] || url.searchParams.get('lease_id')
      if (method === 'DELETE') { state.leases.delete(id); state.tombstones.add(id); return route.fulfill({ status: 204 }) }
      if (method === 'POST' || method === 'PUT') {
        if (state.createError) return send({ error: 'synthetic lease failure' }, state.createError)
        if (state.tombstones.has(id)) return send({ error: 'lease_not_found' }, 404)
        state.leases.add(id); return send({ lease_id: id, ttl_ms: 30000, renew_after_ms: 10000, interval_ms: 1000 })
      }
      if (state.readError) return send({ error: 'synthetic read failure' }, state.readError)
      const sequence = ++state.next
      return send({ node_id: 1, status: 'live', interval_ms: 1000, fresh_for_ms: 3000, epoch: 'synthetic-epoch', latest_sequence: sequence, samples: [{ epoch: 'synthetic-epoch', sequence, sampled_at: iso(Date.now()), elapsed_ms: 1000, cpu_percent: 12.5, memory_used: 268435456, memory_total: 1073741824, net_up_bytes_per_second: 1024, net_down_bytes_per_second: 2048, truncated: false, interfaces: state.interfacesNull ? null : [{ name: 'synthetic-interface-with-a-long-name', rx_bytes_per_second: 2048, tx_bytes_per_second: 1024 }] }] })
    }
    state.unexpected.push({ method, path }); return send({ error: 'unexpected synthetic route' }, 500)
  })
  const page = await context.newPage(); currentPage = page; page.setDefaultTimeout(10000); page.on('pageerror', error => state.errors.push(error.message))
  const open = view => page.goto(new URL(`nodes/1/diagnostics?view=${view}`, base).href)
  const text = key => page.getByText(t(key), { exact: true })
  const button = key => page.getByRole('button', { name: t(key), exact: true })
  const tab = key => page.getByRole('link', { name: t(`serverDetail.diagnostics.${key}`), exact: true })
  const until = async predicate => { const end = Date.now() + 5000; while (!predicate()) { assert.ok(Date.now() < end, 'condition timed out'); await page.waitForTimeout(50) } }
  const checkLayout = async name => { await page.evaluate(() => window.scrollTo(0, 0)); await page.waitForTimeout(250); const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1); assert.equal(overflow, false, name); await page.screenshot({ path: resolve(artifacts, `${name}.png`), fullPage: true }); checks.push(name) }
  const clean = () => { assert.deepEqual(state.errors, []); assert.deepEqual(state.external, []); assert.deepEqual(state.unexpected, []) }
  return { context, page, state, t, open, text, button, tab, until, checkLayout, clean }
}
try {
  const f = await setup()
  await f.open('traffic-history'); await f.page.getByRole('heading', { name: f.t('trafficHistory.title'), exact: true }).waitFor(); assert.equal(f.state.leases.size, 0)
  for (let i = 0; i < 5; i++) { await f.page.locator('.mantine-Accordion-control').nth(i).click(); await f.page.getByText('a'.repeat(32) + ' / ' + String(1793232000000000001n + BigInt(i)), { exact: false }).waitFor() }
  await f.page.locator('.mantine-Accordion-control').first().click(); await f.page.getByText(f.t('trafficHistory.delayUnknown'), { exact: false }).first().waitFor(); await f.checkLayout('mock-desktop-history-partial')
  assert.ok((await f.page.locator('body').innerText()).includes('1793232000000000001'))
  await f.tab('live').click(); await f.text('liveDiagnostics.status.live').waitFor(); await f.until(() => f.state.leases.size === 1); await f.checkLayout('mock-desktop-live')
  await f.button('liveDiagnostics.stop').click(); await f.until(() => f.state.leases.size === 0); await f.text('liveDiagnostics.status.stopped').waitFor()
  await f.button('liveDiagnostics.start').click(); await f.text('liveDiagnostics.status.live').waitFor()
  // Explicit browser visibility fault injection: headless tabs may all remain visible.
  await f.page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' }); document.dispatchEvent(new Event('visibilitychange')) })
  await f.until(() => f.state.leases.size === 0); await f.text('liveDiagnostics.status.inactive').waitFor()
  await f.page.evaluate(() => { delete document.visibilityState; document.dispatchEvent(new Event('visibilitychange')) })
  await f.text('liveDiagnostics.status.live').waitFor(); await f.until(() => f.state.leases.size === 1)
  await f.context.setOffline(true); await f.text('liveDiagnostics.status.inactive').waitFor(); await f.until(() => f.state.leases.size === 0)
  await f.context.setOffline(false); await f.text('liveDiagnostics.status.live').waitFor()
  f.state.readError = 403; await f.text('liveDiagnostics.loadError').waitFor(); await f.until(() => f.state.leases.size === 0)
  f.state.readError = 0; await f.button('liveDiagnostics.start').click(); await f.text('liveDiagnostics.status.live').waitFor()
  await f.tab('traffic-history').click(); await f.until(() => f.state.leases.size === 0)
  f.state.history = { items: [], retention: f.state.history.retention }; await f.button('trafficHistory.latest').click(); await f.text('trafficHistory.empty').waitFor(); await f.checkLayout('mock-desktop-history-empty')
  f.state.historyError = 403; await f.button('trafficHistory.latest').click(); await f.text('trafficHistory.loadError').waitFor(); await f.checkLayout('mock-desktop-history-403')
  f.clean(); await f.context.close(); checks.push('mock-start-stop-hidden-offline-read-error-navigation-cleanup')
  for (const [status, key] of [[409, 'unsupported'], [403, 'loadError'], [429, 'budget']]) {
    const f = await setup('en', 390); f.state.createError = status
    await f.open('live'); await f.text(`liveDiagnostics.${key}`).waitFor(); await f.until(() => f.state.leases.size === 0); await f.checkLayout(`mock-mobile-lease-${status}`)
    assert.equal(await f.button('liveDiagnostics.start').isEnabled(), true); f.clean(); await f.context.close()
  }
  for (const lang of Object.keys(dictionaries)) {
    const f = await setup(lang, 390); f.state.interfacesNull = true
    await f.open('live'); await f.text('liveDiagnostics.status.live').waitFor(); await f.checkLayout(`mock-mobile-${lang}-live-null-interfaces`)
    await f.tab('traffic-history').click(); await f.until(() => f.state.leases.size === 0)
    await f.page.locator('.mantine-Accordion-control').first().click(); await f.checkLayout(`mock-mobile-${lang}-history`)
    f.clean(); await f.context.close()
  }
  await writeFile(resolve(artifacts, 'mock-browser-results.json'), JSON.stringify({ status: 'passed', backend: 'synthetic HTTP interception; not real agent evidence', visibility: 'explicit visibilitychange injection in headless browser', offline: 'Playwright offline browser emulation', external_requests: 0, checks }, null, 2) + '\n')
  console.log(JSON.stringify({ status: 'passed', simulated_backend: true, checks: checks.length, artifacts }))
} catch (error) { await currentPage?.screenshot({ path: resolve(artifacts, 'mock-failure.png'), fullPage: true }).catch(() => {}); throw error } finally { await browser.close() }
