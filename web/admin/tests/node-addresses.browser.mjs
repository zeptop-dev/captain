// Local browser regression for real server profile address controls. APIs are
// explicit fixtures; external requests and unexpected writes are blocked.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.ADDRESS_TEST_URL || 'http://127.0.0.1:5187/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname))
const artifacts = process.env.ADDRESS_ARTIFACTS || '/tmp/captain-fallback-address-browser'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const runID = 'a'.repeat(32)
function addresses() {
  const now = Math.floor(Date.now() / 1000)
  return { auto_ipv4: true, auto_ipv6: false, fallback_auto_ipv4: false, fallback_auto_ipv6: false, entry_ipv4: '192.0.2.10', entry_ipv6: '', dns_pending: false,
    observed: { received_at: now, ipv4: {}, ipv6: {}, fallback_follow_ipv4: {}, fallback_follow_ipv6: {},
      fallback_ipv4: { source: 'captain_cloudflare_trace', origin: 'https://example.com:2096', address: '198.51.100.20', received_at: now, sampled_at: now - 1, check_status: 'no_conflict', run_id: runID } } }
}
function confirm(data) {
  const f = data.observed.fallback_ipv4
  data.observed.fallback_follow_ipv4 = { ...f, candidate_since: f.received_at - 120, candidate_sampled_at: f.sampled_at - 120, last_sampled_at: f.sampled_at, last_success: f.received_at, confirmed_at: f.received_at }
}
let currentPage
async function setup({ locale = 'en', scheme = 'light', mobile = false, role = 'admin', initial = addresses() } = {}) {
  const dict = JSON.parse(await readFile(resolve(source, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => { const value = key.split('.').reduce((v, k) => v?.[k], dict); assert.equal(typeof value, 'string', key); return value }
  const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1100 }, serviceWorkers: 'block', colorScheme: scheme })
  await context.addInitScript(({ locale, scheme }) => { localStorage.setItem('i18nextLng', locale); localStorage.setItem('mantine-color-scheme-value', scheme) }, { locale, scheme })
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { writes: [], unexpected: [], external: [], errors: [], catalogReads: 0, catalog: [], addresses: initial, failRead: false, holdWrite: false, releaseWrite: null }
  const domain = '', shared = false, failCatalog = false
  const node = { id: 1, name: 'Example server', hostname: 'host.example.com', public_addr: '192.0.2.10', internal_addr: '', v6_addr: '', domain, domain_shared: shared, paired: true, online: true, mode: 'full', version: 'v0.91.4', platform: 'linux/amd64', monitor_url: '', inbounds: 0, traffic_today_bytes: 0, user_speed_limit_mbps: 42, decoy_enabled: true, mita_quotas: true }
  await context.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const send = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/admin/settings/nodes/1/addresses' && method === 'PUT') {
      const body = req.postDataJSON(); state.writes.push(body)
      if (state.holdWrite) await new Promise(resolve => { state.releaseWrite = resolve })
      Object.assign(state.addresses, body)
      state.addresses.observed.fallback_follow_ipv4 = {}
      state.addresses.observed.fallback_follow_ipv6 = {}
      return send(state.addresses)
    }
    if (method !== 'GET') { state.unexpected.push({ method, path }); return send({ error: 'Unexpected write' }, 500) }
    switch (path) {
      case '/api/admin/me': return send({ id: 1, role, email: `${role}@example.com` })
      case '/api/site': return send({ name: 'Captain fixture', branding: {}, theme: {} })
      case '/api/admin/system/update': return send({ captain: { current: 'v1.57.1', latest: 'v1.57.1', has_update: false }, bosun_latest: 'v0.91.4' })
      case '/api/admin/nodes': return send([node])
      case '/api/admin/nodes/layout': return send({ revision: 'layout-1', groups: [], nodes: [] })
      case '/api/admin/nodes/agent-https': return send({ enabled: false })
      case '/api/admin/nodes/domain-check': {
        const name = url.searchParams.get('domain') || ''
        const valid = !name || /^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$/.test(name)
        return send({ domain: name, valid, unchanged: name === node.domain, conflicts: name === 'taken.example.com' ? [{ id: 9, name: 'Other server', shared: false }] : [] })
      }
      case '/api/admin/domains': state.catalogReads++; if (role !== 'admin') return send({ error: 'Forbidden' }, 403); return failCatalog ? send({ error: 'Directory unavailable' }, 503) : send({ domains: state.catalog, global_token: false })
      case '/api/admin/groups': case '/api/admin/entries': case '/api/admin/infrastructure/suppliers': return send([])
      case '/api/admin/nodes/1': return send({ node, status: {}, inbounds: [], ingresses: [], traffic: {} })
      case '/api/admin/nodes/1/internal-address-usage': return send({ uses: [] })
      case '/api/admin/nodes/1/metadata': return send({})
      case '/api/admin/nodes/1/dns-health': return send({ node_id: 1, node: node.name, checked_at: 0, stale: true, targets: [], failure_count: 0 })
      case '/api/admin/nodes/1/addresses': return state.failRead ? send({ error: 'Address read failed' }, 503) : send(state.addresses)
      case '/api/admin/nodes/1/probe': return send({ infrastructure_revision: 'fixture-1', region_countries: [], storage_candidates: [], probe: { node_id: 1, hidden: false, region_mode: 'off', info: {}, limit_bytes: 0, reset_day: 1, mode: 'sum', used_up: 0, used_down: 0 } })
      case '/api/admin/infrastructure/nodes/1': return send({ revision: 'fixture-1', supplier: null, asset: null, billing: null, price: '', expires_at: '', auto_renewal: { enabled: false } })
      case '/api/admin/nodes/1/service-control': return send({ node_id: 1, revision: 'control-1', policy: { action: 'alert', threshold_percent: 100, limit_mbps: 0, auto_resume: true }, manual: { mode: 'normal', limit_mbps: 0 }, automatic: { active: false, exempt_current_period: false }, effective: { mode: 'normal', reasons: [] }, traffic: { monitoring_enabled: true, limit_bytes: 0, used_bytes: 0, fresh: false }, capability: { supported: true }, runtime: { status: 'pending' } })
      default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected read' }, 500)
    }
  })
  const page = await context.newPage(); currentPage = page
  page.setDefaultTimeout(10_000); page.on('pageerror', error => state.errors.push(error.message))
  const v4 = page.getByRole('region', { name: 'IPv4', exact: true })
  const v6 = page.getByRole('region', { name: 'IPv6', exact: true })
  const following = () => v4.getByRole('switch', { name: t('nodeAddresses.follow'), exact: true })
  const fallback = () => v4.getByRole('switch', { name: t('nodeAddresses.fallbackFollow') })
  async function open() { await page.goto(new URL('nodes/1/information', base).href); await v4.waitFor(); await page.waitForLoadState('networkidle') }
  async function check(key) { await v4.getByText(t('nodeAddresses.' + key), { exact: true }).waitFor() }
  async function write(control) {
    const result = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname.endsWith('/addresses'))
    await control().click(); assert.equal((await result).status(), 200); await page.waitForLoadState('networkidle')
  }
  async function close() { assert.deepEqual(state.unexpected, []); assert.deepEqual(state.external, []); assert.deepEqual(state.errors, []); await context.close() }
  return { page, context, state, t, v4, v6, following, fallback, open, check, write, close }
}
try {
  const c = await setup(); await c.open()
  assert.equal(await c.fallback().isChecked(), false)
  assert.equal(await c.v6.getByRole('switch', { name: c.t('nodeAddresses.fallbackFollow') }).isDisabled(), true)
  await c.write(c.fallback); assert.deepEqual(c.state.writes, [{ fallback_auto_ipv4: true }])
  await c.check('fallbackStatus.waiting'); assert.equal(await c.fallback().isChecked(), true)
  confirm(c.state.addresses); c.state.addresses.observed.fallback_follow_ipv4.confirmed_at = 0
  await c.open(); await c.check('fallbackStatus.confirming')
  confirm(c.state.addresses); await c.open(); await c.check('fallbackStatus.applying')
  c.state.addresses.entry_ipv4 = c.state.addresses.observed.fallback_ipv4.address
  await c.open(); await c.check('fallbackStatus.following')
  await c.v4.getByText(c.t('nodeAddresses.fallbackSingle'), { exact: true }).waitFor()
  // A saved confirmation must disappear during a pending policy change.
  c.state.holdWrite = true
  await c.fallback().click()
  await c.check('fallbackStatus.unavailable')
  assert.equal(await c.fallback().isDisabled(), true)
  const saved = c.page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname.endsWith('/addresses'))
  for (let i = 0; !c.state.releaseWrite && i < 100; i++) await new Promise(resolve => setTimeout(resolve, 10))
  assert.ok(c.state.releaseWrite); c.state.releaseWrite(); await saved
  c.state.holdWrite = false; await c.page.waitForLoadState('networkidle')
  assert.equal(await c.fallback().isChecked(), false)
  await c.write(c.fallback); await c.check('fallbackStatus.waiting')
  await c.write(c.following)
  assert.deepEqual(c.state.writes.at(-1), { auto_ipv4: false })
  assert.equal(await c.fallback().isChecked(), true); assert.equal(await c.fallback().isDisabled(), true)
  await c.write(c.following); await c.check('fallbackStatus.waiting')
  // Old confirmed evidence cannot survive a changed source/process identity.
  confirm(c.state.addresses); c.state.addresses.observed.fallback_ipv4.run_id = 'b'.repeat(32)
  await c.open(); await c.check('fallbackStatus.waiting')
  c.state.addresses.observed.fallback_ipv4.check_status = 'conflict'
  delete c.state.addresses.observed.fallback_ipv4.run_id
  await c.open(); await c.check('fallbackStatus.conflict')
  assert.equal(await c.v4.getByText(c.t('nodeAddresses.fallbackLegacy'), { exact: true }).count(), 0)
  assert.equal(await c.v4.getByText(c.t('nodeAddresses.fallbackStatus.following'), { exact: true }).count(), 0)
  delete c.state.addresses.observed.fallback_ipv4.check_status
  delete c.state.addresses.observed.fallback_ipv4.run_id
  await c.open(); await c.check('fallbackLegacy'); await c.check('fallbackStatus.legacy')
  // Enabling policy does not claim that an old agent can supply usable evidence.
  await c.write(c.fallback); await c.write(c.fallback); await c.check('fallbackStatus.legacy')
  c.state.addresses = addresses(); c.state.addresses.fallback_auto_ipv4 = true; confirm(c.state.addresses)
  c.state.addresses.entry_ipv4 = c.state.addresses.observed.fallback_ipv4.address
  await c.open(); await c.check('fallbackStatus.following')
  // Browser offline immediately revokes the display without waiting for polling.
  await c.context.setOffline(true); await c.check('fallbackStatus.unavailable')
  await c.context.setOffline(false); await c.open(); await c.check('fallbackStatus.following')
  c.state.failRead = true
  await c.page.waitForTimeout(5100)
  await c.page.evaluate(() => { window.dispatchEvent(new Event('offline')); window.dispatchEvent(new Event('online')) })
  await c.page.getByText(c.t('dnsHealth.loadError'), { exact: true }).waitFor()
  await c.check('fallbackStatus.unavailable')
  c.state.failRead = false; await c.open()
  // A successful HTTP read with an expired observation still cannot show green.
  c.state.addresses.observed.fallback_ipv4.sampled_at -= 901
  await c.open(); await c.check('fallbackStatus.unavailable')
  await c.close()

  const primary = addresses(); delete primary.observed.fallback_ipv4
  primary.observed.ipv4 = { address: primary.entry_ipv4, last_success: primary.observed.received_at, confirmed_at: primary.observed.received_at }
  const p = await setup({ initial: primary }); await p.open(); assert.equal(await p.fallback().isEnabled(), true)
  await p.write(p.fallback); assert.deepEqual(p.state.writes, [{ fallback_auto_ipv4: true }]); await p.close()
  const op = await setup({ role: 'operator' }); await op.open(); assert.equal(await op.v4.getByRole('switch').count(), 0); await op.close()

  for (const locale of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const data = addresses(); data.fallback_auto_ipv4 = true; confirm(data); data.entry_ipv4 = data.observed.fallback_ipv4.address
    const v = await setup({ locale, initial: data, mobile: locale !== 'en', scheme: locale === 'en' ? 'dark' : 'light' })
    await v.open(); await v.check('fallbackStatus.following'); await v.v4.scrollIntoViewIfNeeded()
    assert.equal(await v.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true, locale + ' fits viewport')
    if (['en', 'zh-CN'].includes(locale)) {
      await v.page.screenshot({ path: resolve(artifacts, locale + '.png'), fullPage: true })
      await v.v4.screenshot({ path: resolve(artifacts, locale + '-card.png') })
    }
    await v.close()
  }
  console.log('PASS: fallback policy writes, reset/confirm/apply, identities, conflicts, old reports, offline/error/expiry, primary-only opt-in, operator and six-language responsive layouts')
} catch (error) {
  if (currentPage && !currentPage.isClosed()) await currentPage.screenshot({ path: resolve(artifacts, 'failure.png'), fullPage: true }).catch(() => {})
  throw error
} finally { await browser.close() }
