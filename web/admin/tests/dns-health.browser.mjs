// Local-only admin DNS regression. All API responses are fixtures; provider
// writes and external network access are blocked. Start admin Vite on 5675.
// DNS_UI_TEST_URL, PLAYWRIGHT_MODULE and DNS_UI_ARTIFACTS are optional.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.DNS_UI_TEST_URL || 'http://127.0.0.1:5675/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Only a local admin preview is allowed')
const artifacts = process.env.DNS_UI_ARTIFACTS || '/tmp/captain-dns-ui-browser'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true })
let currentPage
const timestamp = Date.now(), checked = Math.floor(timestamp / 1000) - 60
const seedNodes = [1, 2, 3, 4].map(id => ({
  id, name: `Example server ${id}`, hostname: `server-${id}.example.com`, public_addr: id === 4 ? '2001:db8::40' : '198.51.100.20', v6_addr: id === 2 ? '' : id === 4 ? '2001:db8::40' : '2001:db8::20',
  domain: `node-${id}.example.com`, internal_addr: '', monitor_url: '', paired: id !== 3, online: id !== 3, mode: 'monitor', proxy_allowed: false, monitor_group: '', version: 'v0.82.0', inbounds: 0, traffic_today_bytes: 0,
}))
const records = node => [
  { type: 'A', expected: node.public_addr.includes(':') ? [] : [node.public_addr], status: node.public_addr.includes(':') ? 'unconfigured' : 'ok' },
  { type: 'AAAA', expected: node.v6_addr ? [node.v6_addr] : [], status: node.v6_addr ? 'ok' : 'unconfigured' },
].map(record => ({ ...record, answers: ['panel', 'cloudflare'].map(source => ({ source, addresses: record.expected, ...(record.expected.length ? {} : { error: 'not_found' }) })) }))

async function setup({ role = 'admin', locale = 'en', scheme = 'light', mobile = false } = {}) {
  const dict = JSON.parse(await readFile(resolve(source, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => { const result = key.split('.').reduce((value, part) => value?.[part], dict); assert.equal(typeof result, 'string', key); return result }
  const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }, colorScheme: scheme, serviceWorkers: 'block' })
  await context.addInitScript(({ locale, scheme }) => { localStorage.setItem('i18nextLng', locale); localStorage.setItem('mantine-color-scheme-value', scheme) }, { locale, scheme })
  await context.routeWebSocket('**/*', socket => socket.close())
  const nodes = structuredClone(seedNodes)
  const health = Object.fromEntries(nodes.map(node => [node.id, { node_id: node.id, node: node.name, checked_at: checked, stale: false, failure_count: 0, targets: [{ key: 'node', host: node.domain, label: node.name, mode: 'direct', expected: records(node).flatMap(record => record.expected), status: 'ok', answers: [], families: records(node) }] }]))
  const state = { reads: [], writes: [], unexpected: [], external: [], errors: [], blockers: [], preview: 'write', apply: 'partial', revision: 'review-1' }
  const probe = id => ({ infrastructure_revision: 'fixture-1', infrastructure_linked: false, infrastructure_supplier_linked: false, region_countries: ['US'], storage_candidates: [], billed: 0, probe: { node_id: id, hidden: false, region_mode: 'off', region_detection: {}, info: { note: 'Saved note', price: '', expires_at: '', auto_renewal: { enabled: false }, traffic_reset: { mode: 'manual' }, location_settings: { mode: 'off' } }, limit_bytes: 0, reset_day: 1, mode: 'sum', used_up: 0, used_down: 0, prev_used: 0, period_start: '2026-10-01T00:00:00Z' } })
  const plan = node => ({ node_id: node.id, host: node.domain, revision: state.revision, enabled: state.preview !== 'shared', ...(state.preview === 'shared' ? { reason: 'shared_domain' } : {}), records: state.preview === 'shared' ? [] : records(node).filter(record => record.expected.length).map(record => ({ type: record.type, expected: record.expected[0], action: state.preview === 'same' ? 'unchanged' : record.type === 'A' ? 'updated' : 'created', ...(record.type === 'A' ? { before: { id: 'example-record', type: 'A', name: node.domain, content: state.preview === 'same' ? node.public_addr : '192.0.2.10', ttl: 300, proxied: false } } : {}) })) })
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const send = (value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
    const match = path.match(/^\/api\/admin\/nodes\/([1-4])(?:\/(.*))?$/)
    const node = match ? nodes[Number(match[1]) - 1] : undefined, suffix = match?.[2]
    if (method !== 'GET') {
      state.writes.push({ method, path, body: request.postDataJSON() })
      if (method === 'POST' && node && suffix === 'dns-sync') {
        assert.equal(role, 'admin'); assert.deepEqual(request.postDataJSON(), { revision: state.revision })
        if (state.apply === 'conflict') return send({ error: 'DNS configuration changed' }, 409)
        if (state.apply === 'disconnect') return route.abort('failed')
        health[node.id].stale = true
        return send({ results: records(node).filter(record => record.expected.length).map(record => ({ name: node.domain, ip: record.expected[0], action: record.type === 'A' ? 'updated' : 'created', outcome: record.type === 'AAAA' && state.apply === 'partial' ? 'unknown' : 'success', ...(record.type === 'AAAA' && state.apply === 'partial' ? { error: 'Outcome not confirmed' } : {}) })) })
      }
      if (method === 'POST' && node && suffix === 'dns-health/check') { health[node.id].checked_at = Math.floor(Date.now() / 1000); health[node.id].stale = false; return send(health[node.id]) }
      state.unexpected.push({ method, path }); return send({ error: 'Unexpected fixture write' }, 500)
    }
    state.reads.push(path)
    if (node) {
      if (!suffix) return send({ node, inbounds: [], ingresses: [], status: null })
      if (suffix === 'dns-health') return send(health[node.id])
      if (suffix === 'dns-sync') { assert.equal(role, 'admin'); return send(plan(node)) }
      if (suffix === 'probe') return send(probe(node.id))
      if (suffix === 'metadata') return send({})
      if (suffix === 'internal-address-usage') return send({ uses: [] })
      if (suffix === 'addresses') return send({ auto_ipv4: false, auto_ipv6: false, entry_ipv4: node.public_addr, entry_ipv6: node.v6_addr, observed: { received_at: 0, ipv4: {}, ipv6: {} } })
      if (suffix === 'service-control') return send({ node_id: node.id, revision: 'control-1', policy: { action: 'alert', threshold_percent: 100, limit_mbps: 0, auto_resume: true }, manual: { mode: 'normal', limit_mbps: 0 }, automatic: { active: false, mode: 'normal' }, effective: { mode: 'normal', limit_mbps: 0, revision: 'effective-1', reasons: [] }, traffic: { limit_bytes: 0, used_bytes: 0, fresh: false }, capability: { supported: false, reason: 'monitor_only' }, runtime: { status: 'unsupported' } })
    }
    if (/^\/api\/admin\/infrastructure\/nodes\/[1-4]$/.test(path)) return send({ revision: 'fixture-1', supplier: null, asset: null, price: '', expires_at: '', auto_renewal: { enabled: false } })
    switch (path) {
      case '/api/admin/me': return send({ id: 1, email: `${role}@example.com`, role })
      case '/api/site': return send({ name: 'Captain fixture', branding: {}, theme: {} })
      case '/api/admin/nodes': return send(nodes)
      case '/api/admin/nodes/layout': return send({ revision: 'layout-1', groups: [], nodes: [] })
      case '/api/admin/nodes/domain-check': return send({ domain: '', valid: true, unchanged: true, conflicts: [] })
      case '/api/admin/system/update': return send({ captain: { current: 'v1.47.0', latest: 'v1.47.0', has_update: false } })
      case '/api/admin/groups': case '/api/admin/entries': case '/api/admin/infrastructure/suppliers': return send([])
      case '/api/admin/settings/notice': return send({ enabled: false, title: '', body: '' })
      case '/api/admin/settings/dns/history': return send({ items: [], more: false })
      default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected fixture read' }, 500)
    }
  })
  const page = await context.newPage(); currentPage = page
  page.setDefaultTimeout(10_000)
  page.on('pageerror', error => state.errors.push(error.message))
  page.on('console', message => { if (message.text().includes('only supports one blocker')) state.blockers.push(message.text()) })
  const go = async suffix => { await page.goto(new URL(suffix, base).href); await page.waitForLoadState('networkidle') }
  const modal = () => page.getByRole('dialog', { name: t('dnsHealth.sync.title'), exact: true })
  const preview = async () => { await page.getByRole('button', { name: t('dnsHealth.sync.title'), exact: true }).click(); await modal().waitFor(); await page.waitForLoadState('networkidle') }
  const clean = () => { for (const key of ['unexpected', 'external', 'errors', 'blockers']) assert.deepEqual(state[key], [], key) }
  return { page, context, nodes, health, state, t, go, modal, preview, clean }
}

try {
  const admin = await setup(), { page, state, t } = admin
  await admin.go('nodes')
  const dual = page.getByRole('row').filter({ has: page.getByRole('link', { name: 'Example server 1', exact: true }) })
  await dual.getByText('IPv4', { exact: true }).waitFor(); await dual.getByText('IPv6', { exact: true }).waitFor()
  assert.ok((await dual.innerText()).includes('2001:db8::20'))
  const v6Only = page.getByRole('row').filter({ has: page.getByRole('link', { name: 'Example server 4', exact: true }) })
  assert.equal(await v6Only.getByText('2001:db8::40', { exact: true }).count(), 1, 'Legacy IPv6 in PublicAddr is not duplicated or called IPv4')
  await v6Only.getByText(t('nodeAddresses.unregistered'), { exact: true }).waitFor()
  for (const label of [t('nodes.online'), t('nodes.unpaired'), t('nodeMode.monitor')]) {
    const badges = page.getByRole('table').getByText(label, { exact: true })
    assert.ok(await badges.count(), `Expected visible ${label} badges`)
    for (const badge of await badges.all()) assert.ok(await badge.evaluate(element => element.scrollWidth <= element.clientWidth + 1), `${label} must be fully readable without ellipsis`)
  }
  assert.ok(await dual.getByRole('cell').first().evaluate(element => element.getBoundingClientRect().width >= 220), 'Name column keeps a readable minimum width')
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Wide server table must scroll inside its container, not the page')
  assert.ok(await page.getByRole('table').evaluate(element => {
    let parent = element.parentElement
    while (parent) {
      if (parent.scrollWidth > parent.clientWidth + 1 && ['auto', 'scroll'].includes(getComputedStyle(parent).overflowX)) return true
      parent = parent.parentElement
    }
    return false
  }), 'Additional column width is handled by horizontal table scrolling')
  await page.screenshot({ path: resolve(artifacts, 'server-addresses.png'), fullPage: true })

  await admin.go('nodes/1/information')
  const ipv4 = () => page.getByRole('link', { name: `IPv4 · A · ${t('dnsHealth.statuses.ok')}`, exact: true })
  const ipv6 = () => page.getByRole('link', { name: `IPv6 · AAAA · ${t('dnsHealth.statuses.ok')}`, exact: true })
  await ipv4().waitFor(); await ipv6().waitFor()
  assert.equal(state.writes.length, 0, 'Reading information never synchronizes DNS')
  await page.screenshot({ path: resolve(artifacts, 'information-badges.png'), fullPage: true })
  await page.getByRole('button', { name: t('nodeInformation.edit'), exact: true }).click()
  const note = page.getByLabel(t('nodeProbe.note'), { exact: true })
  await note.fill('Unsaved DNS navigation test')
  await ipv6().click()
  const leave = page.getByRole('dialog', { name: t('workspace.unsaved'), exact: true })
  await leave.getByRole('button', { name: t('workspace.stay'), exact: true }).click()
  assert.equal(await note.inputValue(), 'Unsaved DNS navigation test')
  await ipv6().click(); await leave.getByRole('button', { name: t('workspace.discard'), exact: true }).click()
  await page.waitForURL(url => url.hash === '#dns-1-node-aaaa')
  await page.locator('#dns-1-node-aaaa').waitFor()
  assert.equal(state.writes.length, 0, 'Navigation discards only the confirmed draft and never writes DNS')
  await page.screenshot({ path: resolve(artifacts, 'dns-health.png'), fullPage: true })

  await admin.preview()
  assert.equal(state.writes.length, 0, 'Opening sync reads records but cannot apply')
  await admin.modal().getByText('192.0.2.10', { exact: true }).waitFor()
  await page.screenshot({ path: resolve(artifacts, 'dns-sync-preview.png'), fullPage: true })
  await admin.modal().getByRole('button', { name: t('dnsHealth.sync.close'), exact: true }).click()
  assert.equal(state.writes.length, 0, 'Closing preview cannot apply')
  await admin.preview()
  await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).click()
  await admin.modal().getByText(t('dnsHealth.sync.outcomes.success'), { exact: true }).waitFor()
  await admin.modal().getByText(t('dnsHealth.sync.outcomes.unknown'), { exact: true }).waitFor()
  assert.equal(state.writes.length, 1)
  assert.ok(await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).isDisabled(), 'Ambiguous writes cannot be replayed from the same plan')
  await page.screenshot({ path: resolve(artifacts, 'dns-sync-partial.png'), fullPage: true })
  state.preview = 'same'
  await admin.modal().getByRole('button', { name: t('dnsHealth.sync.reload'), exact: true }).click()
  await admin.modal().getByText(t('dnsHealth.sync.nothing'), { exact: true }).waitFor()
  assert.ok(await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).isDisabled())
  await admin.modal().getByRole('button', { name: t('dnsHealth.sync.close'), exact: true }).click()
  await page.locator('#dns-1-node-a').getByText(t('dnsHealth.statuses.stale'), { exact: true }).waitFor()

  state.preview = 'write'; state.apply = 'conflict'
  await admin.preview(); await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).click()
  await admin.modal().getByText(t('dnsHealth.sync.changed'), { exact: true }).waitFor()
  assert.ok(await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).isDisabled())
  assert.equal(state.writes.length, 2)
  state.preview = 'shared'
  await admin.modal().getByRole('button', { name: t('dnsHealth.sync.reload'), exact: true }).click()
  await admin.modal().getByText(t('dnsHealth.sync.reasons.shared_domain'), { exact: true }).waitFor()
  assert.ok(await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).isDisabled())
  await admin.modal().getByRole('button', { name: t('dnsHealth.sync.close'), exact: true }).click()

  state.preview = 'write'; state.apply = 'disconnect'
  await admin.preview(); await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).click()
  await admin.modal().getByText(t('dnsHealth.sync.requestUnknown'), { exact: true }).waitFor()
  assert.ok(await admin.modal().getByRole('button', { name: t('dnsHealth.sync.apply'), exact: true }).isDisabled(), 'A lost response cannot trigger an automatic or stale-plan retry')
  assert.equal(state.writes.length, 3)
  await admin.modal().getByRole('button', { name: t('dnsHealth.sync.close'), exact: true }).click()

  await admin.go('nodes/2/information')
  await page.getByRole('link', { name: `IPv6 · AAAA · ${t('dnsHealth.statuses.unconfigured')}`, exact: true }).waitFor()
  await ipv4().waitFor()
  admin.health[1].stale = false
  admin.health[1].targets[0].families[1] = { type: 'AAAA', expected: ['2001:db8::20'], status: 'unknown', answers: [{ source: 'panel', addresses: ['2001:db8::20'] }, { source: 'cloudflare', addresses: [], error: 'timeout' }] }
  await admin.go('nodes/1/information')
  await ipv4().waitFor()
  await page.getByRole('link', { name: `IPv6 · AAAA · ${t('dnsHealth.statuses.query_failed')}`, exact: true }).waitFor()
  admin.health[1].checked_at = Math.floor(Date.now() / 1000) - 901
  await admin.go('nodes/1/information')
  assert.equal(await page.getByRole('link', { name: new RegExp(t('dnsHealth.statuses.stale')) }).count(), 2)
  await admin.go('nodes/3/diagnostics')
  await page.getByRole('heading', { name: t('dnsHealth.title'), exact: true }).waitFor()
  const checkResponse = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/dns-health/check'))
  await page.getByRole('button', { name: t('dnsHealth.recheck'), exact: true }).click()
  assert.equal((await checkResponse).status(), 200)
  await page.getByRole('button', { name: new RegExp(`${t('dnsHealth.recheck')} \\(`) }).waitFor()
  assert.ok(await page.getByRole('button', { name: new RegExp(t('dnsHealth.recheck')) }).isDisabled(), 'A successful check exposes the server cooldown')
  assert.equal(state.writes.length, 4)
  admin.clean(); await admin.context.close()

  const operator = await setup({ role: 'operator' })
  await operator.go('nodes/1/information')
  await operator.page.getByRole('link', { name: `IPv6 · AAAA · ${operator.t('dnsHealth.statuses.ok')}`, exact: true }).click()
  await operator.page.locator('#dns-1-node-aaaa').waitFor()
  assert.equal(await operator.page.getByRole('button', { name: operator.t('dnsHealth.sync.title'), exact: true }).count(), 0)
  assert.ok(!operator.state.reads.some(path => path.endsWith('/dns-sync')))
  operator.clean(); await operator.context.close()

  const mobile = await setup({ locale: 'zh-CN', scheme: 'dark', mobile: true })
  await mobile.go('nodes/1/information')
  await mobile.page.getByRole('link', { name: 'IPv6 · AAAA · 正常', exact: true }).waitFor()
  assert.equal(await mobile.page.evaluate(() => document.documentElement.dataset.mantineColorScheme), 'dark')
  await mobile.page.getByRole('link', { name: 'IPv6 · AAAA · 正常', exact: true }).click()
  await mobile.page.locator('#dns-1-node-aaaa').waitFor()
  assert.ok(await mobile.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'DNS table scrolls within its card on mobile')
  await mobile.page.evaluate(() => window.scrollTo(0, 0))
  await mobile.page.screenshot({ path: resolve(artifacts, 'dns-health-mobile-dark.png'), fullPage: true })
  await mobile.preview()
  assert.ok(await mobile.modal().getByRole('button', { name: mobile.t('dnsHealth.sync.apply'), exact: true }).isEnabled())
  assert.ok(await mobile.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'Sync preview has no page-wide overflow')
  await mobile.page.screenshot({ path: resolve(artifacts, 'dns-sync-mobile-dark.png'), fullPage: true })
  mobile.clean(); await mobile.context.close()
  console.log(`DNS UI regression passed. Screenshots: ${artifacts}`)
} catch (error) {
  if (currentPage && !currentPage.isClosed()) await currentPage.screenshot({ path: resolve(artifacts, 'failure.png'), fullPage: true })
  throw error
} finally { await browser.close() }
