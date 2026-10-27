// Mocked-API server draft regression. Start the integrated admin app first:
//   pnpm dev --host 127.0.0.1 --port 5634
// Run: node tests/node-drafts.browser.mjs
// Optional: ADMIN_TEST_URL, PLAYWRIGHT_MODULE, CHROME_EXECUTABLE,
// SETTINGS_SOURCE_ROOT (web/admin directory when testing another worktree).
// All API calls use synthetic fixtures; external requests and WebSockets are
// blocked. No backend login, credentials, or real server writes are used.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5634/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Use a local admin server only')
const dict = JSON.parse(await readFile(resolve(source, 'src/i18n/en.json'), 'utf8'))
const t = key => { const text = key.split('.').reduce((value, part) => value?.[part], dict); assert.equal(typeof text, 'string', key); return text }
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' })
await context.addInitScript(() => localStorage.setItem('i18nextLng', 'en'))
await context.routeWebSocket('**/*', socket => socket.close())
const state = { writes: [], unexpected: [], external: [], errors: [], blockerWarnings: [] }
const nodes = [1, 2].map(id => ({ id, name: id === 1 ? 'Example full server' : 'Example monitor server', hostname: 'server.example.com', public_addr: '192.0.2.10', internal_addr: '', v6_addr: '', domain: '', monitor_url: '', paired: true, online: true, proxy_allowed: id === 1, mode: id === 1 ? 'full' : 'monitor', monitor_group: '', version: 'v0.82.0' }))
const supplier = { id: 1, name: 'Example hosting', url: 'https://hosting.example.com' }
const probes = Object.fromEntries(nodes.map(node => [node.id, {
  infrastructure_revision: 'fixture-1', infrastructure_linked: false, infrastructure_supplier_linked: true, region_countries: ['US'], storage_candidates: [], billed: 0,
  probe: { node_id: node.id, hidden: false, region_mode: 'off', region_detection: {}, info: { provider: supplier.name, provider_url: supplier.url, note: 'Saved note', billing: { amount: '12.5', currency: 'USD', cycle: 'monthly' }, price: 'USD 12.5 / month', expires_at: '2028-01-31', auto_renewal: { enabled: true, anchor_date: '2028-01-31', anchor_day: 31 }, traffic_reset: { mode: 'manual' }, location_settings: { mode: 'off' } }, limit_bytes: 0, reset_day: 1, mode: 'sum', used_up: 0, used_down: 0, prev_used: 0, period_start: '' },
}]))
// Unrelated charts are deliberately unavailable; they must not prevent editing.
const unavailableNodeReads = new Set(['availability', 'network-quality', 'resource-history'])
await context.route('**/*', async route => {
  const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
  if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
  if (!path.startsWith('/api/')) {
    if (method === 'GET' || method === 'HEAD') return route.continue()
    state.unexpected.push({ method, path }); return route.abort()
  }
  const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
  const match = path.match(/^\/api\/admin\/nodes\/(1|2)(?:\/(.*))?$/)
  if (method === 'PUT' && match?.[2] === 'probe') {
    const body = request.postDataJSON(), value = probes[Number(match[1])]
    state.writes.push({ method, path, body })
    assert.equal(body.InfrastructureRevision, value.infrastructure_revision)
    value.probe = { ...value.probe, hidden: body.Hidden, info: { ...body.Info, auto_renewal: { ...value.probe.info.auto_renewal, ...body.Info.auto_renewal } }, limit_bytes: body.LimitBytes, reset_day: body.ResetDay, mode: body.Mode }
    return send(value)
  }
  if (method !== 'GET') { state.unexpected.push({ method, path }); return send({ error: 'Unexpected mocked write' }, 500) }
  if (match) {
    const id = Number(match[1]), suffix = match[2]
    if (!suffix) return send({ node: nodes[id - 1], status: { cores: {} }, inbounds: [], ingresses: [], traffic: {} })
    if (suffix === 'probe') return send(probes[id])
    if (suffix === 'overrides' || suffix === 'metadata' || suffix === 'core-options') return send({})
    if (suffix === 'routing') return send({ outbounds: [], routes: [], default_outbound: '' })
    if (suffix === 'reverse-connections') return send([])
    if (suffix === 'forwards') return send({ forwards: [], status: {} })
    if (suffix === 'network-diagnostics') return send([])
    if (suffix === 'return-routes') return send({ schedule: { enabled: false, interval_hours: 6, city: 'beijing', family: 'ipv4' }, history: [], tool_supported: false, tool_online: false })
    if (suffix === 'addresses') return send({ auto_ipv4: false, auto_ipv6: false, entry_ipv4: '192.0.2.10', entry_ipv6: '', observed: { received_at: 0, ipv4: {}, ipv6: {} } })
    if (suffix === 'dns-health') return send({ node_id: id, node: nodes[id - 1].name, checked_at: 0, stale: true, targets: [], failure_count: 0 })
    if (unavailableNodeReads.has(suffix)) return send({ error: 'Chart unavailable in draft fixture' }, 503)
  }
  const relation = path.match(/^\/api\/admin\/infrastructure\/nodes\/(1|2)$/)
  if (relation) { const value = probes[Number(relation[1])]; return send({ revision: value.infrastructure_revision, supplier, asset: null, billing: value.probe.info.billing, price: value.probe.info.price, expires_at: value.probe.info.expires_at, auto_renewal: value.probe.info.auto_renewal }) }
  if (/^\/api\/admin\/settings\/nodes\/[12]\/egress$/.test(path)) return send({ enabled: false, upstreams: [] })
  if (/^\/api\/admin\/settings\/nodes\/[12]\/wireguard-connections$/.test(path)) return send([])
  if (/^\/api\/admin\/settings\/nodes\/[12]\/wireguard-readiness$/.test(path)) return send({ error: 'No WireGuard capability in fixture' }, 503)
  switch (path) {
    case '/api/admin/me': return send({ id: 1, email: 'admin@example.com', role: 'admin' })
    case '/api/site': return send({ name: 'Captain fixture', branding: {}, theme: {} })
    case '/api/admin/system/update': return send({ captain: { current: 'v1.45.0', latest: 'v1.45.0', has_update: false, release_build: true, has_backup: false, in_container: false } })
    case '/api/admin/groups': return send([])
    case '/api/admin/nodes': return send(nodes)
    case '/api/admin/nodes/layout': return send({ revision: 'layout-1', groups: [], nodes: [] })
    case '/api/admin/node-access': return send({ revision: 'access-1', groups: [], plans: [], inbounds: [] })
    case '/api/admin/entries': case '/api/admin/config-presets': return send([])
    case '/api/admin/infrastructure/suppliers': return send([supplier])
    case '/api/admin/settings/infrastructure': return send({ assets: [], suppliers: [supplier], paid_totals: [] })
    case '/api/admin/settings/infrastructure/payments': return send({ items: [], total: 0 })
    case '/api/admin/settings/notice': return send({ enabled: false, title: '', body: '' })
    case '/api/admin/settings/site': return send({ name: 'Captain fixture', branding: {}, theme: {}, node_ids: [], hero_title: 'Example', hero_subtitle: '', features: [], faq: [], show_plans: false })
    default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected mocked read' }, 500)
  }
})
const page = await context.newPage()
page.setDefaultTimeout(10_000)
page.on('pageerror', error => state.errors.push(error.message))
page.on('console', message => { if (message.text().includes('only supports one blocker')) state.blockerWarnings.push(message.text()) })
const note = () => page.getByLabel(t('nodeProbe.note'), { exact: true })
const amount = () => page.getByLabel(t('nodeCost.amount'), { exact: true })
const probeSave = () => note().locator('xpath=ancestor::form').getByRole('button', { name: t('common.save'), exact: true })
const dirty = () => page.getByRole('status').filter({ hasText: t('workspace.unsaved') })
const dialog = () => page.getByRole('dialog', { name: t('workspace.unsaved'), exact: true })
const suppliersLink = () => page.getByRole('link', { name: t('infraLink.manageSuppliers'), exact: true })
const settingsLink = () => page.getByRole('link', { name: t('nav.settings'), exact: true })
async function openNode(id) { await page.goto(new URL(`nodes/${id}`, base).href); await note().waitFor(); await probeSave().waitFor({ state: 'visible' }); assert.equal(await probeSave().isEnabled(), true) }
async function saveProbe(id, clean = true) {
  const response = page.waitForResponse(r => new URL(r.url()).pathname === `/api/admin/nodes/${id}/probe` && r.request().method() === 'PUT')
  await probeSave().click(); assert.equal((await response).status(), 200)
  await dirty().waitFor({ state: clean ? 'hidden' : 'visible' })
}
async function cancelNavigation(link, id) {
  await link.click(); await dialog().getByText(t('workspace.leaveHint'), { exact: true }).waitFor()
  await dialog().getByRole('button', { name: t('workspace.stay'), exact: true }).click()
  assert.equal(new URL(page.url()).pathname, new URL(`nodes/${id}`, base).pathname)
}
try {
  await openNode(1)
  await note().fill('Unsaved operations note'); await amount().fill('19.5'); await dirty().waitFor()
  for (const link of [suppliersLink(), settingsLink()]) {
    await cancelNavigation(link, 1)
    assert.equal(await note().inputValue(), 'Unsaved operations note')
    assert.equal(await amount().inputValue(), '19.5')
  }
  assert.equal(state.writes.length, 0)
  await amount().fill('12.5'); await saveProbe(1)
  assert.equal(probes[1].probe.info.note, 'Unsaved operations note')
  assert.equal(probes[1].probe.info.billing.amount, '12.5')
  await suppliersLink().click(); await page.waitForURL('**/infrastructure?tab=suppliers'); assert.equal(await dialog().count(), 0)
  await openNode(1); await settingsLink().click(); await page.waitForURL('**/settings/category/site'); assert.equal(await dialog().count(), 0)

  await openNode(1)
  const overrideSection = page.locator('[data-section=overrides]')
  await overrideSection.getByRole('button').first().click()
  const xray = overrideSection.getByLabel('Xray', { exact: true })
  const override = { log: { loglevel: 'warning' } }
  await xray.fill(JSON.stringify(override)); await note().fill('Saved independently'); await saveProbe(1, false)
  await cancelNavigation(suppliersLink(), 1)
  assert.deepEqual(JSON.parse(await xray.inputValue()), override, 'Saving probe must preserve the sibling override draft')
  assert.equal(probes[1].probe.info.note, 'Saved independently')
  await suppliersLink().click(); await dialog().getByRole('button', { name: t('workspace.discard'), exact: true }).click(); await page.waitForURL('**/infrastructure?tab=suppliers')
  await openNode(1); await overrideSection.getByRole('button').first().click(); assert.equal(await xray.inputValue(), '', 'Discarded override must not return on re-entry')
  assert.equal(await note().inputValue(), 'Saved independently')
  assert.equal(state.writes.some(write => write.path.endsWith('/overrides')), false)

  await openNode(2)
  assert.equal(await page.locator('[data-section=overrides]').count(), 0, 'Monitor-only servers have no proxy editor')
  await note().fill('Monitor-only unsaved note'); await amount().fill('21'); await dirty().waitFor()
  await cancelNavigation(settingsLink(), 2)
  assert.equal(await note().inputValue(), 'Monitor-only unsaved note'); assert.equal(await amount().inputValue(), '21')
  assert.equal(probes[2].probe.info.note, 'Saved note', 'Canceled navigation must not save monitor-only data')
  assert.equal(state.writes.length, 2)
  assert.deepEqual(state.unexpected, [], 'All API requests must be handled by explicit synthetic fixtures')
  assert.deepEqual(state.external, [], 'No external network requests')
  assert.deepEqual(state.errors, [], 'No runtime JavaScript errors')
  assert.deepEqual(state.blockerWarnings, [], 'Server forms share exactly one navigation blocker')
  console.log('Server draft browser regression passed: supplier/settings cancellation, saved navigation, sibling override preservation/discard, and monitor-only drafts')
} finally { await browser.close() }
