// Local browser regression for location IP selection and stale lookup responses. APIs are
// explicit fixtures; external requests and unexpected writes are blocked.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.LOCATION_IP_TEST_URL || 'http://127.0.0.1:5196/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname))
const artifacts = process.env.LOCATION_IP_ARTIFACTS || '/tmp/captain-geo-validation'
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
let currentPage
async function setup({ locale = 'en', scheme = 'light', mobile = false, role = 'admin', initial = addresses() } = {}) {
  const dict = JSON.parse(await readFile(resolve(source, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => { const value = key.split('.').reduce((v, k) => v?.[k], dict); assert.equal(typeof value, 'string', key); return value }
  const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1100 }, serviceWorkers: 'block', colorScheme: scheme })
  await context.addInitScript(({ locale, scheme }) => { localStorage.setItem('i18nextLng', locale); localStorage.setItem('mantine-color-scheme-value', scheme) }, { locale, scheme })
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { writes: [], unexpected: [], external: [], errors: [], catalogReads: 0, catalog: [], addresses: initial, failRead: false, holdWrite: false, releaseWrite: null }
  const domain = '', shared = false, failCatalog = false
  state.detects = 0; state.failSave = false; state.failDetect = false; state.holdDetect = false; state.releaseDetect = null; state.probeReads = 0
  const now = Math.floor(Date.now() / 1000)
  const probe = { infrastructure_revision: 'fixture-1', region_countries: ['JP','US'], billed: 0, storage_candidates: [], probe: { node_id: 1, hidden: false, region_mode: 'manual', location_ip_settings: undefined, region_detection: { fresh: true, ip: '2001:db8::20', ip_source: 'primary', selection_reason: 'selected', source: 'country.is', checked_at: now, observed_at: now, country: 'US', ipv4: {}, ipv6: { ip: '2001:db8::20', country: 'US', status: 'ok', checked_at: now } }, info: { region: 'JP', location_settings: { mode: 'off' }, note: 'Saved note' }, limit_bytes: 0, reset_day: 1, mode: 'sum', used_up: 0, used_down: 0, prev_used: 0 } }

  const node = { id: 1, name: 'Example server', hostname: 'host.example.com', public_addr: '192.0.2.10', internal_addr: '', v6_addr: '', domain, domain_shared: shared, paired: true, online: true, mode: 'full', version: 'v0.91.4', platform: 'linux/amd64', monitor_url: '', inbounds: 0, traffic_today_bytes: 0, user_speed_limit_mbps: 42, decoy_enabled: true, mita_quotas: true }
  await context.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const send = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/admin/nodes/1/probe' && method === 'PUT') {
      const body = req.postDataJSON(); state.writes.push(body)
      if (state.failSave) return send({ error: 'Location save rejected' }, 422)
      Object.assign(probe.probe, { location_ip_settings: body.LocationIP, hidden: body.Hidden, region_mode: body.RegionMode, info: body.Info, limit_bytes: body.LimitBytes, reset_day: body.ResetDay, mode: body.Mode, resources: body.Resources, storage: body.Storage })
      const mode = body.LocationIP.mode
      probe.probe.region_detection = { ...probe.probe.region_detection, fresh: true, checked_at: 0, country: '', ip: mode === 'manual' ? body.LocationIP.manual_ip : mode === 'entry' ? node.public_addr : '2001:db8::20', ip_source: mode === 'manual' ? 'manual' : mode === 'entry' ? 'entry_override' : 'primary' }
      return send(probe)
    }
    if (path === '/api/admin/nodes/1/region-detect' && method === 'POST') {
      state.detects++
      if (state.holdDetect) { const snapshot = structuredClone(probe); await new Promise(resolve => { state.releaseDetect = resolve }); return send(snapshot) }
      if (state.failDetect) return send({ error: 'Location lookup unavailable' }, 503)
      probe.probe.region_detection = { ...probe.probe.region_detection, checked_at: now, country: 'US' }
      return send(probe)
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
      case '/api/admin/nodes/1/probe': state.probeReads++; return state.failRead ? send({ error: 'Probe read failed' }, 503) : send(probe)
      case '/api/admin/infrastructure/nodes/1': return send({ revision: 'fixture-1', supplier: null, asset: null, billing: null, price: '', expires_at: '', auto_renewal: { enabled: false } })
      case '/api/admin/nodes/1/service-control': return send({ node_id: 1, revision: 'control-1', policy: { action: 'alert', threshold_percent: 100, limit_mbps: 0, auto_resume: true }, manual: { mode: 'normal', limit_mbps: 0 }, automatic: { active: false, exempt_current_period: false }, effective: { mode: 'normal', reasons: [] }, traffic: { monitoring_enabled: true, limit_bytes: 0, used_bytes: 0, fresh: false }, capability: { supported: true }, runtime: { status: 'pending' } })
      default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected read' }, 500)
    }
  })
  const page = await context.newPage(); currentPage = page
  page.setDefaultTimeout(10_000); page.on('pageerror', error => state.errors.push(error.message))
  const card = page.getByTestId('node-information'), fields = page.getByTestId('location-ip-fields')
  const tx = (key, values = {}) => { let text = t(key); for (const [k, v] of Object.entries(values)) text = text.replaceAll(`{{${k}}}`, String(v)); return text }
  async function open() { await page.goto(new URL('nodes/1/information', base).href); await card.waitFor(); await page.waitForLoadState('networkidle') }
  async function edit() { await card.getByRole('button', { name: t('nodeInformation.edit'), exact: true }).click(); await fields.waitFor() }
  async function select(key, option) { await card.getByRole('textbox', { name: t(key), exact: true }).click(); await page.getByRole('option', { name: t(option), exact: true }).click() }
  async function save(status = 200) { const response = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname.endsWith('/probe')); await card.getByRole('button', { name: t('common.save'), exact: true }).click(); assert.equal((await response).status(), status); await page.waitForLoadState('networkidle') }
  async function close() { assert.deepEqual(state.unexpected, []); assert.deepEqual(state.external, []); assert.deepEqual(state.errors, []); await context.close() }
  return { page, context, state, probe, card, fields, t: tx, open, edit, select, save, close }
}
try {
  if (process.env.LOCATION_IP_RACE_ONLY) {
    for (const aba of [false, true]) {
      const r = await setup(); await r.open(); await r.edit()
      r.state.holdDetect = true
      await r.card.getByRole('button', { name: r.t('nodeRegion.detect'), exact: true }).click()
      for (let i = 0; !r.state.releaseDetect && i < 100; i++) await new Promise(resolve => setTimeout(resolve, 10))
      assert.ok(r.state.releaseDetect)
      // Another browser saves B; the optional second save returns to policy A
      // with new source evidence. The current tab receives this via polling.
      r.probe.probe.location_ip_settings = { mode: 'manual', manual_ip: '203.0.113.30' }
      r.probe.probe.region_detection = { ...r.probe.probe.region_detection, ip: '203.0.113.30', ip_source: 'manual' }
      if (aba) {
        r.probe.probe.location_ip_settings = { mode: 'auto' }
        r.probe.probe.region_detection = { ...r.probe.probe.region_detection, ip_source: 'primary' }
      }
      await r.fields.getByText(r.t('locationIP.current', { ip: '203.0.113.30' }), { exact: true }).waitFor({ timeout: 16000 })
      const finished = r.page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/region-detect'))
      r.state.releaseDetect(); await finished; await r.page.waitForLoadState('networkidle')
      assert.equal(await r.fields.getByText(r.t('locationIP.current', { ip: '203.0.113.30' }), { exact: true }).count(), 1, aba ? 'ABA lookup must not revive old evidence' : 'Late lookup must not overwrite newly saved location policy')
      await r.close()
    }
    console.log('Location IP delayed-response race regression passed, including policy ABA.')
  } else {
  const c = await setup(); await c.open()
  await c.card.getByText(c.t('locationIP.source', { source: c.t('locationIP.sources.primary') }), { exact: true }).waitFor()
  await c.edit()
  assert.equal(await c.fields.getByRole('switch').isChecked(), false)
  const detect = c.card.getByRole('button', { name: c.t('nodeRegion.detect'), exact: true })
  assert.equal(await detect.isEnabled(), true)
  await c.select('locationIP.mode', 'locationIP.modes.manual')
  const input = c.fields.getByRole('textbox', { name: c.t('locationIP.manual'), exact: true })
  for (const ip of ['10.10.0.2', 'example.com', '[2001:db8::20]:443']) { await input.fill(ip); assert.equal(await c.card.getByRole('button', { name: c.t('common.save'), exact: true }).isDisabled(), true) }
  await input.fill('198.51.100.20')
  assert.equal(await detect.isDisabled(), true); assert.equal(c.state.detects, 0)
  await c.fields.getByText(c.t('locationIP.saveFirst')).waitFor()
  await c.card.getByRole('textbox', { name: c.t('nodeProbe.note'), exact: true }).fill('Keep this unrelated draft')
  c.state.failSave = true; await c.save(422)
  assert.equal(await input.inputValue(), '198.51.100.20')
  assert.equal(await c.card.getByRole('textbox', { name: c.t('nodeProbe.note'), exact: true }).inputValue(), 'Keep this unrelated draft')
  c.state.failSave = false; await c.save()
  assert.deepEqual(c.state.writes.at(-1).LocationIP, { mode: 'manual', manual_ip: '198.51.100.20', allow_entry_fallback: false })
  assert.equal(c.state.writes.at(-1).RegionMode, 'manual'); assert.equal(c.state.writes.at(-1).Info.region, 'JP'); assert.equal(c.state.writes.at(-1).Info.location_settings.mode, 'off')
  assert.equal(c.state.writes.at(-1).Info.note, 'Keep this unrelated draft')
  await c.card.getByText(c.t('locationIP.source', { source: c.t('locationIP.sources.manual') }), { exact: true }).waitFor()
  await c.edit(); assert.equal(await detect.isEnabled(), true)
  c.state.failDetect = true; await detect.click(); await c.page.getByText('Location lookup unavailable', { exact: true }).waitFor()
  assert.equal(await c.fields.getByText(c.t('locationIP.current', { ip: '198.51.100.20' }), { exact: true }).count(), 1)
  c.state.failDetect = false; await detect.click(); await c.page.waitForLoadState('networkidle')
  await c.select('locationIP.mode', 'locationIP.modes.entry'); assert.equal(await detect.isDisabled(), true)
  await c.save(); assert.equal(c.state.writes.at(-1).LocationIP.mode, 'entry')
  await c.card.getByText(c.t('locationIP.source', { source: c.t('locationIP.sources.entry_override') }), { exact: true }).waitFor()
  await c.edit(); await c.select('locationIP.mode', 'locationIP.modes.manual'); await input.fill('unfinished')
  await c.select('locationIP.mode', 'locationIP.modes.auto'); await c.fields.getByRole('switch').check(); await c.save()
  assert.equal(c.state.writes.at(-1).LocationIP.allow_entry_fallback, true)
  assert.equal(c.state.writes.at(-1).LocationIP.manual_ip, '')
  c.probe.probe.region_detection = { ...c.probe.probe.region_detection, fresh: false, stale: true, ip_source: 'fallback' }
  await c.open(); await c.card.getByText(c.t('locationIP.previous', { ip: '2001:db8::20' }), { exact: true }).waitFor()
  await c.edit(); assert.equal(await detect.isDisabled(), true)
  await c.close()
  for (const locale of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const x = await setup({ locale, scheme: locale === 'ru' ? 'dark' : 'light' }); await x.open(); await x.edit()
    await x.fields.getByText(x.t('locationIP.priority'), { exact: true }).waitFor()
    await x.select('locationIP.mode', 'locationIP.modes.manual')
    await x.fields.getByRole('textbox', { name: x.t('locationIP.manual'), exact: true }).fill('2001:db8::30')
    assert.equal(await x.card.getByRole('button', { name: x.t('nodeRegion.detect'), exact: true }).isDisabled(), true)
    if (locale === 'zh-CN') { await x.fields.scrollIntoViewIfNeeded(); await x.page.screenshot({ path: resolve(artifacts, 'location-ip-desktop-zh-CN.png') }) }
    assert.equal(await x.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, locale)
    await x.close()
  }
  const m = await setup({ locale: 'zh-CN', mobile: true }); await m.open()
  await m.card.getByTestId('location-ip-evidence').scrollIntoViewIfNeeded(); await m.page.screenshot({ path: resolve(artifacts, 'location-ip-summary-mobile-zh-CN.png') })
  await m.edit(); await m.fields.scrollIntoViewIfNeeded(); await m.page.screenshot({ path: resolve(artifacts, 'location-ip-mobile-zh-CN.png') })
  assert.equal(await m.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true)
  await m.close()
  const o = await setup({ role: 'operator' }); await o.open(); await o.edit()
  assert.equal(await o.fields.getByRole('textbox', { name: o.t('locationIP.mode'), exact: true }).inputValue(), o.t('locationIP.modes.auto'))
  await o.select('locationIP.mode', 'locationIP.modes.manual'); await o.fields.getByRole('textbox', { name: o.t('locationIP.manual'), exact: true }).fill('203.0.113.30'); await o.save()
  assert.equal('InfrastructureRevision' in o.state.writes.at(-1), false)
  await o.close()
  console.log('Location IP browser regression passed: saved/draft isolation, validation, save/detect errors, independent country/map, source evidence, six locales, desktop/mobile, operator compatibility.')
  }
} catch (error) {
  if (currentPage && !currentPage.isClosed()) await currentPage.screenshot({ path: resolve(artifacts, 'failure.png'), fullPage: true })
  throw error
} finally { await browser.close() }
