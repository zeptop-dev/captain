// Local browser regression for the server overview/information/diagnostics split.
// Start the integrated admin app on 127.0.0.1:5662, then run this file with Node.
// Optional: SERVER_DETAIL_TEST_URL, SETTINGS_SOURCE_ROOT (integrated web/admin),
// PLAYWRIGHT_MODULE and CHROME_EXECUTABLE. Every API is mocked; external HTTP
// and WebSockets are blocked. This script never contacts a real Captain backend.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.SERVER_DETAIL_TEST_URL || 'http://127.0.0.1:5662/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Use a local admin server only')
const dict = JSON.parse(await readFile(resolve(source, 'src/i18n/en.json'), 'utf8'))
const t = key => { const text = key.split('.').reduce((value, part) => value?.[part], dict); assert.equal(typeof text, 'string', key); return text }
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const artifacts = process.env.SERVER_DETAIL_ARTIFACTS || '/tmp/captain-server-detail-browser'
await mkdir(artifacts, { recursive: true })
let currentPage
const supplier = { id: 1, name: 'Example hosting', url: 'https://hosting.example.com' }
const timestamp = Date.now()
const host = { cpu_percent: 12, mem_total: 1073741824, mem_used: 268435456, disk_total: 10737418240, disk_used: 2147483648, net_up: 1024, net_down: 2048, resources: { at: timestamp, cpus: [], networks: [], filesystems: [], disks: [], processes: [] } }
const seedNodes = [1, 2, 3].map(id => ({ id, name: ['Example full server', 'Example monitor server', 'Example unpaired server'][id - 1], hostname: 'server.example.com', public_addr: '192.0.2.10', internal_addr: '', v6_addr: '', domain: '', monitor_url: '', paired: id !== 3, online: id !== 3, proxy_allowed: id !== 2, mode: id === 2 ? 'monitor' : 'full', monitor_group: '', version: 'v0.82.0', pair_code: id === 3 ? 'EXAMPLE-PAIR-CODE' : undefined }))

async function setup(role = 'admin', { locale = 'en', scheme = 'light', viewport = { width: 1440, height: 1000 } } = {}) {
  const context = await browser.newContext({ viewport, serviceWorkers: 'block', colorScheme: scheme })
  await context.addInitScript(({ locale, scheme }) => { localStorage.setItem('i18nextLng', locale); localStorage.setItem('mantine-color-scheme-value', scheme) }, { locale, scheme })
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { reads: [], writes: [], unexpected: [], external: [], errors: [], blockerWarnings: [] }
  const nodes = structuredClone(seedNodes)
  const probes = Object.fromEntries(nodes.map(node => [node.id, {
    infrastructure_revision: 'fixture-1', infrastructure_linked: role === 'operator', infrastructure_supplier_linked: true, region_countries: ['US'], storage_candidates: [], billed: 3072,
    live: { at: new Date(timestamp).toISOString(), host },
    probe: { node_id: node.id, hidden: false, region_mode: 'off', region_detection: {}, info: { provider: supplier.name, provider_url: supplier.url, note: 'Saved note', billing: { amount: '12.5', currency: 'USD', cycle: 'monthly' }, price: 'USD 12.5 / month', expires_at: '2028-01-31', auto_renewal: { enabled: true, anchor_date: '2028-01-31', anchor_day: 31 }, traffic_reset: { mode: 'manual' }, location_settings: { mode: 'off' } }, limit_bytes: 1073741824, reset_day: 1, mode: 'sum', used_up: 1024, used_down: 2048, prev_used: 0, period_start: '2027-12-31T00:00:00Z' },
  }]))
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) {
      if (method === 'GET' || method === 'HEAD') return route.continue()
      state.unexpected.push({ method, path }); return route.abort()
    }
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
    const match = path.match(/^\/api\/admin\/nodes\/([123])(?:\/(.*))?$/)
    if (method === 'PUT' && match?.[2] === 'probe') {
      const body = request.postDataJSON(), value = probes[Number(match[1])]
      state.writes.push({ method, path, body })
      if (role === 'admin') assert.equal(body.InfrastructureRevision, value.infrastructure_revision)
      else { assert.equal('InfrastructureRevision' in body, false); assert.equal('SupplierID' in body, false) }
      value.probe = { ...value.probe, hidden: body.Hidden, info: { ...body.Info, auto_renewal: { ...value.probe.info.auto_renewal, ...body.Info.auto_renewal } }, limit_bytes: body.LimitBytes, reset_day: body.ResetDay, mode: body.Mode, resources: body.Resources, storage: body.Storage }
      return send(value)
    }
    if (method !== 'GET') { state.unexpected.push({ method, path }); return send({ error: 'Unexpected mocked write' }, 500) }
    state.reads.push(path)
    if (match) {
      const id = Number(match[1]), suffix = match[2]
      if (!suffix) return send({ node: nodes[id - 1], status: { host, cores: {}, certs: [], doctor: { at: new Date(timestamp).toISOString(), summary: { ok: 1, warn: 0, fail: 0 }, checks: [] } }, inbounds: [], ingresses: [], traffic: {} })
      if (suffix === 'probe') return send(probes[id])
      if (['overrides', 'metadata', 'core-options'].includes(suffix)) return send({})
      if (suffix === 'internal-address-usage') return send({ uses: [] })
      if (suffix === 'routing') return send({ outbounds: [], routes: [], default_outbound: '' })
      if (suffix === 'reverse-connections' || suffix === 'network-diagnostics') return send([])
      if (suffix === 'forwards') return send({ forwards: [], status: {} })
      if (suffix === 'return-routes') return send({ schedule: { enabled: false, interval_hours: 6, city: 'beijing', family: 'ipv4' }, history: [], tool_supported: false, tool_online: false })
      if (suffix === 'addresses') return send({ auto_ipv4: false, auto_ipv6: false, entry_ipv4: '192.0.2.10', entry_ipv6: '', observed: { received_at: 0, ipv4: {}, ipv6: {} } })
      if (suffix === 'dns-health') return send({ node_id: id, node: nodes[id - 1].name, checked_at: 0, stale: true, targets: [], failure_count: 0 })
      const now = Math.floor(timestamp / 1000)
      if (suffix === 'availability') {
        const summary = { online_seconds: 3600, offline_seconds: 0, unknown_seconds: 0, maintenance_seconds: 0, percent: 100, coverage_percent: 100 }
        return send({ ...summary, online_seconds: 86400, from: now - 86400, to: now, buckets: Array.from({ length: 24 }, (_, i) => ({ ...summary, from: now - (24 - i) * 3600, to: now - (23 - i) * 3600 })) })
      }
      if (suffix === 'network-quality') {
        const current = ['Telecom', 'Unicom', 'Mobile'].map((name, i) => ({ task_id: -i - 1, name, family: 'ipv4', type: 'icmp', series: `fixture-${i}`, latency_ms: 120 + i * 13, loss: 0, at: now }))
        return send({ current, from: now - 86400, to: now, step: 3600, points: current.flatMap(p => Array.from({ length: 24 }, (_, i) => ({ ...p, ts: now - (23 - i) * 3600, n: 60, lost: 0, avg_ms: p.latency_ms + i % 3 }))) })
      }
      if (suffix === 'resource-history') return send({ step: 3600, truncated: false, series: [{ key: 'host::cpu', kind: 'host', device: '', metric: 'cpu' }], points: Array.from({ length: 24 }, (_, i) => ({ ts: now - (23 - i) * 3600, average: 10 + i % 6, peak: 20 + i % 6, samples: 60 })) })
    }
    const relation = path.match(/^\/api\/admin\/infrastructure\/nodes\/([123])$/)
    if (relation) {
      assert.equal(role, 'admin', 'Operator must not fetch private infrastructure data')
      const value = probes[Number(relation[1])]
      return send({ revision: value.infrastructure_revision, supplier, asset: null, billing: value.probe.info.billing, price: value.probe.info.price, expires_at: value.probe.info.expires_at, auto_renewal: value.probe.info.auto_renewal })
    }
    if (/^\/api\/admin\/settings\/nodes\/[123]\/egress$/.test(path)) return send({ enabled: false, upstreams: [] })
    if (/^\/api\/admin\/settings\/nodes\/[123]\/wireguard-connections$/.test(path)) return send([])
    if (/^\/api\/admin\/settings\/nodes\/[123]\/wireguard-readiness$/.test(path)) return send({ error: 'No WireGuard capability in fixture' }, 503)
    switch (path) {
      case '/api/admin/me': return send({ id: 1, email: `${role}@example.com`, role })
      case '/api/site': return send({ name: 'Captain fixture', branding: {}, theme: {} })
      case '/api/admin/system/update': return send({ captain: { current: 'v1.46.0', latest: 'v1.46.0', has_update: false, release_build: true, has_backup: false, in_container: false } })
      case '/api/admin/groups': return send([])
      case '/api/admin/nodes': return send(nodes)
      case '/api/admin/nodes/domain-check': return send({ domain: '', valid: true, unchanged: true, conflicts: [] })
      case '/api/admin/domains': return send({ domains: [] })
      case '/api/admin/nodes/layout': return send({ revision: 'layout-1', groups: ['Example monitoring group'], nodes: [] })
      case '/api/admin/node-access': return send({ revision: 'access-1', groups: [], plans: [], inbounds: [] })
      case '/api/admin/entries': case '/api/admin/config-presets': return send([])
      case '/api/admin/infrastructure/suppliers': assert.equal(role, 'admin'); return send([supplier])
      case '/api/admin/settings/infrastructure': return send({ assets: [], suppliers: [supplier], paid_totals: [] })
      case '/api/admin/settings/infrastructure/payments': return send({ items: [], total: 0 })
      case '/api/admin/settings/notice': return send({ enabled: false, title: '', body: '' })
      default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected mocked read' }, 500)
    }
  })
  const page = await context.newPage()
  currentPage = page
  page.setDefaultTimeout(10_000)
  page.on('pageerror', error => state.errors.push(error.message))
  page.on('console', message => { if (message.text().includes('only supports one blocker')) state.blockerWarnings.push(message.text()) })
  const note = () => page.getByLabel(t('nodeProbe.note'), { exact: true })
  const amount = () => page.getByLabel(t('nodeCost.amount'), { exact: true })
  const visibility = () => page.getByRole('switch', { name: t('nodeInformation.showOnProbe') })
  const editor = () => page.getByTestId('node-information-editor')
  const probeSave = () => note().locator('xpath=ancestor::form').getByRole('button', { name: t('common.save'), exact: true })
  const dirty = () => page.getByRole('status').filter({ hasText: t('workspace.unsaved') })
  const dialog = () => page.getByRole('dialog', { name: t('workspace.unsaved'), exact: true })
  const destination = (id, suffix = '') => new URL(`nodes/${id}${suffix}`, base)
  const link = (id, suffix = '') => page.locator(`a[href="${destination(id, suffix).pathname}"]`).first()
  async function open(id, suffix = '') {
    const start = state.reads.length
    await page.goto(destination(id, suffix).href)
    await page.getByRole('heading', { name: nodes[id - 1].name, exact: true }).waitFor()
    await page.waitForLoadState('networkidle')
    return state.reads.slice(start)
  }
  async function information(id) {
    await open(id, '/information')
    await page.getByTestId('node-information').waitFor()
    assert.equal(await editor().count(), 0, 'Information initially shows a read-only summary')
    assert.ok(await visibility().isDisabled(), 'Visibility changes require explicit information editing')
    await page.getByRole('button', { name: t('nodeInformation.edit'), exact: true }).click()
    await note().waitFor(); await probeSave().waitFor(); assert.ok(await probeSave().isEnabled())
  }
  async function navigateAndCancel(id, suffix) {
    await link(id, suffix).click()
    await dialog().getByText(t('workspace.leaveHint'), { exact: true }).waitFor()
    await dialog().getByRole('button', { name: t('workspace.stay'), exact: true }).click()
    assert.equal(new URL(page.url()).pathname, destination(id, '/information').pathname)
  }
  async function discard(id, suffix) {
    await link(id, suffix).click()
    await dialog().getByRole('button', { name: t('workspace.discard'), exact: true }).click()
    await page.waitForURL(url => url.pathname === destination(id, suffix).pathname)
    await dialog().waitFor({ state: 'hidden' })
    await page.waitForLoadState('networkidle')
  }
  async function save(id) {
    const response = page.waitForResponse(r => new URL(r.url()).pathname === `/api/admin/nodes/${id}/probe` && r.request().method() === 'PUT')
    await probeSave().click(); assert.equal((await response).status(), 200)
    await dirty().waitFor({ state: 'hidden' })
    await editor().waitFor({ state: 'hidden' })
  }
  function clean() {
    assert.deepEqual(state.unexpected, [], 'All API requests need explicit fixtures')
    assert.deepEqual(state.external, [], 'No external network requests')
    assert.deepEqual(state.errors, [], 'No runtime JavaScript errors')
    assert.deepEqual(state.blockerWarnings, [], 'Exactly one navigation blocker per editing page')
  }
  return { context, page, state, probes, note, amount, visibility, editor, probeSave, dirty, dialog, link, destination, open, information, navigateAndCancel, discard, save, clean }
}

function overviewOnly(reads) {
  assert.ok(reads.some(path => path.endsWith('/probe')), 'Overview still loads live probe data')
  assert.ok(!reads.some(path => path.includes('/infrastructure')), 'Overview must not load private supplier/billing directories')
  assert.ok(!reads.some(path => /\/(network-diagnostics|return-routes|core-options|routing|overrides)$/.test(path)), 'Overview must not mount diagnostic or settings queries')
}

try {
  const admin = await setup()
  const { page, state } = admin
  overviewOnly(await admin.open(1))
  assert.equal(await admin.note().count(), 0)
  assert.equal(await admin.amount().count(), 0)
  assert.equal(await admin.visibility().count(), 0)
  assert.equal(await page.getByText(t('privateReady.title'), { exact: true }).count(), 0)
  assert.equal(state.writes.length, 0, 'Opening the overview must not issue jobs or saves')
  await page.screenshot({ path: resolve(artifacts, 'overview.png'), fullPage: true })

  await admin.information(1)
  assert.equal(await admin.amount().inputValue(), '12.5')
  assert.equal(await admin.visibility().isChecked(), true)
  assert.equal(await page.getByRole('heading', { name: t('resources.title'), exact: true }).count(), 0, 'Information page does not repeat resource monitoring')
  await page.screenshot({ path: resolve(artifacts, 'information-edit.png'), fullPage: true })
  await admin.note().fill('Draft information note'); await admin.amount().fill('19.5'); await admin.visibility().uncheck(); await admin.dirty().waitFor()
  for (const suffix of ['', '/diagnostics', '/settings/proxy']) {
    await admin.navigateAndCancel(1, suffix)
    assert.equal(await admin.note().inputValue(), 'Draft information note')
    assert.equal(await admin.amount().inputValue(), '19.5')
    assert.equal(await admin.visibility().isChecked(), false)
  }
  assert.equal(state.writes.length, 0, 'Canceling tab navigation does not save drafts')
  await admin.save(1)
  assert.equal(admin.probes[1].probe.info.note, 'Draft information note')
  assert.equal(admin.probes[1].probe.info.billing.amount, '19.5')
  assert.equal(admin.probes[1].probe.hidden, true)
  assert.equal(admin.probes[1].probe.mode, 'sum')
  assert.equal(admin.probes[1].probe.limit_bytes, 1073741824)
  assert.equal(admin.probes[1].probe.info.auto_renewal.enabled, true)
  await admin.link(1).click(); await page.waitForURL(url => url.pathname === admin.destination(1).pathname)
  assert.equal(await admin.dialog().count(), 0)
  await admin.information(1)
  assert.equal(await admin.note().inputValue(), 'Draft information note')
  await admin.note().fill('Discard this note'); await admin.amount().fill('77'); await admin.visibility().check()
  await admin.discard(1, '/diagnostics')
  assert.equal(await admin.note().count(), 0)
  assert.equal(await admin.visibility().count(), 0)
  const networkReads = await admin.open(1, '/diagnostics?view=network')
  await page.getByRole('heading', { name: t('diagnostics.title'), exact: true }).waitFor()
  assert.ok(networkReads.some(path => path.endsWith('/network-diagnostics')))
  assert.ok(!networkReads.some(path => path.includes('/infrastructure') || /\/(return-routes|core-options)$/.test(path)), 'Network diagnostics only mount the selected tool')
  const routeReads = await admin.open(1, '/diagnostics?view=routes')
  assert.ok(routeReads.some(path => path.endsWith('/return-routes')))
  assert.ok(!routeReads.some(path => path.includes('/infrastructure') || /\/(network-diagnostics|core-options)$/.test(path)), 'Route diagnostics do not mount the other tools')
  const healthReads = await admin.open(1, '/diagnostics')
  assert.ok(!healthReads.some(path => path.includes('/infrastructure') || /\/(network-diagnostics|return-routes)$/.test(path)), 'Health diagnostics do not start network or route tools')
  await admin.information(1)
  assert.equal(await admin.note().inputValue(), 'Draft information note')
  assert.equal(await admin.amount().inputValue(), '19.5')
  assert.equal(await admin.visibility().isChecked(), false)
  assert.equal(state.writes.length, 1)
  await admin.note().fill('Cancel information edits'); await admin.amount().fill('88')
  await page.getByRole('button', { name: t('nodeInformation.cancel'), exact: true }).click()
  await admin.editor().waitFor({ state: 'hidden' })
  await page.getByRole('button', { name: t('nodeInformation.edit'), exact: true }).click()
  assert.equal(await admin.note().inputValue(), 'Draft information note')
  assert.equal(await admin.amount().inputValue(), '19.5')

  // Old bookmarks must select the destination page instead of mounting all cards.
  for (const section of ['ingresses', 'routing', 'overrides']) {
    await admin.open(1, `?section=${section}`)
    assert.ok(new URL(page.url()).pathname.startsWith(admin.destination(1, '/settings/').pathname))
    await page.locator(`[data-section="${section}"]`).waitFor()
    assert.equal(await admin.note().count(), 0)
  }
  await admin.open(1, '#diagnostics')
  assert.equal(new URL(page.url()).pathname, admin.destination(1, '/diagnostics').pathname)
  await page.getByRole('heading', { name: t('diagnostics.title'), exact: true }).waitFor()
  await admin.open(1, '#billing-link')
  assert.equal(new URL(page.url()).pathname, admin.destination(1, '/information').pathname)
  await page.locator('#billing-link').waitFor()
  assert.equal(await admin.amount().inputValue(), '19.5')

  // Proxy settings own a blocker even when the information form is not mounted.
  await admin.open(1, '?section=overrides')
  const overrideSection = page.locator('[data-section="overrides"]')
  const xray = overrideSection.getByLabel('Xray', { exact: true })
  await xray.waitFor()
  await xray.fill('{"log":{"loglevel":"warning"}}')
  await admin.link(1).click(); await admin.dialog().waitFor()
  await admin.dialog().getByRole('button', { name: t('workspace.stay'), exact: true }).click()
  assert.deepEqual(JSON.parse(await xray.inputValue()), { log: { loglevel: 'warning' } })
  await admin.discard(1, '')
  await admin.open(1, '?section=overrides')
  assert.equal(await xray.inputValue(), '')
  assert.equal(state.writes.length, 1)

  overviewOnly(await admin.open(2))
  await admin.information(2)
  assert.equal(await page.locator('[data-section="overrides"]').count(), 0)
  await admin.note().fill('Unsaved monitor note'); await admin.amount().fill('21')
  await admin.navigateAndCancel(2, '')
  assert.equal(await admin.note().inputValue(), 'Unsaved monitor note')
  await admin.discard(2, '')
  await admin.information(2)
  assert.equal(await admin.note().inputValue(), 'Saved note')
  assert.equal(await admin.amount().inputValue(), '12.5')
  assert.equal(state.writes.length, 1)
  const monitorSettingsReads = await admin.open(2, '/settings/proxy')
  assert.equal(await page.locator('[data-section="overrides"], [data-section="routing"], [data-section="ingresses"]').count(), 0, 'Monitor-only settings cannot expose proxy editors')
  assert.ok(!monitorSettingsReads.some(path => /\/(routing|overrides|core-options)$/.test(path)), 'Monitor-only settings do not query proxy configuration')

  // Saving one information form must not clear its sibling's unsaved group.
  await admin.information(1)
  const monitoringGroup = page.getByRole('textbox', { name: t('monitoring.group'), exact: true })
  await monitoringGroup.click(); await page.getByRole('option', { name: 'Example monitoring group', exact: true }).click()
  await admin.note().fill('Saved while group stays dirty')
  const siblingSave = page.waitForResponse(r => new URL(r.url()).pathname === '/api/admin/nodes/1/probe' && r.request().method() === 'PUT')
  await admin.probeSave().click(); assert.equal((await siblingSave).status(), 200)
  await admin.editor().waitFor({ state: 'hidden' }); await admin.dirty().waitFor()
  assert.equal(await monitoringGroup.inputValue(), 'Example monitoring group')
  await admin.navigateAndCancel(1, '')
  assert.equal(await monitoringGroup.inputValue(), 'Example monitoring group')
  await admin.discard(1, '')
  await admin.open(1, '/information')
  assert.equal(await monitoringGroup.inputValue(), t('nodeLayout.ungrouped'))
  assert.equal(admin.probes[1].probe.info.note, 'Saved while group stays dirty')
  assert.equal(state.writes.length, 2, 'Group changes were never implicitly saved')

  // Browser Back is also guarded while a basic-information modal owns a draft.
  await admin.open(1)
  await admin.link(1, '/information').click(); await page.getByTestId('node-information').waitFor()
  const profileCard = page.locator('.mantine-Card-root').filter({ has: page.getByText(t('serverDetail.profile'), { exact: true }) })
  await profileCard.getByRole('button', { name: t('common.edit'), exact: true }).click()
  const profileModal = page.getByRole('dialog', { name: t('serverDetail.profile'), exact: true })
  const serverName = profileModal.getByRole('textbox', { name: t('nodes.name'), exact: true })
  await serverName.fill('Unsaved basic profile')
  await page.goBack(); await admin.dialog().waitFor()
  await admin.dialog().getByRole('button', { name: t('workspace.stay'), exact: true }).click()
  assert.equal(new URL(page.url()).pathname, admin.destination(1, '/information').pathname)
  assert.equal(await serverName.inputValue(), 'Unsaved basic profile')
  await page.goBack(); await admin.dialog().getByRole('button', { name: t('workspace.discard'), exact: true }).click()
  await page.waitForURL(url => url.pathname === admin.destination(1).pathname)
  await profileModal.waitFor({ state: 'hidden' })
  assert.equal(state.writes.length, 2, 'Discarding a basic profile does not save it')

  const unpairedReads = await admin.open(3)
  assert.ok(!unpairedReads.some(path => /\/(availability|network-quality|resource-history|network-diagnostics|return-routes)$/.test(path)), 'Unpaired overview does not mount monitoring or diagnostic tools')
  assert.equal(await admin.note().count(), 0)
  admin.clean(); await admin.context.close()

  const operator = await setup('operator')
  overviewOnly(await operator.open(1))
  await operator.information(1)
  assert.ok(await operator.amount().isDisabled(), 'Linked bill remains read-only for operators')
  assert.equal(await operator.page.getByRole('link', { name: t('infraLink.manageSuppliers'), exact: true }).count(), 0)
  await operator.note().fill('Operator information draft')
  await operator.navigateAndCancel(1, '')
  assert.equal(await operator.note().inputValue(), 'Operator information draft')
  await operator.save(1)
  assert.equal(operator.probes[1].probe.info.note, 'Operator information draft')
  assert.equal(operator.probes[1].probe.info.billing.amount, '12.5')
  await operator.open(1, '/settings/proxy')
  assert.ok(!operator.state.reads.some(path => path.includes('/infrastructure')), 'Operators never read infrastructure APIs')
  assert.ok(!operator.state.reads.some(path => /\/(network-diagnostics|return-routes)$/.test(path)), 'Operator navigation does not request admin diagnostic histories')
  operator.clean(); await operator.context.close()

  // Capture populated monitoring views for responsive, Chinese and dark review.
  for (const [name, options] of [
    ['overview-zh-dark', { locale: 'zh-CN', scheme: 'dark' }],
    ['overview-mobile-zh-light', { locale: 'zh-CN', viewport: { width: 390, height: 844 } }],
  ]) {
    const visual = await setup('admin', options)
    overviewOnly(await visual.open(1))
    await visual.page.locator('.mantine-Loader-root').first().waitFor({ state: 'hidden' })
    await visual.page.locator('.recharts-line-curve').first().waitFor()
    assert.equal(await visual.page.locator('html').getAttribute('data-mantine-color-scheme'), options.scheme || 'light')
    const overflow = await visual.page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    assert.ok(overflow <= 1, `Overview overflows the viewport by ${overflow}px`)
    await visual.page.screenshot({ path: resolve(artifacts, `${name}.png`), fullPage: true, animations: 'disabled' })
    if (options.viewport) {
      await visual.link(1, '/information').focus(); await visual.page.keyboard.press('Enter')
      await visual.page.getByTestId('node-information').waitFor()
      await visual.page.screenshot({ path: resolve(artifacts, 'information-mobile-zh-light.png'), fullPage: true, animations: 'disabled' })
    }
    visual.clean(); await visual.context.close()
  }
  console.log('PASS: overview query isolation, information save/discard, sibling and modal drafts, legacy links, monitor/operator/unpaired boundaries, Chinese dark/mobile screenshots and keyboard navigation')
} catch (error) {
  if (currentPage && !currentPage.isClosed()) {
    await currentPage.screenshot({ path: resolve(artifacts, 'failure.png'), fullPage: true })
    console.error(await currentPage.locator('[data-testid="node-information"]').ariaSnapshot().catch(() => 'Information card not mounted'))
    console.error(`Failure screenshot: ${resolve(artifacts, 'failure.png')}`)
  }
  throw error
} finally { await browser.close() }
