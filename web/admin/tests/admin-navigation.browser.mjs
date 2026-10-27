// Start the integrated admin app: pnpm dev --host 127.0.0.1 --port 5644
// Run: node tests/admin-navigation.browser.mjs
// Optional: ADMIN_TEST_URL, NAVIGATION_SOURCE_ROOT (web/admin),
// PLAYWRIGHT_MODULE, CHROME_EXECUTABLE. NAVIGATION_SCENARIO selects one case;
// NAVIGATION_LANGUAGE and NAVIGATION_WIDTH narrow responsive checks only.
// Every API request is mocked; external
// requests and WebSockets are blocked. No real backend or credentials are used.
import assert from 'node:assert/strict'
import { readFile, mkdtemp } from 'node:fs/promises'
import { dirname, resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'

const source = process.env.NAVIGATION_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5644/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Use a local Vite server only')
const locales = ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']
const dictionaries = Object.fromEntries(await Promise.all(locales.map(async language => [language, JSON.parse(await readFile(resolve(source, `src/i18n/${language}.json`), 'utf8'))])))
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const screenshots = await mkdtemp(join(tmpdir(), 'captain-admin-navigation-'))
const design = { groups: [], rules: [], final: '', enhancements: {} }
const mail = { provider: 'smtp', from_name: 'Example', from_address: 'mail@example.com', smtp: { host: 'smtp.example.com', port: 587, username: '', password: '', security: 'starttls' }, resend: { api_key: '' }, verify_registration: false, reminders: true, traffic_thresholds: [90], language: 'en' }
const emptyPage = { items: [], total: 0, page: 1, per_page: 50 }
const defaults = { clash: 'proxies: {{proxies}}\nproxy-groups: []\nrules: []', stash: 'proxies: {{proxies}}', surge: '[Proxy]\n{{proxies}}' }

async function setup({ role = 'admin', language = 'en', width = 1440, scheme = 'light' } = {}) {
  const context = await browser.newContext({ locale: language, viewport: { width, height: 1000 }, serviceWorkers: 'block' })
  await context.addInitScript(({ language, scheme }) => { if (location.protocol === 'http:') { localStorage.setItem('i18nextLng', language); localStorage.setItem('mantine-color-scheme-value', scheme) } }, { language, scheme })
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { requests: [], unexpected: [], errors: [], blockers: [], external: [] }
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) {
      if (method === 'GET' || method === 'HEAD') return route.continue()
      state.unexpected.push({ method, path }); return route.abort()
    }
    const body = request.postData() ? request.postDataJSON() : undefined
    state.requests.push({ method, path, body })
    const send = (value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
    // Inspection is a read-only POST; saving settings is never needed here.
    if (method === 'POST' && path === '/api/admin/settings/sub-design/inspect') return send({ effective_design: body.design, formats: [{ format: 'clash', supported: true, reasons: [] }] })
    if (method !== 'GET') { state.unexpected.push({ method, path }); return send({ error: 'Unexpected mock write' }, 500) }
    switch (path) {
      case '/api/admin/me': return send({ id: 1, email: `${role}@example.com`, role, version: 'v1.45.0' })
      case '/api/site': return send({ name: 'Example', branding: {}, theme: {} })
      case '/api/admin/system/update': return send({ captain: { current: 'v1.45.0', latest: 'v1.45.0', has_update: false }, bosun_latest: 'v0.82.0' })
      case '/api/admin/system/selfcheck': return send({ checks: [] })
      case '/api/admin/dashboard': return send({ stats: { users: 0, active_subs: 0, nodes: 0, nodes_online: 0, revenue_today_cents: 0, revenue_month_cents: 0, orders_pending: 0, traffic_today_bytes: 0, online_devices: 0 }, traffic: [], open_tickets: 0, attention: {} })
      case '/api/admin/users':
      case '/api/admin/orders':
      case '/api/admin/tickets':
      case '/api/admin/settings/infrastructure/payments': return send(emptyPage)
      case '/api/admin/nodes':
      case '/api/admin/entries':
      case '/api/admin/entry-categories':
      case '/api/admin/entries/tags':
      case '/api/admin/lines':
      case '/api/admin/groups':
      case '/api/admin/plans':
      case '/api/admin/renewals':
      case '/api/admin/monitoring':
      case '/api/admin/monitoring/dns':
      case '/api/admin/coupons':
      case '/api/admin/gifts/batches':
      case '/api/admin/withdrawals':
      case '/api/admin/articles':
      case '/api/admin/external/sources':
      case '/api/admin/external/nodes':
      case '/api/admin/settings/sub-profiles':
      case '/api/admin/settings/response-rules': return send([])
      case '/api/admin/nodes/layout': return send({ revision: 'layout-1', groups: [], nodes: [] })
      case '/api/admin/node-access': return send({ revision: 'fixture-1', groups: [], plans: [], inbounds: [] })
      case '/api/admin/domains': return send({ domains: [], global_token: false, can_issue: false })
      case '/api/admin/certificates': return send({ certificates: [], webhook_url: '', can_issue: false })
      case '/api/admin/settings/infrastructure': return send({ assets: [], suppliers: [], paid_totals: [] })
      case '/api/admin/monitoring/incidents': return send({ items: [], next: 0 })
      case '/api/admin/monitoring/windows': return send({ items: [], truncated: false, now: Math.floor(Date.now() / 1000) })
      case '/api/admin/settings/dns/history': return send({ items: [], more: false })
      case '/api/admin/speedtest': return send({ nodes: [], entries: [], probe_enabled: false })
      case '/api/admin/settings/sub-templates': return send({ templates: {}, defaults })
      case '/api/admin/settings/sub-library/templates': return send({ items: [], defaults })
      case '/api/admin/settings/sub-design': return send({ design, catalogue: [], presets: [], regions: [], tags: [], formats: ['clash', 'stash'] })
      case '/api/admin/settings/sub-design/claude-rules': {
        const snapshot = { source: 'builtin', revision: 'fixture', rules: [] }
        return send({ builtin: snapshot, library: { snapshot, synced_at: 0, count: 0, url: '', license_url: '' }, applied: { status: 'unknown' } })
      }
      case '/api/admin/settings/mail': return send({ settings: mail, has_smtp_password: false, has_resend_key: false })
      case '/api/admin/settings/telegram': return send({ settings: { bot_token: '', bot_username: '', admin_chat_id: 0, notify_orders: false, notify_tickets: false }, has_token: false })
      case '/api/admin/settings/webhooks': return send({ settings: { endpoints: [] }, events: [] })
      case '/api/admin/settings/login-clients': return send({ clients: [], base_url: 'https://panel.example.com' })
      default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected mock read' }, 500)
    }
  })
  const page = await context.newPage()
  page.setDefaultTimeout(10_000)
  page.on('pageerror', error => state.errors.push(error.message))
  page.on('console', message => { if (message.text().includes('only supports one blocker')) state.blockers.push(message.text()) })
  const t = key => {
    const value = key.split('.').reduce((part, key) => part?.[key], dictionaries[language])
    assert.equal(typeof value, 'string', `Missing ${language} translation: ${key}`)
    return value
  }
  return { context, page, state, t, role, language }
}

const sidebar = run => run.page.getByRole('navigation', { name: run.t('workspace.navigation'), exact: true })
const sections = run => run.page.getByRole('navigation', { name: run.t('workspace.sectionNavigation'), exact: true })
const href = path => base.pathname.replace(/\/$/, '') + (path === '/' ? '' : path)
const sectionLink = (run, key) => sections(run).getByRole('link', { name: run.t(key), exact: true })
const sideLink = (run, path) => sidebar(run).locator(`a[href="${href(path)}"]`)
const dirtyDialog = run => run.page.getByRole('dialog', { name: run.t('workspace.unsaved'), exact: true })
async function eventually(read, check, message) {
  const deadline = Date.now() + 10_000
  let value
  do { value = await read(); if (check(value)) return value; await new Promise(resolve => setTimeout(resolve, 25)) } while (Date.now() < deadline)
  assert.fail(`${message}: ${JSON.stringify(value)}`)
}
async function selected(run, modulePath, sectionKey) {
  await sidebar(run).waitFor()
  await eventually(() => sidebar(run).locator('a[aria-current="page"]').getAttribute('href'), value => value === href(modulePath), 'Exactly one active sidebar module')
  assert.equal(await sidebar(run).locator('a[aria-current="page"]').count(), 1)
  if (sectionKey !== undefined) {
    await sections(run).waitFor()
    assert.equal(await sections(run).locator('a[aria-current="page"]').count(), 1)
    await eventually(() => sectionLink(run, sectionKey).getAttribute('aria-current'), value => value === 'page', `Active child ${sectionKey}`)
  }
}
async function finish(run) {
  assert.deepEqual(run.state.errors, [], 'Runtime errors')
  assert.deepEqual(run.state.unexpected, [], 'Every API call is explicitly mocked')
  assert.deepEqual(run.state.blockers, [], 'No overlapping navigation blockers')
  assert.deepEqual(run.state.external, [], 'No external network')
  assert.deepEqual(run.state.requests.filter(r => r.method !== 'GET' && r.path !== '/api/admin/settings/sub-design/inspect'), [], 'Navigation and canceled drafts must not save data')
  await run.context.close()
}

// Independent expectations: production metadata cannot silently bless missing links.
const modules = [
  ['/', 'nav.dashboard'], ['/users', 'workspace.userManagement'], ['/nodes', 'nav.nodes'],
  ['/entries', 'entryFlow.title'], ['/sub-templates', 'workspace.subscriptionManagement'],
  ['/monitoring', 'workspace.monitoring'], ['/plans', 'workspace.transactions'], ['/tickets', 'workspace.customerService'],
]
const destinations = [
  ['/users', 'users.viewList', '/users', 3], ['/users?tab=groups', 'users.groups', '/users', 3], ['/users?tab=renewals', 'users.viewRenewals', '/users', 3],
  ['/nodes', 'workspace.serverList', '/nodes', 3], ['/infrastructure', 'nav.infrastructure', '/nodes', 3], ['/domains', 'nav.domains', '/nodes', 3],
  ['/entries', 'entryFlow.nodesTab', '/entries', 3], ['/entries?tab=lines', 'lines.tab', '/entries', 3], ['/external', 'nav.external', '/entries', 3],
  ['/sub-templates', 'workspace.templateEditor', '/sub-templates', 4], ['/sub-templates?mode=library', 'subProfiles.library', '/sub-templates', 4],
  ['/sub-templates?mode=profiles', 'subProfiles.profiles', '/sub-templates', 4], ['/sub-templates?mode=rules', 'subTemplates.modeRules', '/sub-templates', 4],
  ['/monitoring', 'nav.monitoring', '/monitoring', 4], ['/monitoring?tab=alerts', 'alerts.title', '/monitoring', 4], ['/monitoring?tab=dns', 'dnsHealth.title', '/monitoring', 4], ['/speedtest', 'nav.speedtest', '/monitoring', 4],
  ['/plans', 'nav.plans', '/plans', 5], ['/orders', 'nav.orders', '/plans', 5], ['/coupons', 'nav.coupons', '/plans', 5], ['/gifts', 'nav.gifts', '/plans', 5], ['/withdrawals', 'nav.withdrawals', '/plans', 5],
  ['/tickets', 'nav.tickets', '/tickets', 2], ['/articles', 'nav.articles', '/tickets', 2],
]
const open = (run, path) => run.page.goto(new URL(path.replace(/^\//, ''), base).href)
async function sidebarMatrix(run, expected = modules) {
  await sidebar(run).waitFor()
  assert.deepEqual(await sidebar(run).getByRole('link').evaluateAll(links => links.map(link => link.getAttribute('href'))), expected.map(([path]) => href(path)))
  for (const [path, key] of expected) assert.equal(await sideLink(run, path).innerText(), run.t(key))
  assert.equal(await sidebar(run).getByRole('button').count(), 0, 'No nested sidebar folders or More button')
  const settings = run.page.getByRole('link', {name: run.t('workspace.systemSettings'), exact:true})
  assert.equal(await settings.count(), run.role === 'admin' ? 1 : 0)
  if (run.role === 'admin') {
    assert.equal(await settings.getAttribute('href'), href('/settings/category/site'))
    assert.equal(await sidebar(run).getByRole('link', {name: run.t('workspace.systemSettings'), exact:true}).count(), 0, 'Settings is separate from workspace modules')
  }
}
async function cancel(run) { await dirtyDialog(run).waitFor(); await dirtyDialog(run).getByRole('button', {name:run.t('workspace.stay'),exact:true}).click(); await dirtyDialog(run).waitFor({state:'hidden'}) }
async function discard(run) { await dirtyDialog(run).waitFor(); await dirtyDialog(run).getByRole('button', {name:run.t('workspace.discard'),exact:true}).click(); await dirtyDialog(run).waitFor({state:'hidden'}) }
async function activeNoop(run, link) {
  const url = run.page.url()
  await link.click()
  await run.page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  assert.equal(await dirtyDialog(run).count(), 0, 'Current navigation item must not prompt to discard')
  assert.equal(run.page.url(), url)
}
const scenarios = {
  async 'text-discard'() {
    const run = await setup()
  await run.page.goto(new URL('sub-templates?mode=text&format=clash', base).href)
  const editor = run.page.locator('textarea:not([aria-hidden])')
  await eventually(() => editor.inputValue(), value => value === defaults.clash, 'Clash baseline')
  await editor.fill(defaults.clash + '\n# unsaved-clash')
  await run.page.getByRole('textbox', {name:run.t('subTemplates.format'),exact:true}).click()
  await run.page.getByRole('option', {name:'Surge',exact:true}).click()
  await dirtyDialog(run).waitFor()
  await dirtyDialog(run).getByRole('button',{name:run.t('workspace.discard'),exact:true}).click()
  await eventually(() => editor.inputValue(), value => value === defaults.surge, 'Surge baseline')
  await run.page.getByRole('textbox', {name:run.t('subTemplates.format'),exact:true}).click()
  await run.page.getByRole('option', {name:'mihomo / Clash Meta',exact:true}).click()
  await eventually(() => editor.inputValue(), value => value === defaults.clash, 'Discarded Clash draft must not reappear')
    await finish(run)
  },
  async routes() {
    const run = await setup()
    await open(run, '/')
    await sidebarMatrix(run)
    await selected(run, '/')
    assert.equal(await sections(run).count(), 0)
    for (const [path, key, module, count] of destinations) {
      await open(run, path)
      await selected(run, module, key)
      assert.equal(await sections(run).getByRole('link').count(), count, path)
      await activeNoop(run, sectionLink(run, key))
    }
    await open(run, '/users?tab=groups&filter=active')
    await sectionLink(run, 'users.viewRenewals').click()
    await run.page.waitForURL('**/users?tab=renewals&filter=active')
    await selected(run, '/users', 'users.viewRenewals')
    await sectionLink(run, 'users.viewList').click()
    await run.page.waitForURL('**/users?filter=active')
    await open(run, '/sub-templates?mode=text&format=stash')
    await selected(run, '/sub-templates', 'workspace.templateEditor')
    await activeNoop(run, sectionLink(run, 'workspace.templateEditor'))
    await sectionLink(run, 'subProfiles.profiles').click()
    await run.page.waitForURL('**/sub-templates?mode=profiles&format=stash')
    await run.page.goBack()
    await run.page.waitForURL('**/sub-templates?mode=text&format=stash')
    for (const [legacy, target, module, key] of [['/business/marketing?filter=active#old', '/coupons?filter=active#old', '/plans', 'nav.coupons'], ['/business/content?filter=active#old', '/articles?filter=active#old', '/tickets', 'nav.articles']]) {
      await open(run, legacy)
      await run.page.waitForURL(new URL(target.replace(/^\//, ''), base).href)
      await selected(run, module, key)
    }
    await finish(run)
  },
  async roles() {
    for (const role of ['operator', 'support']) {
      const run = await setup({role})
      const expected = role === 'operator' ? modules.filter(([path]) => path !== '/sub-templates') : [modules[0], modules[1], ['/orders', 'workspace.transactions'], modules[7]]
      await open(run, '/')
      await sidebarMatrix(run, expected)
      for (const [path] of expected) {
        await sideLink(run, path).click()
        await selected(run, path)
        if (role === 'support' || path === '/') assert.equal(await sections(run).count(), 0)
      }
      if (role === 'operator') {
        await open(run, '/nodes')
        assert.equal(await sections(run).getByRole('link').count(), 2)
        assert.equal(await sectionLink(run, 'nav.infrastructure').count(), 0)
      }
      const restricted = role === 'operator' ? ['/sub-templates?mode=text', '/infrastructure', '/settings/category/site'] : ['/users?tab=groups', '/users?tab=renewals', '/entries', '/settings/category/site']
      for (const path of restricted) {
        const before = run.state.requests.length
        await open(run, path)
        await run.page.waitForURL(new RegExp('/admin/?$'))
        assert.equal(await sections(run).count(), 0)
        assert.ok(!run.state.requests.slice(before).some(r => r.path.startsWith('/api/admin/settings/') || r.path === '/api/admin/entries' || r.path === '/api/admin/node-access'), 'Forbidden direct URL must not mount restricted editor')
      }
      await finish(run)
    }
  },
  async drafts() {
    const run = await setup()
    await open(run, '/sub-templates?mode=text&format=clash')
    const editor = run.page.locator('textarea:not([aria-hidden])')
    await eventually(() => editor.inputValue(), text => text === defaults.clash, 'Template loaded')
    await editor.fill(defaults.clash + '\n# keep-me')
    await activeNoop(run, sectionLink(run, 'workspace.templateEditor'))
    for (const link of [sectionLink(run, 'subTemplates.modeRules'), sideLink(run, '/users')]) {
      await link.click(); await cancel(run)
      assert.equal(await editor.inputValue(), defaults.clash + '\n# keep-me')
    }
    await sideLink(run, '/users').click(); await discard(run)
    await selected(run, '/users', 'users.viewList')
    await sideLink(run, '/sub-templates').click()
    const regions = run.page.getByRole('checkbox', {name:run.t('subDesign.regionsOption'), exact:true})
    await regions.check()
    await activeNoop(run, sectionLink(run, 'workspace.templateEditor'))
    await activeNoop(run, sideLink(run, '/sub-templates'))
    await sectionLink(run, 'subProfiles.library').click(); await cancel(run)
    assert.equal(await regions.isChecked(), true)
    await sideLink(run, '/users').click(); await discard(run)
    await selected(run, '/users', 'users.viewList')
    await open(run, '/settings/category/integrations#mail')
    const sender = run.page.getByRole('textbox', {name:run.t('mail.fromName'),exact:true})
    await eventually(() => sender.inputValue(), text => text === 'Example', 'Mail form hydrated')
    await sender.fill('Unsaved sender')
    await sideLink(run, '/users').click(); await cancel(run)
    assert.equal(await sender.inputValue(), 'Unsaved sender')
    await sideLink(run, '/users').click(); await discard(run)
    await selected(run, '/users', 'users.viewList')
    await finish(run)
  },
  async responsive() {
    const languages = process.env.NAVIGATION_LANGUAGE ? [process.env.NAVIGATION_LANGUAGE] : locales
    const widths = process.env.NAVIGATION_WIDTH ? [Number(process.env.NAVIGATION_WIDTH)] : [1440, 390]
    assert.ok(languages.every(language => locales.includes(language)))
    assert.ok(widths.every(width => width >= 320 && width <= 2560))
    for (const language of languages) for (const width of widths) for (const scheme of ['light', 'dark']) {
      const run = await setup({language, width, scheme})
      await open(run, '/plans')
      await sections(run).waitFor()
      assert.equal(await run.page.locator('html').getAttribute('data-mantine-color-scheme'), scheme)
      const geometry = await sections(run).evaluate(nav => ({rect:nav.getBoundingClientRect().toJSON(), viewport:innerWidth, links:[...nav.querySelectorAll('a')].map(a => a.getBoundingClientRect().toJSON())}))
      assert.ok(geometry.rect.x >= 0 && geometry.rect.right <= width + 1, `${language}/${width} section navigation fits viewport`)
      assert.ok(geometry.links.every(r => r.width > 0 && r.x >= geometry.rect.x - 1 && r.right <= geometry.rect.right + 1), `${language}/${width} links wrap inside navigation`)
      if (width < 768) {
        const burger = run.page.getByRole('button', {name:run.t('workspace.navigation'),exact:true})
        await burger.click()
        await eventually(() => sidebar(run).boundingBox(), rect => rect !== null && rect.x >= 0, 'Mobile drawer finishes opening')
        await sidebarMatrix(run)
        await sideLink(run, '/users').click()
        await sections(run).waitFor()
        await eventually(() => sidebar(run).boundingBox(), rect => rect === null || rect.x + rect.width <= 1, 'Mobile drawer closes after navigation')
        if (['en', 'ru', 'zh-CN'].includes(language)) await run.page.screenshot({path:join(screenshots, `${language}-${width}-${scheme}-closed.png`)})
        await burger.click()
        await eventually(() => sidebar(run).boundingBox(), rect => rect !== null && rect.x >= 0, 'Mobile drawer finishes reopening')
      } else await sidebarMatrix(run)
      const sideGeometry = await sidebar(run).evaluate(nav => ({rect:nav.getBoundingClientRect().toJSON(), links:[...nav.querySelectorAll('a')].map(a => ({rect:a.getBoundingClientRect().toJSON(),scroll:a.scrollWidth,client:a.clientWidth}))}))
      assert.ok(sideGeometry.links.every(({rect,scroll,client}) => rect.x >= sideGeometry.rect.x - 1 && rect.right <= sideGeometry.rect.right + 1 && scroll <= client + 1), `${language}/${width} sidebar labels fit`)
      if (['en', 'ru', 'zh-CN'].includes(language)) await run.page.screenshot({path:join(screenshots, `${language}-${width}-${scheme}.png`)})
      await finish(run)
    }
  },
}
try {
  const filter = process.env.NAVIGATION_SCENARIO
  assert.ok(!filter || filter in scenarios, 'Unknown NAVIGATION_SCENARIO')
  for (const [name, scenario] of Object.entries(scenarios)) if (!filter || name === filter) {
    await scenario()
    console.log(`PASS ${name}`)
  }
  console.log(`Navigation screenshots: ${screenshots}`)
} finally { await browser.close() }
