// Local browser regression with synthetic API fixtures; no production traffic.
import assert from 'node:assert/strict'
import { mkdtemp, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
const base = process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5687/admin/'
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname))
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const screenshots = await mkdtemp(join(tmpdir(), 'captain-install-packages-'))
const locales = ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko']
try {
  for (const locale of locales) {
    const messages = JSON.parse(await readFile(new URL(`../src/i18n/${locale}.json`, import.meta.url)))
    const context = await browser.newContext({ viewport: { width: locale === 'zh-CN' ? 390 : 1440, height: 1100 }, serviceWorkers: 'block' })
    await context.addInitScript(language => localStorage.setItem('i18nextLng', language), locale)
    await context.routeWebSocket('**/*', socket => socket.close())
    const errors = [], unexpected = [], writes = []
    const node = { id: 1, name: 'Example server', hostname: '', public_addr: '', internal_addr: '', domain: '', v6_addr: '', monitor_url: '', paired: false, online: false, proxy_allowed: true, mode: 'full', monitor_group: '', version: '', traffic_today_bytes: 0, inbounds: 0, pair_code: 'EXAMPLE-PAIR', effective_agent_url: 'https://agent.example.com:24443', download_source: 'captain' }
    await context.route('**/*', async route => {
      const r = route.request(), u = new URL(r.url())
      if (u.origin !== new URL(base).origin) return route.abort()
      if (!u.pathname.startsWith('/api/')) return route.continue()
      const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
      if (r.method() !== 'GET') { writes.push(`${r.method()} ${u.pathname}`); return send({}, 500) }
      switch (u.pathname) {
        case '/api/admin/me': return send({ id: 1, email: 'admin@example.com', role: 'admin', version: 'v1.56.0' })
        case '/api/site': return send({ name: 'Captain', branding: {}, theme: {} })
        case '/api/admin/dashboard': return send({})
        case '/api/admin/system/update': return send({ captain: { current: 'v1.56.0' } })
        case '/api/admin/nodes/1/service-control': case '/api/admin/nodes/2/service-control': case '/api/admin/nodes/3/service-control': return send({ node_id: 1, revision: 'r1', policy: { action: 'alert', threshold_percent: 100, limit_mbps: 0, auto_resume: true }, manual: { mode: 'normal', limit_mbps: 0 }, automatic: { active: false, mode: 'normal', limit_mbps: 0, exempt_current_period: false }, effective: { mode: 'normal', limit_mbps: 0, revision: 'r1', reasons: [] }, traffic: { monitoring_enabled: false, limit_bytes: 0, used_bytes: 0, fresh: false }, capability: { supported: false, reason: 'unpaired' }, runtime: { status: 'unconfigured' } })
        case '/api/admin/nodes/1': return send({ node, inbounds: [], ingresses: [] })
        case '/api/admin/nodes/2': return send({ node: { ...node, id: 2, download_source: 'github' }, inbounds: [], ingresses: [] })
        case '/api/admin/nodes/3': return send({ node: { ...node, id: 3, mode: 'monitor' }, inbounds: [], ingresses: [] })
        default: unexpected.push(u.pathname); return send({}, 500)
      }
    })
    const page = await context.newPage(); page.setDefaultTimeout(10000)
    page.on('pageerror', error => errors.push(error.message))
    const choice = page.getByRole('switch', { name: messages.nodes.installSystemPackages })
    const native = page.locator('pre').filter({ hasText: '/api/agent/install.sh' }).first()
    await page.goto(`${base}nodes/1`)
    await choice.waitFor()
    assert.equal(await choice.isChecked(), false)
    assert.doesNotMatch(await native.textContent(), /--system-packages/)
    await choice.check()
    assert.match(await native.textContent(), /--system-packages install/)
    await page.getByText(messages.nodes.installSystemPackagesHint, { exact: true }).waitFor()
    const dimensions = await native.evaluate(el => ({ width: el.clientWidth, scroll: el.scrollWidth }))
    assert.ok(dimensions.scroll <= dimensions.width + 2, `${locale} installation command overflow`)
    if (['en', 'zh-CN'].includes(locale)) { await page.waitForTimeout(250); await page.screenshot({ path: join(screenshots, `${locale}.png`), fullPage: true }) }
    await choice.uncheck()
    assert.doesNotMatch(await native.textContent(), /--system-packages/)
    await page.goto(`${base}nodes/2`)
    await choice.waitFor()
    assert.equal(await choice.isChecked(), true)
    const dockerBefore = await page.locator('pre').filter({ hasText: '--mode docker' }).textContent()
    await choice.uncheck()
    assert.match(await native.textContent(), /--system-packages skip/)
    assert.equal(await page.locator('pre').filter({ hasText: '--mode docker' }).textContent(), dockerBefore)
    await page.goto(`${base}nodes/3`)
    await page.getByText(messages.nodes.installCmd, { exact: true }).waitFor()
    assert.equal(await choice.count(), 0, 'monitor/OpenWrt installation has no network enforcement package option')
    assert.doesNotMatch(await native.textContent(), /--system-packages/)
    assert.deepEqual(writes, [], 'switching local installation options does not save server settings')
    assert.deepEqual(errors, [])
    assert.deepEqual(unexpected, [])
    await context.close()
  }
  console.log(`PASS: six locales, Captain/GitHub defaults and overrides, Docker unchanged, monitor/OpenWrt omitted. Screenshots: ${screenshots}`)
} finally { await browser.close() }
