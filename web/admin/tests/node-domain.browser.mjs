// Server hostname picker integration regression. Run against a local admin Vite
// server; all APIs are mocked and non-local network traffic is rejected.
// NODE_DOMAIN_TEST_URL, SETTINGS_SOURCE_ROOT, PLAYWRIGHT_MODULE,
// CHROME_EXECUTABLE and NODE_DOMAIN_ARTIFACTS can override local defaults.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.NODE_DOMAIN_TEST_URL || 'http://127.0.0.1:5179/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Use a local admin server only')
const artifacts = process.env.NODE_DOMAIN_ARTIFACTS || '/tmp/captain-node-domain-browser'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const zones = [
  { name: 'example.com', provider: 'cloudflare', auto_dns: true, has_token: true },
  { name: 'edge.example.com', provider: 'cloudflare', auto_dns: false, has_token: true },
  { name: 'example.net', provider: 'manual', auto_dns: false, has_token: false },
]
let currentPage
async function setup({ domain = '', shared = false, catalog = zones, failCatalog = false, locale = 'en', scheme = 'light', mobile = false, role = 'admin' } = {}) {
  const dict = JSON.parse(await readFile(resolve(source, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => { const value = key.split('.').reduce((v, k) => v?.[k], dict); assert.equal(typeof value, 'string', key); return value }
  const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }, serviceWorkers: 'block', colorScheme: scheme })
  await context.addInitScript(({ locale, scheme }) => { localStorage.setItem('i18nextLng', locale); localStorage.setItem('mantine-color-scheme-value', scheme) }, { locale, scheme })
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { writes: [], unexpected: [], external: [], errors: [], suggestionRequests: [], release: null, hold: false, dnsCheck: 'checked', failSuggestion: false, catalogReads: 0 }
  const node = { id: 1, name: 'Example server', hostname: 'host.example.com', public_addr: '192.0.2.10', internal_addr: '', v6_addr: '', domain, domain_shared: shared, paired: true, online: true, mode: 'full', version: 'v0.91.4', platform: 'linux/amd64', monitor_url: '', inbounds: 0, traffic_today_bytes: 0, user_speed_limit_mbps: 42, decoy_enabled: true, mita_quotas: true }
  await context.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const send = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/admin/nodes/domain-suggestion' && method === 'POST') {
      assert.equal(role, 'admin', 'An operator must never request DNS suggestions')
      const body = req.postDataJSON(); state.suggestionRequests.push(body)
      if (state.hold) await new Promise(resolve => { state.release = resolve })
      try {
        if (state.failSuggestion) return await send({ error: 'Suggestion unavailable' }, 503)
        return await send({ prefix: 'cedar-k7m2', domain: `cedar-k7m2.${body.zone}`, dns_check: state.dnsCheck })
      } catch { return } // Cancellation/unmount may close the intercepted request.
    }
    if (path === '/api/admin/nodes' && method === 'POST') {
      const body = req.postDataJSON(); state.writes.push({ method, path, body })
      return send({ ...node, id: 2, name: body.Name, domain: body.Domain, domain_shared: body.DomainShared, paired: false, pair_code: 'EXAMPLE-PAIR-CODE' })
    }
    if (path === '/api/admin/nodes/1' && method === 'PATCH') {
      const body = req.postDataJSON(); state.writes.push({ method, path, body })
      if (body.Domain !== undefined) node.domain = body.Domain
      if (body.DomainShared !== undefined) node.domain_shared = body.DomainShared
      return send({ ok: true })
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
      case '/api/admin/domains': state.catalogReads++; if (role !== 'admin') return send({ error: 'Forbidden' }, 403); return failCatalog ? send({ error: 'Directory unavailable' }, 503) : send({ domains: catalog, global_token: false })
      case '/api/admin/groups': case '/api/admin/entries': case '/api/admin/infrastructure/suppliers': return send([])
      case '/api/admin/nodes/1': return send({ node, status: {}, inbounds: [], ingresses: [], traffic: {} })
      case '/api/admin/nodes/1/internal-address-usage': return send({ uses: [] })
      case '/api/admin/nodes/1/metadata': return send({})
      case '/api/admin/nodes/1/dns-health': return send({ node_id: 1, node: node.name, checked_at: 0, stale: true, targets: [], failure_count: 0 })
      case '/api/admin/nodes/1/addresses': return send({ auto_ipv4: false, auto_ipv6: false, entry_ipv4: node.public_addr, entry_ipv6: '', observed: { received_at: 0, ipv4: {}, ipv6: {} } })
      case '/api/admin/nodes/1/probe': return send({ infrastructure_revision: 'fixture-1', region_countries: [], storage_candidates: [], probe: { node_id: 1, hidden: false, region_mode: 'off', info: {}, limit_bytes: 0, reset_day: 1, mode: 'sum', used_up: 0, used_down: 0 } })
      case '/api/admin/infrastructure/nodes/1': return send({ revision: 'fixture-1', supplier: null, asset: null, billing: null, price: '', expires_at: '', auto_renewal: { enabled: false } })
      case '/api/admin/nodes/1/service-control': return send({ node_id: 1, revision: 'control-1', policy: { action: 'alert', threshold_percent: 100, limit_mbps: 0, auto_resume: true }, manual: { mode: 'normal', limit_mbps: 0 }, automatic: { active: false, exempt_current_period: false }, effective: { mode: 'normal', reasons: [] }, traffic: { monitoring_enabled: true, limit_bytes: 0, used_bytes: 0, fresh: false }, capability: { supported: true }, runtime: { status: 'pending' } })
      default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected read' }, 500)
    }
  })
  const page = await context.newPage(); currentPage = page
  page.setDefaultTimeout(10_000)
  page.on('pageerror', e => state.errors.push(e.message))
  const dialog = () => page.getByRole('dialog').filter({ has: page.getByRole('textbox', { name: t('nodes.name'), exact: true }) })
  const prefix = () => dialog().getByLabel(t('nodeDomain.prefix'), { exact: true })
  const suffix = () => dialog().getByLabel(t('nodeDomain.zone'), { exact: true })
  const full = () => dialog().getByLabel(t('nodeDomain.fullName'), { exact: true })
  const sharedToggle = () => dialog().getByRole('switch', { name: t('nodes.domainShared') })
  const random = () => dialog().getByRole('button', { name: t('nodeDomain.random'), exact: true })
  const save = () => dialog().getByRole('button', { name: t('common.save'), exact: true })
  const create = () => dialog().getByRole('button', { name: t('common.create'), exact: true })
  async function createForm() {
    await page.goto(new URL('nodes', base).href)
    await page.getByRole('button', { name: t('nodes.create'), exact: true }).click()
    await dialog().waitFor()
    await dialog().getByRole('textbox', { name: t('nodes.name'), exact: true }).fill('New example server')
    await page.waitForLoadState('networkidle')
  }
  async function editForm() {
    await page.goto(new URL('nodes/1/information', base).href)
    await page.getByRole('heading', { name: node.name, exact: true }).waitFor()
    await page.getByRole('button', { name: t('common.edit'), exact: true }).first().click()
    await dialog().waitFor(); await page.waitForLoadState('networkidle')
  }
  async function choose(name) {
    await suffix().click(); await suffix().fill(name)
    await page.getByRole('option').filter({ has: page.getByText(name, { exact: true }) }).click()
  }
  async function submit(button) {
    const response = page.waitForResponse(r => ['POST', 'PATCH'].includes(r.request().method()) && /\/api\/admin\/nodes(?:\/1)?$/.test(new URL(r.url()).pathname))
    await button().click(); assert.equal((await response).status(), 200)
    await dialog().waitFor({ state: 'hidden' })
  }
  async function settled() { await page.waitForTimeout(400) }
  function clean() {
    assert.deepEqual(state.unexpected, [], 'All API accesses must use explicit fixtures')
    assert.deepEqual(state.external, [], 'No external requests')
    assert.deepEqual(state.errors, [], 'No runtime JavaScript errors')
  }
  async function close() { clean(); await context.close() }
  return { page, context, state, t, dialog, prefix, suffix, full, sharedToggle, random, save, create, createForm, editForm, choose, submit, settled, clean, close }
}

try {
  // Optional means both empty, never a half-filled hostname or implicit apex.
  const c = await setup(); await c.createForm()
  assert.equal(await c.random().isDisabled(), true)
  await c.prefix().fill('manual'); await c.settled()
  assert.equal(await c.create().isDisabled(), true)
  await c.prefix().fill(''); await c.choose('example.com'); await c.settled()
  assert.equal(await c.create().isDisabled(), true)
  await c.random().click(); await c.prefix().waitFor()
  await c.page.waitForFunction(() => Array.from(document.querySelectorAll('input')).some(i => i.value === 'cedar-k7m2'))
  assert.equal(await c.prefix().inputValue(), 'cedar-k7m2')
  assert.deepEqual(c.state.suggestionRequests, [{ zone: 'example.com' }])
  assert.deepEqual(c.state.writes, [], 'Generating a hostname must not create a server or write DNS')
  await c.dialog().getByText(c.t('nodeDomain.checked'), { exact: true }).waitFor()
  await c.choose('example.net'); assert.equal(await c.prefix().inputValue(), 'cedar-k7m2', 'Changing suffix retains prefix')
  await c.settled(); await c.submit(c.create)
  assert.equal(c.state.writes[0].body.Domain, 'cedar-k7m2.example.net')
  assert.equal(c.state.writes[0].body.DomainShared, false)
  await c.close()

  // Panel collision/invalid-input validation still controls both click and Enter submissions.
  const invalid = await setup(); await invalid.createForm(); await invalid.choose('example.com')
  await invalid.prefix().fill('taken'); await invalid.settled(); assert.equal(await invalid.create().isDisabled(), true)
  await invalid.prefix().press('Enter'); await invalid.settled(); assert.deepEqual(invalid.state.writes, [])
  await invalid.prefix().fill('-bad'); await invalid.settled(); assert.equal(await invalid.create().isDisabled(), true)
  await invalid.close()

  // Provider failures are not claimed to be successful availability checks.
  const unverified = await setup(); await unverified.createForm(); await unverified.choose('example.com')
  unverified.state.dnsCheck = 'unverified'; await unverified.random().click()
  await unverified.dialog().getByText(unverified.t('nodeDomain.unverified'), { exact: true }).waitFor()
  assert.deepEqual(unverified.state.writes, [])
  unverified.state.failSuggestion = true; await unverified.random().click()
  await unverified.dialog().getByRole('alert').filter({ hasText: unverified.t('nodeDomain.randomFailed') }).waitFor()
  assert.equal(await unverified.prefix().inputValue(), 'cedar-k7m2', 'A failed retry retains the current draft')
  await unverified.close()

  // A late suggestion cannot overwrite newer edits or a shared/external hostname.
  for (const action of ['type', 'shared', 'close']) {
    const late = await setup(); await late.createForm(); await late.choose('example.com')
    late.state.hold = true; await late.random().click()
    await late.page.waitForTimeout(50); assert.equal(typeof late.state.release, 'function')
    if (action === 'type') await late.prefix().fill('keep-me')
    if (action === 'shared') { await late.sharedToggle().check(); await late.full().fill('external.example.net') }
    if (action === 'close') await late.dialog().getByRole('button', { name: late.t('common.cancel'), exact: true }).click()
    late.state.release(); await late.settled()
    if (action === 'type') assert.equal(await late.prefix().inputValue(), 'keep-me')
    if (action === 'shared') assert.equal(await late.full().inputValue(), 'external.example.net')
    if (action === 'close') {
      await late.page.getByRole('button', { name: late.t('nodes.create'), exact: true }).click()
      await late.suffix().waitFor(); assert.equal(await late.prefix().inputValue(), '', 'Closed modal ignores late results, including its exit animation')
    }
    assert.deepEqual(late.state.writes, []); await late.close()
  }

  const edit = await setup({ domain: 'region.jp.edge.example.com' }); await edit.editForm()
  assert.equal(await edit.prefix().inputValue(), 'region.jp')
  assert.equal(await edit.suffix().inputValue(), 'edge.example.com')
  await edit.prefix().fill('new.jp'); await edit.settled(); await edit.submit(edit.save)
  assert.equal(edit.state.writes[0].body.Domain, 'new.jp.edge.example.com')
  assert.ok(!edit.state.writes[0].body.DomainShared, 'Editing a managed hostname never enables shared DNS')
  assert.equal(edit.state.writes[0].body.DecoyEnabled, true)
  assert.equal(edit.state.writes[0].body.UserSpeedLimitMbps, 42)
  assert.equal(edit.state.writes[0].body.MitaQuotas, true, 'Domain edits preserve unrelated proxy settings')
  await edit.close()

  for (const domain of ['legacy.external.example.org', 'example.com']) {
    const legacy = await setup({ domain }); await legacy.editForm()
    assert.equal(await legacy.full().inputValue(), domain)
    assert.equal(await legacy.sharedToggle().isChecked(), false)
    await legacy.dialog().getByRole('textbox', { name: legacy.t('nodes.name'), exact: true }).fill('Renamed server')
    await legacy.submit(legacy.save)
    assert.equal(legacy.state.writes[0].body.Domain, domain)
    assert.ok(!legacy.state.writes[0].body.DomainShared)
    await legacy.close()
  }

  const external = await setup({ domain: 'jp.example.com' }); await external.editForm()
  await external.sharedToggle().check(); assert.equal(await external.full().inputValue(), 'jp.example.com')
  await external.full().fill('other.external.example.org'); await external.sharedToggle().uncheck()
  assert.equal(await external.full().inputValue(), 'other.external.example.org', 'Unmatched external names remain complete after leaving shared mode')
  assert.equal(await external.sharedToggle().isChecked(), false)
  await external.settled(); await external.submit(external.save)
  assert.equal(external.state.writes[0].body.Domain, 'other.external.example.org')
  assert.ok(!external.state.writes[0].body.DomainShared)
  await external.close()

  for (const failure of [false, true]) {
    const fallback = await setup({ catalog: [], failCatalog: failure }); await fallback.createForm()
    await fallback.full().fill('manual.example.org'); await fallback.settled()
    assert.equal(await fallback.sharedToggle().isChecked(), false)
    await fallback.submit(fallback.create); assert.equal(fallback.state.writes[0].body.Domain, 'manual.example.org')
    await fallback.close()
  }

  // A partial draft is retained when switching input modes, without enabling DNS sharing.
  const partial = await setup(); await partial.createForm(); await partial.prefix().fill('keep-prefix')
  await partial.dialog().getByRole('button', { name: partial.t('nodeDomain.enterFull'), exact: true }).click()
  assert.equal(await partial.full().inputValue(), 'keep-prefix')
  assert.equal(await partial.sharedToggle().isChecked(), false)
  await partial.full().fill('keep-prefix.example.com')
  await partial.dialog().getByRole('button', { name: partial.t('nodeDomain.chooseZone'), exact: true }).click()
  assert.equal(await partial.prefix().inputValue(), 'keep-prefix')
  await partial.sharedToggle().check(); await partial.sharedToggle().uncheck()
  assert.equal(await partial.prefix().inputValue(), 'keep-prefix')
  assert.equal(await partial.suffix().inputValue(), 'example.com')
  assert.deepEqual(partial.state.writes, []); await partial.close()

  const operator = await setup({ role: 'operator', domain: 'jp.example.com' }); await operator.editForm()
  assert.equal(await operator.full().inputValue(), 'jp.example.com')
  assert.equal(await operator.random().count(), 0)
  assert.deepEqual(operator.state.suggestionRequests, [], 'Unavailable administrator catalog does not trigger privileged DNS requests')
  assert.deepEqual(operator.state.writes, []); await operator.close()

  // Real field geometry and screenshots at desktop/mobile, in both color modes.
  for (const scheme of ['light', 'dark']) for (const mobile of [false, true]) {
    const visual = await setup({ locale: 'zh-CN', scheme, mobile }); await visual.createForm(); await visual.choose('example.com'); await visual.random().click(); await visual.dialog().getByText(visual.t('nodeDomain.checked'), { exact: true }).waitFor(); await visual.settled()
    await visual.prefix().scrollIntoViewIfNeeded()
    const left = await visual.prefix().boundingBox(), right = await visual.suffix().boundingBox()
    assert.ok(left.width >= 160 && right.width >= 160, 'Inputs retain usable widths')
    if (mobile) assert.ok(right.y > left.y, 'Mobile stacks the hostname controls')
    else assert.ok(Math.abs(left.y - right.y) <= 1, 'Desktop inputs align')
    assert.ok(await visual.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'No page-level horizontal overflow')
    await visual.page.screenshot({ path: resolve(artifacts, `${mobile ? 'mobile' : 'desktop'}-${scheme}.png`), fullPage: true })
    await visual.close()
  }
  console.log('PASS: hostname create/edit, validation, random suggestion, stale response, legacy DNS, fallback and responsive layouts')
} catch (error) {
  if (currentPage && !currentPage.isClosed()) await currentPage.screenshot({ path: resolve(artifacts, 'failure.png'), fullPage: true }).catch(() => {})
  throw error
} finally { await browser.close() }
