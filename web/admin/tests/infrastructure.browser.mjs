// Server information / primary billing regression with isolated API fixtures.
// Start the integrated admin app, then run this script with ADMIN_TEST_URL.
// Optional: PLAYWRIGHT_MODULE, CHROME_EXECUTABLE, SETTINGS_SOURCE_ROOT,
// INFRA_TEST_EVIDENCE. No real credentials, backend writes or external requests.
import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5662/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Use a local admin server only')
const evidence = process.env.INFRA_TEST_EVIDENCE
if (evidence) await mkdir(evidence, { recursive: true })
const dict = JSON.parse(await readFile(resolve(source, 'src/i18n/en.json'), 'utf8'))
const t = key => { const value = key.split('.').reduce((value, key) => value?.[key], dict); assert.equal(typeof value, 'string', key); return value }
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' })
await context.addInitScript(() => { if (!localStorage.getItem('i18nextLng')) localStorage.setItem('i18nextLng', 'en') })
await context.routeWebSocket('**/*', socket => socket.close())
const suppliers = [{ id: 1, name: 'Example Hosting', url: 'https://example.com' }, { id: 2, name: 'Other Supplier', url: 'https://example.com/other' }]
let role = 'admin', revision = 'r1', supplier = null, linked = null, forceConflict = false, failProbe = false, mismatch = false, hidden = false
let info = { provider: 'Legacy Provider', provider_url: 'https://example.com/legacy', billing: { amount: '12.5', currency: 'USD', cycle: 'annual' }, expires_at: '2027-01-31', auto_renewal: { enabled: true, anchor_day: 31 }, note: 'Saved note', traffic_reset: { mode: 'manual' }, location_settings: { mode: 'off' }, region: '' }
const asset = { id: 5, supplier_id: 2, supplier_name: 'Other Supplier', node_id: null, primary_node_id: null, name: 'Existing annual bill', currency: 'USD', amount_minor: 2000, period_months: 12, anchor_day: 31, next_due: '2027-12-31', remind_days: 7, active: true, notes: '', external_ref: '', revision: 1, auto_renewal_enabled: false }
const assets = [asset, { ...asset, id: 6, name: 'Unsupported two-month bill', period_months: 2 }, { ...asset, id: 7, name: 'Another server bill', node_id: 2 }, { ...asset, id: 9, name: 'Archived bill', active: false }]
const probe = () => ({ infrastructure_revision: revision, infrastructure_linked: !!linked, infrastructure_supplier_linked: !!supplier, region_countries: ['US', 'JP'], storage_candidates: [], probe: { node_id: 1, hidden, info, region_mode: 'off', region_detection: {}, resources: {}, limit_bytes: 0, reset_day: 1, mode: 'sum', used_up: 0, used_down: 0, prev_used: 0, period_start: '' }, billed: 0 })
const relation = () => ({ revision: mismatch ? 'other' : revision, supplier, asset: linked, billing: info.billing, price: info.price ?? '', expires_at: info.expires_at, auto_renewal: info.auto_renewal })
const node = { id: 1, name: 'Example Server', public_addr: '192.0.2.10', internal_addr: '', v6_addr: '', domain: '', monitor_url: '', monitor_group: '', version: 'v0.82.0', platform: 'linux/amd64', hostname: 'server.example.com', paired: true, online: true, proxy_allowed: false, mode: 'monitor', traffic_today_bytes: 0, inbounds: 0, outdated: false, cert_problem: false }
const requests = [], errors = [], external = [], unexpected = [], checks = []
await context.route('**/*', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
  if (url.origin !== base.origin) { external.push(url.origin); return route.abort() }
  if (!path.startsWith('/api/')) return method === 'GET' || method === 'HEAD' ? route.continue() : route.abort()
  const record = { path, method }; requests.push(record)
  const body = req.postDataJSON()
  const send = (value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
  if (path === '/api/admin/nodes/1/probe') {
    if (method === 'PUT') {
      record.body = body
      if (forceConflict) { forceConflict = false; revision += 'conflict'; info.billing.amount = '45'; return send({ error: 'Changed fixture revision' }, 409) }
      if (role === 'admin') assert.equal(body.InfrastructureRevision, revision)
      const renewal = info.auto_renewal
      info = { ...body.Info, auto_renewal: { ...renewal, ...body.Info.auto_renewal } }; hidden = body.Hidden
      if (body.SupplierID !== undefined) supplier = suppliers.find(s => s.id === body.SupplierID) ?? null
      if (supplier) { info.provider = supplier.name; info.provider_url = supplier.url }
      revision += 's'; return send(probe())
    }
    if (failProbe) return send({ error: 'Fixture read unavailable' }, 503)
    return send(probe())
  }
  if (path === '/api/admin/infrastructure/suppliers') {
    if (method === 'POST') { const result = { id: 3, ...body }; suppliers.push(result); record.body = body; return send(result) }
    return send(suppliers)
  }
  if (path === '/api/admin/infrastructure/nodes/1') {
    if (method === 'PUT') {
      record.body = body; assert.equal(body.revision, revision)
      assert.equal(Object.hasOwn(body, 'confirm_disable_auto_renewal'), false, 'Linking must preserve online date extension')
      if (body.action === 'unlink_asset') linked = null
      else {
        linked = body.action === 'link_asset' ? { ...asset, node_id: 1, primary_node_id: 1 } : { ...asset, id: 8, name: body.name, supplier_id: supplier.id, supplier_name: supplier.name, amount_minor: Number(info.billing.amount) * 100, next_due: info.expires_at, node_id: 1, primary_node_id: 1 }
        supplier = suppliers.find(s => s.id === linked.supplier_id)
        info = { ...info, provider: supplier.name, provider_url: supplier.url, billing: { amount: String(linked.amount_minor / 100), currency: linked.currency, cycle: 'annual' }, expires_at: linked.next_due }
        linked.auto_renewal_enabled = info.auto_renewal.enabled
      }
      revision += 'l'; return send(relation())
    }
    return send(relation())
  }
  if (method !== 'GET') { unexpected.push(record); return send({ error: 'Unexpected fixture write' }, 500) }
  switch (path) {
    case '/api/admin/me': return send({ id: 1, email: 'admin@example.com', role })
    case '/api/site': return send({ name: 'Captain fixture', branding: {}, theme: {} })
    case '/api/admin/system/update': return send({ captain: { current: 'v1.46.0', latest: 'v1.46.0', has_update: false, release_build: true } })
    case '/api/admin/nodes/1': return send({ node, inbounds: [], status: null })
    case '/api/admin/nodes/1/metadata': return send({})
    case '/api/admin/nodes/1/addresses': return send({ auto_ipv4: false, auto_ipv6: false, entry_ipv4: node.public_addr, entry_ipv6: '', observed: { received_at: 0, ipv4: {}, ipv6: {} } })
    case '/api/admin/nodes': return send([node])
    case '/api/admin/nodes/layout': return send({ revision: 'layout-1', groups: [], nodes: [] })
    case '/api/admin/settings/infrastructure': return send({ suppliers, assets, paid_totals: [] })
    default: unexpected.push(record); return send({ error: 'Unexpected fixture read' }, 500)
  }
})
const page = await context.newPage()
page.setDefaultTimeout(15_000)
page.on('pageerror', error => errors.push(error.message))
const information = () => page.getByTestId('node-information')
const editor = () => page.getByTestId('node-information-editor')
const note = () => editor().getByLabel(t('nodeProbe.note'), { exact: true })
const amount = () => editor().getByLabel(t('nodeCost.amount'), { exact: true })
const supplierSelect = () => editor().getByRole('textbox', { name: t('nodeProbe.provider'), exact: true })
const dialog = () => page.getByRole('dialog')
const probeWrites = () => requests.filter(r => r.method === 'PUT' && r.path.endsWith('/probe'))
async function edit() { await information().getByRole('button', { name: t('nodeInformation.edit'), exact: true }).click(); await note().waitFor() }
async function save() {
  const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/nodes/1/probe') && r.request().method() === 'PUT')
  await editor().getByRole('button', { name: t('common.save'), exact: true }).click(); return (await response).status()
}
async function linkedAction(label) {
  const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/infrastructure/nodes/1') && r.request().method() === 'PUT')
  await editor().getByRole('button', { name: label, exact: true }).click()
  return { response }
}
async function unlink() {
  const { response } = await linkedAction(t('infraLink.unlink'))
  await dialog().getByRole('checkbox').check(); await dialog().getByRole('button', { name: t('common.save'), exact: true }).click()
  assert.equal((await response).status(), 200); await editor().getByRole('button', { name: t('infraLink.linkExisting'), exact: true }).waitFor()
}
try {
  failProbe = true
  await page.goto(new URL('nodes/1/information', base).href)
  await information().getByText(t('workspace.loadError'), { exact: true }).waitFor()
  assert.equal(await information().getByRole('button', { name: t('nodeInformation.edit'), exact: true }).isDisabled(), true)
  failProbe = false; mismatch = true; await information().getByRole('button', { name: t('workspace.retry'), exact: true }).click()
  await information().getByText(t('infraLink.snapshotMismatch'), { exact: true }).waitFor()
  assert.equal(await information().getByRole('button', { name: t('nodeInformation.edit'), exact: true }).isDisabled(), true)
  mismatch = false; await information().getByRole('button', { name: t('workspace.retry'), exact: true }).click()
  await edit(); assert.equal(await note().inputValue(), 'Saved note')
  checks.push('failed initial read and mismatched finance snapshots block edits; retry hydrates coherent saved data')
  await supplierSelect().click(); await page.getByRole('option', { name: 'Example Hosting', exact: true }).click()
  assert.equal(await editor().getByLabel(t('nodeProbe.providerURL'), { exact: true }).inputValue(), 'https://example.com')
  assert.equal(await editor().getByLabel(t('nodeProbe.providerURL'), { exact: true }).getAttribute('readonly'), '')
  await note().fill('Draft supplier note'); assert.equal(await save(), 200); await editor().waitFor({ state: 'hidden' })
  assert.equal(supplier.id, 1); assert.equal(info.note, 'Draft supplier note')
  if (evidence) await information().screenshot({ path: resolve(evidence, 'information-readonly-desktop.png') })
  checks.push('supplier and note save atomically from information editor')
  await edit(); await supplierSelect().click(); await page.getByRole('option', { name: `＋ ${t('infra.addSupplier')}`, exact: true }).click()
  await dialog().getByLabel(t('infra.name'), { exact: true }).fill('New Supplier')
  await dialog().getByLabel(t('infra.url'), { exact: true }).fill('https://example.com/new')
  await dialog().getByRole('button', { name: t('common.save'), exact: true }).click(); await dialog().waitFor({ state: 'hidden' })
  assert.equal(supplier.id, 1); assert.equal(await supplierSelect().inputValue(), 'New Supplier')
  assert.equal(await save(), 200); await editor().waitFor({ state: 'hidden' }); assert.equal(supplier.id, 3)
  checks.push('dropdown creation selects new supplier without saving server until explicit save')
  await edit(); forceConflict = true; await note().fill('Keep unrelated note'); await amount().fill('30')
  assert.equal(await save(), 409)
  failProbe = true
  const failedReload = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/nodes/1/probe') && r.status() === 503)
  await editor().getByRole('button', { name: t('infraLink.reloadFinance'), exact: true }).click(); await failedReload
  assert.equal(await amount().inputValue(), '30'); assert.equal(await note().inputValue(), 'Keep unrelated note')
  failProbe = false
  await editor().getByRole('button', { name: t('infraLink.reloadFinance'), exact: true }).click()
  await page.waitForFunction(() => document.querySelector('[data-testid="node-information-editor"] input[value="45"]'))
  assert.equal(await amount().inputValue(), '45'); assert.equal(await note().inputValue(), 'Keep unrelated note')
  const beforeCancel = probeWrites().length
  await editor().getByRole('button', { name: t('nodeInformation.cancel'), exact: true }).click(); await editor().waitFor({ state: 'hidden' })
  assert.equal(probeWrites().length, beforeCancel); await edit(); assert.equal(await note().inputValue(), 'Draft supplier note'); assert.equal(await amount().inputValue(), '45')
  checks.push('CAS conflict and failed reload keep drafts; successful finance reload preserves note; cancel restores current saved snapshot without write')
  await information().getByRole('switch', { name: t('nodeInformation.showOnProbe') }).uncheck()
  assert.equal(await save(), 200); await editor().waitFor({ state: 'hidden' }); assert.equal(hidden, true)
  assert.equal(Object.hasOwn(probeWrites().at(-1).body, 'SupplierID'), false)
  checks.push('positive visibility control saves inverse Hidden and omits unchanged supplier mutation')
  await edit(); await editor().getByRole('button', { name: t('infraLink.createAsset'), exact: true }).click()
  await dialog().getByLabel(t('infra.name'), { exact: true }).fill('Created primary bill')
  assert.equal(await dialog().getByRole('checkbox').count(), 0)
  await dialog().getByRole('button', { name: t('common.save'), exact: true }).click(); await dialog().waitFor({ state: 'hidden' })
  await editor().getByRole('button', { name: t('infraLink.unlink'), exact: true }).waitFor(); assert.equal(linked.id, 8); assert.equal(linked.amount_minor, 4500); assert.equal(info.auto_renewal.enabled, true)
  await unlink(); assert.equal(linked, null); assert.equal(info.auto_renewal.enabled, true)
  checks.push('create from saved billing and unlink preserve shared values and online expiry extension')
  await editor().getByRole('button', { name: t('infraLink.linkExisting'), exact: true }).click()
  await dialog().getByRole('textbox', { name: t('infra.asset'), exact: true }).click()
  for (const name of ['Unsupported two-month bill', 'Another server bill', 'Archived bill']) assert.equal(await page.getByRole('option', { name: new RegExp(name) }).count(), 0)
  await page.getByRole('option', { name: /Existing annual bill/ }).click()
  await dialog().getByText(t('infraLink.currentServer'), { exact: true }).waitFor(); await dialog().getByText(t('infraLink.selectedAsset'), { exact: true }).waitFor()
  assert.equal(await dialog().getByRole('checkbox').count(), 0)
  await dialog().getByRole('button', { name: t('common.save'), exact: true }).click(); await dialog().waitFor({ state: 'hidden' })
  await editor().getByRole('button', { name: t('infraLink.unlink'), exact: true }).waitFor(); assert.equal(linked.id, 5)
  await page.waitForFunction(() => document.querySelector('[data-testid="node-information-editor"] input[value="20"]'))
  assert.equal(await amount().inputValue(), '20'); assert.equal(info.auto_renewal.enabled, true)
  assert.equal(await editor().getByRole('switch', { name: t('nodeCost.autoRenewal') }).isEnabled(), true)
  await supplierSelect().click(); assert.equal(await page.getByRole('option', { name: t('infraLink.unlinkedProvider'), exact: true }).count(), 0); await page.keyboard.press('Escape')
  checks.push('existing active bill preview filters unsupported and occupied assets; linked canonical values stay editable and auto extension remains enabled')
  await editor().getByRole('button', { name: t('nodeInformation.cancel'), exact: true }).click()
  await page.goto(new URL('nodes/1/information#billing-link', base).href); await editor().locator('#billing-link').waitFor()
  assert.equal(await amount().inputValue(), '20')
  checks.push('billing hash opens information editor and existing relation controls')
  for (const lang of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const tr = JSON.parse(await readFile(resolve(source, `src/i18n/${lang}.json`), 'utf8'))
    await page.evaluate(lang => localStorage.setItem('i18nextLng', lang), lang); await page.setViewportSize({ width: 390, height: 844 })
    await page.goto(new URL('nodes/1/information', base).href)
    await information().getByRole('button', { name: tr.nodeInformation.edit, exact: true }).waitFor()
    await page.waitForTimeout(300)
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${lang}: read-only overflow`)
    if (lang === 'zh-CN' && evidence) { await information().evaluate(el => el.scrollIntoView({ block: 'start' })); await page.evaluate(() => scrollBy(0, -76)); await page.screenshot({ path: resolve(evidence, 'information-readonly-390.png') }) }
    await information().getByRole('button', { name: tr.nodeInformation.edit, exact: true }).click()
    await editor().getByRole('textbox', { name: tr.nodeProbe.provider, exact: true }).scrollIntoViewIfNeeded()
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${lang}: editor overflow`)
    await editor().getByRole('button', { name: tr.nodeInformation.cancel, exact: true }).click()
  }
  checks.push('six languages fit 390px in read-only and editing modes')
  role = 'operator'; await page.evaluate(() => localStorage.setItem('i18nextLng', 'en'))
  const operatorStart = requests.length
  await page.goto(new URL('nodes/1/information', base).href); await edit()
  assert.equal(await amount().isDisabled(), true)
  assert.equal(await editor().getByLabel(t('nodeProbe.provider'), { exact: true }).getAttribute('readonly'), '')
  assert.equal(requests.slice(operatorStart).filter(r => r.path.startsWith('/api/admin/infrastructure')).length, 0)
  checks.push('operator linked financial fields stay read-only with no private infrastructure requests')
  assert.deepEqual(errors, []); assert.deepEqual(unexpected, []); assert.deepEqual(external, [])
  if (evidence) await writeFile(resolve(evidence, 'infrastructure-browser.json'), JSON.stringify({ checks, errors, unexpected, external, scope: 'Actual integrated admin UI; fully mocked local API' }, null, 2))
  console.log(`Infrastructure information browser passed: ${checks.length} checks`)
} catch (error) {
  if (evidence) { await page.screenshot({ path: resolve(evidence, 'infrastructure-failure.png') }); await writeFile(resolve(evidence, 'infrastructure-failure.json'), JSON.stringify({ error: error.message, errors, unexpected, external, checks }, null, 2)) }
  throw error
} finally { await browser.close() }
