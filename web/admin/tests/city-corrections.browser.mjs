// Local browser regression for location IP selection and stale lookup responses. APIs are
// explicit fixtures; external requests and unexpected writes are blocked.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.LOCATION_IP_TEST_URL || 'http://127.0.0.1:5680/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname))
const artifacts = process.env.LOCATION_IP_ARTIFACTS || '/tmp/captain-city-corrections'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || '../../probe/node_modules/playwright/index.mjs')
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
  const probe = { infrastructure_revision: 'fixture-1', region_countries: ['GB','JP','US'], billed: 0, storage_candidates: [], probe: { node_id: 1, hidden: false, region_mode: 'auto', location_ip_settings: undefined, region_detection: { fresh: true, ip: '2001:db8::20', ip_source: 'primary', selection_reason: 'selected', source: 'country.is', checked_at: now, observed_at: now, country: 'US', ipv4: {}, ipv6: { ip: '2001:db8::20', country: 'US', status: 'ok', checked_at: now } }, info: { region: 'US', location: { country: 'US', precision: 'country' }, location_settings: { mode: 'auto' }, note: 'Saved note' }, limit_bytes: 0, reset_day: 1, mode: 'sum', used_up: 0, used_down: 0, prev_used: 0 } }

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
      probe.probe.info.location = body.Info.location_settings.mode === 'manual' ? body.Info.location_settings.manual : body.Info.location_settings.mode === 'off' ? null : { country: 'US', precision: 'country' }
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
 const r = await setup({ locale: 'zh-CN' })
 await r.open()
 const evidence = r.card.getByTestId('location-comparison')
 assert.ok((await evidence.innerText()).includes('US'))
 await r.edit()
 const selectCity = async (run, text) => {
  await run.card.getByRole('textbox', { name: run.t('locationCorrection.city'), exact: true }).fill(text)
  await run.page.getByRole('option', { name: /London/ }).click()
 }
 await selectCity(r, '伦敦')
 assert.equal(r.state.writes.length, 0)
 assert.equal(r.state.detects, 0)
 assert.ok((await evidence.innerText()).includes('US'))
 assert.ok(!(await evidence.innerText()).includes('London'))
 await r.card.getByRole('button', {name:r.t('nodeInformation.cancel'),exact:true}).click()
 assert.equal(r.state.writes.length,0)
 assert.ok(!(await evidence.innerText()).includes('London'))
 await r.edit();await selectCity(r,'London')
 assert.equal(await r.card.getByRole('textbox', {name:r.t('mapLocation.city'),exact:true}).inputValue(),'London')
 r.state.failSave=true
 await r.save(422)
 assert.equal(await r.card.getByRole('textbox', {name:r.t('mapLocation.city'),exact:true}).inputValue(),'London')
 r.state.failSave=false
 await r.save()
 const body=r.state.writes.at(-1)
 assert.equal(body.RegionMode,'manual');assert.equal(body.Info.region,'GB')
 assert.equal(body.Info.location_settings.mode,'manual')
 assert.equal(body.Info.location_settings.manual.city,'London')
 assert.equal(body.Info.location_settings.manual.country,'GB')
 assert.equal(body.Info.location_settings.manual.subdivision,undefined)
 assert.equal(typeof body.Info.location_settings.manual.lat,'number')
 assert.equal(typeof body.Info.location_settings.manual.lng,'number')
 assert.equal(body.LocationIP.mode,'auto')
 assert.ok((await evidence.innerText()).includes('US'))
 assert.ok((await evidence.innerText()).includes('London'))
 await evidence.getByText(r.t('locationCorrection.difference'),{exact:true}).waitFor()
 await r.page.reload();await r.card.waitFor()
 await evidence.getByText(r.t('locationCorrection.difference'),{exact:true}).waitFor()
 await evidence.scrollIntoViewIfNeeded();await r.page.screenshot({path:resolve(artifacts,'correction-summary.png')})
 await r.edit()
 assert.equal(await r.card.getByRole('textbox', {name:r.t('mapLocation.city'),exact:true}).inputValue(),'London')
 await r.select('mapLocation.mode','mapLocation.auto')
 await r.save()
 assert.equal(r.state.writes.at(-1).Info.region,'GB')
 assert.equal(r.state.writes.at(-1).Info.location_settings.mode,'auto')
 assert.equal(r.state.writes.at(-1).Info.location_settings.manual.city,'London')
 await r.close()
 const cached=await setup({locale:'en'})
 cached.probe.probe.region_detection.fresh=false
 await cached.open()
 assert.ok((await cached.card.getByTestId('location-comparison').innerText()).includes(cached.t('locationCorrection.cached')))
 cached.probe.probe.region_detection.country=''
 await cached.page.reload();await cached.card.waitFor()
 const comparison=cached.card.getByTestId('location-comparison')
 await comparison.getByText(cached.t('locationCorrection.automatic')+': '+cached.t('mapLocation.unknown'),{exact:true}).waitFor()
 assert.equal(await comparison.getByText(cached.t('locationCorrection.difference'),{exact:true}).count(),0)
 await cached.close()
 for(const locale of ['en','zh-CN','zh-TW','ja','ru','ko']) {
  const m=await setup({locale,mobile:true});await m.open();await m.edit()
  await selectCity(m,'London')
  assert.ok(await m.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
  await m.card.getByRole('textbox',{name:m.t('locationCorrection.city'),exact:true}).scrollIntoViewIfNeeded()
  await m.page.screenshot({path:resolve(artifacts,locale+'-mobile.png')})
  await m.save();assert.equal(m.state.writes.at(-1).Info.region,'GB')
  await m.close()
 }
 console.log('City correction browser checks passed: explicit paired draft, failed save, saved raw/effective comparison, reload, independent auto mode, six locales')
} finally { await browser.close() }
