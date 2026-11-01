// Local, synthetic API browser acceptance. No agent or core evidence is claimed.
// All API requests are intercepted; this verifies the real built UI only.
import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.RUNTIME_EVIDENCE_URL || 'http://127.0.0.1:5669/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname))
const artifacts = process.env.RUNTIME_EVIDENCE_ARTIFACTS || '/tmp/runtime-evidence-browser'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const now = new Date().toISOString(), cursor = 'opaque+cursor/one?two='
const node = { id: 1, name: 'Example relay', hostname: 'relay.example.com', version: 'fixture', platform: 'synthetic', public_addr: '192.0.2.10', internal_addr: '', v6_addr: '', domain: '', mode: 'full', paired: true, online: true, proxy_allowed: true, traffic_today_bytes: 0, inbounds: 1 }
const record = { id: 'example-delivery', session: 'example-session', config_revision: 'cfg1-' + 'a'.repeat(64), state_revision: 's'.repeat(64), issued_at: now, confirmed_at: now, observed_at: now, last_receipt_at: now, current: true, applied: true, fresh: true, fresh_for_ms: 180000, metadata_status: 'valid', evidence: { version: 1, status: 'observed', artifacts: [{ core: 'singbox-extended', digest: 'rcf1-' + 'b'.repeat(64), digest_scope: 'process', scope: 'activation_files', format: 'json', files: 2, processes: 1, activated_at: now, runtime_updates: true }], native_config: 'FORBIDDEN_NATIVE_CONFIG' } }
const rendered = { status: 'observed', config_revision: record.config_revision, state_revision: record.state_revision, current: record, history: [{ ...record, id: 'historical', current: false, fresh: false, fresh_for_ms: 0 }] }
const operation = { id: 'path_rollout:ExamplePath', source: 'path_rollout', source_id: 'ExamplePath', kind: 'edit', status: 'completed', evidence: 'historical', name: 'Example staged path', actor: 'admin@example.com', created_at: now, updated_at: now, stage: 'cleanup', line_id: 4, steps: [{ id: 'step1', node_id: 1, node_name: node.name, phase: 'expand', order: 1, attempt: 2, status: 'applied', issued_at: now, confirmed_at: now, observed_at: now, last_receipt_at: now, fresh_for_ms: 180000, receipt_applied: true }], attempts: [{ step_id: 'step1', attempt: 1, observed_at: now, received_at: now, applied: false }], related: [{ relation: 'compensates', operation_id: 'path_rollout:OlderPath', available: true }, { relation: 'compensated_by', operation_id: '', available: false }], attempts_truncated: true }
const operations = [operation, { id: 'node_upgrade:UpgradeOne', source: 'node_upgrade', source_id: 'UpgradeOne', kind: 'upgrade', status: 'observed', evidence: 'historical', created_at: now, node_id: 1, target_version: 'v0.87.0', name: 'Example relay' }, { id: 'node_job:ExampleJob', source: 'node_job', source_id: 'ExampleJob', kind: 'host_manage', status: 'waiting', evidence: 'historical', created_at: now, node_id: 1, action: 'apply', outcome: 'waiting' }]
const checks = []
async function setup(locale = 'en', width = 1440, role = 'admin') {
  const dictionary = JSON.parse(await readFile(resolve(root, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => key.split('.').reduce((value, part) => value?.[part], dictionary)
  const context = await browser.newContext({ viewport: { width, height: width === 390 ? 844 : 1000 }, serviceWorkers: 'block' })
  await context.addInitScript(locale => { localStorage.setItem('i18nextLng', locale); localStorage.setItem('mantine-color-scheme-value', 'light') }, locale)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { rendered: structuredClone(rendered), renderedCode: 200, operationCode: 200, list: structuredClone(operations), reads: [], writes: [], errors: [], external: [], hold: false, held: null }
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    if (request.method() !== 'GET') { state.writes.push({ path, method: request.method() }); return route.fulfill({ status: 500, json: { error: 'unexpected write' } }) }
    state.reads.push(path + url.search)
    const send = (json, status = 200) => route.fulfill({ status, json })
    if (path === '/api/admin/me') return send({ id: 1, email: role + '@example.com', role })
    if (path === '/api/site') return send({ name: 'Captain UI fixture', branding: {}, theme: {} })
    if (path === '/api/admin/system/update') return send({ captain: { current: 'fixture', latest: 'fixture', has_update: false } })
    if (path === '/api/admin/nodes/1') return send({ node, status: { cores: {}, certs: [], host: null }, inbounds: [], ingresses: [] })
    if (path === '/api/admin/nodes/1/rendered-config') { if (state.hold) { state.held = route; return } return send(state.renderedCode === 200 ? state.rendered : { error: 'synthetic access failure' }, state.renderedCode) }
    if (path === '/api/admin/operations') return send(state.operationCode === 200 ? { items: url.searchParams.has('before') ? [{ ...operation, id: 'line_rollout:Older', source: 'line_rollout', source_id: 'Older', name: 'Older record' }] : state.list, ...(url.searchParams.has('before') || !state.list.length ? {} : { next_before: cursor }) } : { error: 'synthetic access failure' }, state.operationCode)
    if (path.startsWith('/api/admin/operations/')) { const id = decodeURIComponent(path.slice('/api/admin/operations/'.length)); return send(state.operationCode === 200 ? state.list.find(item => item.id === id) || operation : { error: 'not found' }, state.operationCode) }
    if (path === '/api/admin/stats') return send({})
    return send([])
  })
  const page = await context.newPage(); page.setDefaultTimeout(10000)
  page.on('pageerror', e => state.errors.push(e.message))
  const main = page.locator('main')
  const goto = suffix => page.goto(new URL(suffix, base).href)
  const visible = text => main.getByText(text, { exact: true }).first().waitFor({ state: 'visible' })
  const overflow = async () => assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `${locale}/${width} horizontal overflow`)
  const screenshot = async name => { await overflow(); await page.screenshot({ path: resolve(artifacts, `${locale}-${width}-${name}.png`), fullPage: true }) }
  const close = async () => { assert.deepEqual(state.writes, []); assert.deepEqual(state.external, []); assert.deepEqual(state.errors, []); await context.close() }
  return { context, page, main, state, t, goto, visible, overflow, screenshot, close }
}
try {
  for (const locale of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const r = await setup(locale, 390)
    await r.goto('nodes/1/diagnostics?view=rendered-config'); await r.visible(r.t('renderedConfig.status.observed'))
    assert.equal(await r.main.getByText('FORBIDDEN_NATIVE_CONFIG').count(), 0)
    await r.visible(r.t('renderedConfig.runtimeUpdates'))
    await r.screenshot('rendered')
    await r.goto('operations'); await r.visible('Example staged path'); await r.screenshot('operations')
    await r.goto('operations/path_rollout%3AExamplePath'); await r.visible(r.t('operations.stepFresh')); await r.screenshot('operation-detail')
    const source = r.main.getByRole('link', { name: r.t('operations.openSource'), exact: true })
    assert.match(await source.getAttribute('href'), /operation_rollout=ExamplePath&operation_source=path_rollout/)
    assert.equal(await r.main.getByText(r.t('operations.attemptsTruncated'), { exact: true }).count(), 1)
    checks.push({ case: 'six-language mobile rendering, read-only and source navigation', locale, width: 390, result: 'pass' })
    await r.close()
  }
  const r = await setup()
  await r.goto('nodes/1/diagnostics?view=rendered-config'); await r.visible(r.t('renderedConfig.status.observed')); await r.screenshot('rendered')
  await r.context.setOffline(true); await r.visible(r.t('renderedConfig.status.unknown'))
  await r.context.setOffline(false); await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('renderedConfig.status.observed'))
  // Deterministic visibility fault injection, recorded as synthetic behavior.
  await r.page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')) })
  await r.visible(r.t('renderedConfig.status.unknown'))
  await r.page.evaluate(() => { delete document.visibilityState; document.dispatchEvent(new Event('visibilitychange')) })
  await r.visible(r.t('renderedConfig.status.observed'))
  r.state.rendered.current.fresh_for_ms = 700
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('renderedConfig.status.stale'))
  r.state.rendered = { ...structuredClone(rendered), status: 'unavailable', current: { ...structuredClone(record), evidence: { ...structuredClone(record.evidence), status: 'unavailable' } } }
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('renderedConfig.partial'))
  assert.equal(await r.main.getByText(r.t('renderedConfig.status.observed'), { exact: true }).count(), 0)
  r.state.rendered.current.metadata_status = 'invalid'
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('renderedConfig.metadata.invalid'))
  r.state.rendered = { ...structuredClone(rendered), status: 'unknown', current: null, history: [] }
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('renderedConfig.empty'))
  r.state.renderedCode = 403
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('renderedConfig.loadError'))
  r.state.renderedCode = 200; r.state.rendered = structuredClone(rendered)
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('renderedConfig.status.observed'))
  r.state.hold = true
  const aborted = r.page.waitForEvent('requestfailed', request => request.url().endsWith('/rendered-config'))
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click()
  await r.main.getByRole('link', { name: r.t('operations.title'), exact: true }).click()
  await aborted; r.state.hold = false; if (r.state.held) await r.state.held.abort().catch(() => {})
  await r.visible('Example staged path'); await r.screenshot('operations')
  await r.main.getByRole('button', { name: r.t('operations.older'), exact: true }).click(); await r.visible('Older record')
  assert.ok(r.state.reads.some(url => new URL(url, base).searchParams.get('before') === cursor))
  await r.main.getByRole('button', { name: r.t('operations.newer'), exact: true }).click(); await r.visible('Example staged path')
  await r.main.getByLabel(r.t('operations.nodeID'), { exact: true }).fill('1')
  await r.main.getByRole('button', { name: r.t('operations.filter'), exact: true }).click(); await r.visible('Example staged path')
  assert.equal(new URL(r.page.url()).searchParams.get('node_id'), '1')
  await r.goto('operations/node_job%3AExampleJob'); await r.visible(r.t('operations.waitingHint'))
  await r.goto('operations/node_upgrade%3AUpgradeOne'); await r.visible(r.t('operations.upgradeHint'))
  await r.goto('operations/path_rollout%3AExamplePath'); await r.visible(r.t('operations.stepFresh')); await r.screenshot('operation-detail')
  r.state.operationCode = 404
  await r.main.getByRole('button', { name: r.t('common.refresh'), exact: true }).click(); await r.visible(r.t('operations.detailError'))
  assert.equal(await r.main.getByText('Example staged path', { exact: true }).count(), 0)
  r.state.operationCode = 200; r.state.list = []
  await r.goto('operations'); await r.visible(r.t('operations.empty'))
  checks.push({ case: 'desktop lifecycle, offline, simulated visibility, expiry, partial/invalid/empty/error, real fetch abort on navigation, cursor/filter and historical semantics', result: 'pass' })
  await r.close()
  for (const role of ['operator', 'support']) {
    const r = await setup('en', 390, role)
    await r.goto('operations/path_rollout%3AExamplePath'); await r.page.waitForURL(url => url.pathname.replace(/\/$/, '') === base.pathname.replace(/\/$/, ''))
    assert.equal(r.state.reads.some(path => path.startsWith('/api/admin/operations')), false)
    // The denied route redirects to the original dashboard. Its API is outside this fixture.
    assert.deepEqual(r.state.writes, []); await r.context.close()
    checks.push({ case: 'admin-only history route denies role before fetching evidence', role, result: 'pass' })
  }
  await writeFile(resolve(artifacts, 'results.json'), JSON.stringify({ mode: 'Real built UI with synthetic local API responses; no backend/core provenance claimed', checks, readonly: true }, null, 2) + '\n')
  console.log(JSON.stringify({ checks: checks.length, result: 'pass', artifacts }))
} finally { await browser.close() }
