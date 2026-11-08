// Ingress hostname and address-source integration regression. Run against a local admin Vite
// server; all APIs are mocked and non-local network traffic is rejected.
// INGRESS_DOMAIN_TEST_URL, SETTINGS_SOURCE_ROOT, PLAYWRIGHT_MODULE,
// CHROME_EXECUTABLE and INGRESS_DOMAIN_ARTIFACTS can override local defaults.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.INGRESS_DOMAIN_TEST_URL || 'http://127.0.0.1:5179/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Use a local admin server only')
const artifacts = process.env.INGRESS_DOMAIN_ARTIFACTS || '/tmp/captain-ingress-domain-browser'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const zones = [
  { name: 'example.com', provider: 'cloudflare', auto_dns: true, has_token: true },
  { name: 'edge.example.com', provider: 'cloudflare', auto_dns: false, has_token: true },
  { name: 'example.net', provider: 'manual', auto_dns: false, has_token: false },
]
let currentPage
async function setup({ domain = '', shared = false, catalog = zones, ipv6 = '', failCatalog = false, locale = 'en', scheme = 'light', mobile = false, role = 'admin' } = {}) {
  const dict = JSON.parse(await readFile(resolve(source, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => { const value = key.split('.').reduce((v, k) => v?.[k], dict); assert.equal(typeof value, 'string', key); return value }
  const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }, serviceWorkers: 'block', colorScheme: scheme })
  await context.addInitScript(({ locale, scheme }) => { localStorage.setItem('i18nextLng', locale); localStorage.setItem('mantine-color-scheme-value', scheme) }, { locale, scheme })
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { writes: [], unexpected: [], external: [], errors: [], suggestionRequests: [], release: null, hold: false, dnsCheck: 'checked', failSuggestion: false, catalogReads: 0, catalog: structuredClone(catalog) }
  const node = { id: 1, name: 'Example server', hostname: 'host.example.com', public_addr: '192.0.2.10', internal_addr: '', v6_addr: ipv6, domain, domain_shared: shared, paired: true, online: true, mode: 'full', version: 'v0.91.4', platform: 'linux/amd64', monitor_url: '', inbounds: 0, traffic_today_bytes: 0, user_speed_limit_mbps: 42, decoy_enabled: true, mita_quotas: true }
  await context.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const send = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/admin/ingresses/domain-suggestion' && method === 'POST') {
      assert.equal(role, 'admin', 'An operator must never request DNS suggestions')
      const body = req.postDataJSON(); state.suggestionRequests.push(body)
      if (state.hold) await new Promise(resolve => { state.release = resolve })
      try {
        if (state.failSuggestion) return await send({ error: 'Suggestion unavailable' }, 503)
        return await send({ prefix: 'cedar-k7m2', domain: `cedar-k7m2.${body.zone}`, dns_check: state.dnsCheck })
      } catch { return } // Cancellation/unmount may close the intercepted request.
    }
    if (path === '/api/admin/nodes/1/ingresses' && method === 'POST') {
      const body = req.postDataJSON(); state.writes.push({ method, path, body }); return send({ ingress: { id: 5, ...body } })
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
  const dialog = () => page.getByRole('dialog').first()
  const prefix = () => dialog().getByLabel(t('nodeDomain.prefix'), { exact: true })
  const suffix = () => dialog().getByLabel(t('nodeDomain.zone'), { exact: true })
  const save = () => dialog().getByRole('button', { name: t('common.save'), exact: true })
  async function choose(key, option) {
    await dialog().getByLabel(t(key), {exact:true}).click()
    await page.getByRole('option').filter({has:page.getByText(option,{exact:true})}).click()
  }
  async function open() {
    await page.goto(new URL('nodes/1/settings/ingresses',base).href)
    await page.getByRole('button',{name:t('ingress.add'),exact:true}).click()
    await dialog().getByLabel(t('ingress.name')).fill('Example NAT')
    await page.waitForLoadState('networkidle')
  }
  async function close() {
    assert.deepEqual(state.unexpected,[],'explicit API mocks'); assert.deepEqual(state.external,[]); assert.deepEqual(state.errors,[])
    await context.close()
  }
  return {page,context,state,t,dialog,prefix,suffix,save,choose,open,close}
}
try {
  const s=await setup(),{page,state,t,dialog,choose,prefix,suffix,save}=s; await s.open()
  await choose('ingress.addressSource',t('ingress.addressIPv6'))
  await dialog().getByText(t('ingress.addressMissing'),{exact:true}).waitFor(); assert.equal(await save().isDisabled(),true)
  await choose('ingress.addressSource',t('ingress.addressIPv4'))
  assert.equal(await dialog().getByLabel(t('ingress.currentAddress'),{exact:true}).inputValue(),'192.0.2.10')
  await prefix().fill('nat'); assert.equal(await save().isDisabled(),true)
  await dialog().getByRole('button',{name:t('common.cancel'),exact:true}).click()
  await page.getByRole('button',{name:t('workspace.stay'),exact:true}).click(); assert.equal(await prefix().inputValue(),'nat')
  await choose('nodeDomain.zone','example.com'); await dialog().getByText('nat.example.com',{exact:true}).waitFor()
  assert.equal(await save().isDisabled(),false)
  state.hold=true; await dialog().getByRole('button',{name:t('nodeDomain.random'),exact:true}).click()
  for(let i=0;i<100&&!state.release;i++) await page.waitForTimeout(20)
  assert.ok(state.release); await prefix().fill('edited'); state.release(); await page.waitForTimeout(100)
  assert.equal(await prefix().inputValue(),'edited')
  await save().click(); await dialog().waitFor({state:'hidden'})
  assert.equal(state.writes.length,1); assert.equal(state.writes[0].body.EntryAddressSource,'server_ipv4'); assert.equal(state.writes[0].body.EntryDomain,'edited.example.com'); assert.equal('DomainDraft' in state.writes[0].body,false)
  await page.getByRole('button',{name:t('ingress.add'),exact:true}).click()
  assert.equal(await prefix().inputValue(),'','A new ingress must not retain a prior domain draft')
  await prefix().fill('discard-prefix')
  await dialog().getByRole('button',{name:t('common.cancel'),exact:true}).click()
  await page.getByRole('button',{name:t('workspace.discard'),exact:true}).click();await dialog().waitFor({state:'hidden'})
  await page.getByRole('button',{name:t('ingress.add'),exact:true}).click()
  assert.equal(await prefix().inputValue(),'','An incomplete discarded draft cannot survive a new editor')
  await s.close()
  for(const locale of ['en','zh-CN','zh-TW','ja','ko','ru']) {
    const s=await setup({locale,ipv6:'2001:db8::10',mobile:locale==='zh-CN'}); await s.open()
    await s.choose('ingress.addressSource',s.t('ingress.addressIPv6'))
    assert.equal(await s.dialog().getByLabel(s.t('ingress.currentAddress'),{exact:true}).inputValue(),'2001:db8::10')
    await s.choose('nodeDomain.zone','example.com'); await s.dialog().getByRole('button',{name:s.t('nodeDomain.random'),exact:true}).click()
    await s.dialog().getByText('cedar-k7m2.example.com',{exact:true}).waitFor()
    assert.equal(await s.dialog().getByRole('switch',{name:s.t('nodes.domainShared')}).count(),0)
    await s.page.screenshot({path:resolve(artifacts,`ingress-${locale}.png`),fullPage:true}); await s.save().click(); await s.dialog().waitFor({state:'hidden'})
    assert.equal(s.state.writes[0].body.EntryAddressSource,'server_ipv6'); await s.close()
  }
  const f=await setup({failCatalog:true});await f.open()
  await f.dialog().getByRole('textbox',{name:f.t('ingress.entryHost'),exact:true}).fill('203.0.113.30')
  await f.dialog().getByLabel(f.t('nodeDomain.fullName'),{exact:true}).fill('external.example.net')
  await f.save().click();await f.dialog().waitFor({state:'hidden'});assert.equal(f.state.writes[0].body.EntryDomain,'external.example.net');await f.close()
  console.log('PASS: ingress domain/source, missing IPv6, incomplete draft, canceled suggestion, six locales and manual fallback')
} catch(error) { if(currentPage) await currentPage.screenshot({path:resolve(artifacts,'failure.png'),fullPage:true}); throw error }
finally {await browser.close()}
