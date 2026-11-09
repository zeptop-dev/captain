// Real local browser + mocked APIs. No production or external requests.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
const base = process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5674/admin/'
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname))
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || '../../probe/node_modules/playwright/index.mjs')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const artifacts = process.env.ACCOUNT_INFO_ARTIFACTS || '/tmp/captain-account-info-browser'
await mkdir(artifacts, { recursive: true })

async function setup(locale = 'en', width = 1440) {
  const dict = JSON.parse(await readFile(new URL(`../src/i18n/${locale}.json`, import.meta.url)))
  const t = key => key.split('.').reduce((v, k) => v[k], dict)
  const context = await browser.newContext({ viewport: { width, height: 1000 }, serviceWorkers: 'block' })
  await context.addInitScript(lang => localStorage.setItem('i18nextLng', lang), locale)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { settings: { urls: ['https://sub.example.com'], info_lines: ['Legacy {{PLAN}}'], hwid: {}, account_info: { traffic: false, reset: false, expiry: false } }, writes: [], errors: [], failSave: false, failRead: false, unexpected: [] }
  await context.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname
    if (url.origin !== new URL(base).origin) return route.abort()
    if (!path.startsWith('/api/')) return route.continue()
    const send = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/admin/settings/subscription') {
      if (req.method() === 'GET') return state.failRead ? send({ error: 'Unavailable' }, 503) : send(state.settings)
      const body = req.postDataJSON(); state.writes.push(body)
      if (state.failSave) return send({ error: 'Unavailable' }, 503)
      state.settings = { ...state.settings, ...body }; return send(state.settings)
    }
    if (req.method() !== 'GET') { state.unexpected.push(path); return send({}, 500) }
    switch (path) {
      case '/api/admin/me': return send({ id: 1, role: 'admin', email: 'admin@example.com' })
      case '/api/site': return send({ name: 'Captain', branding: {}, theme: {} })
      case '/api/admin/system/update': return send({ captain: { current: 'v1.60.0' } })
      case '/api/admin/settings/client-catalog': return send({ clients: [], storage: { used_bytes: 0, max_bytes: 1, max_asset_bytes: 1 } })
      case '/api/admin/settings/clients': return send({ items: [] })
      case '/api/admin/settings/service-status': return send({ enabled: false, groups: [] })
      case '/api/admin/settings/sub-profiles': return send([])
      case '/api/admin/settings/sub-library/templates': return send({ items: [], defaults: { clash: 'proxies: []' } })
      case '/api/admin/entries': case '/api/admin/entry-categories': return send([])
      default: state.unexpected.push(path); return send({}, 500)
    }
  })
  const page = await context.newPage()
  page.setDefaultTimeout(10000)
  page.on('pageerror', e => state.errors.push(e.message))
  await page.goto(`${base}settings/category/subscription#subscription`)
  const card = page.locator('.mantine-Card-root').filter({ has: page.getByRole('heading', { name: t('accountInfo.title'), exact: true }) })
  await card.getByRole('checkbox', { name: t('accountInfo.traffic'), exact: true }).waitFor()
  const save = card.getByRole('button', { name: t('settingsLayout.saveSection').replace('{{section}}', t('accountInfo.title')) })
  return { state, page, context, card, save, t }
}

try {
  const run = await setup()
  const { card, save, page, state, t } = run
  assert.equal(await save.isDisabled(), true)
  const legacy = page.getByRole('textbox', { name: t('settings.infoLines'), exact: true })
  await legacy.fill('Unsaved legacy draft')
  await card.getByRole('checkbox', { name: t('accountInfo.traffic'), exact: true }).check()
  await card.getByRole('checkbox', { name: t('accountInfo.reset'), exact: true }).check()
  state.failSave = true
  await Promise.all([page.waitForResponse(r => r.url().endsWith('/api/admin/settings/subscription') && r.request().method() === 'PUT' && r.status() === 503), save.click()])
  await page.getByText('Unavailable', { exact: true }).waitFor()
  await page.waitForFunction(() => !document.querySelector('[aria-busy=true]'))
  assert.equal(await card.getByRole('checkbox', { name: t('accountInfo.traffic'), exact: true }).isChecked(), true)
  state.failSave = false
  await Promise.all([page.waitForResponse(r => r.url().endsWith('/api/admin/settings/subscription') && r.request().method() === 'PUT' && r.status() === 200), save.click()])
  await page.waitForFunction(name => [...document.querySelectorAll('button')].find(b => b.textContent === name)?.disabled, t('settingsLayout.saveSection').replace('{{section}}', t('accountInfo.title')))
  assert.equal(await save.isDisabled(), true)
  assert.equal(await legacy.inputValue(), 'Unsaved legacy draft')
  assert.deepEqual(state.writes[1], { account_info: { traffic: true, reset: true, expiry: false } })
  assert.deepEqual(state.settings.info_lines, ['Legacy {{PLAN}}'])
  const legacySave = page.getByRole('button', { name: t('settingsLayout.saveSection').replace('{{section}}', t('settings.subUrls')), exact: true })
  await Promise.all([page.waitForResponse(r => r.url().endsWith('/api/admin/settings/subscription') && r.request().method() === 'PUT'), legacySave.click()])
  await page.waitForFunction(() => !document.querySelector('[aria-busy=true]'))
  assert.deepEqual(state.settings.account_info, { traffic: true, reset: true, expiry: false })
  assert.equal('account_info' in state.writes[2], false)
  await page.reload()
  await card.getByRole('checkbox', { name: t('accountInfo.traffic'), exact: true }).waitFor()
  assert.equal(await card.getByRole('checkbox', { name: t('accountInfo.traffic'), exact: true }).isChecked(), true)
  await card.scrollIntoViewIfNeeded()
  await page.screenshot({ path: `${artifacts}/desktop.png`, fullPage: true })
  await page.goto(`${base}sub-templates?mode=profiles`)
  await page.getByRole('button', { name: t('subProfiles.newProfile'), exact: true }).click()
  const modal = page.getByRole('dialog')
  await modal.getByRole('checkbox', { name: t('accountInfo.override'), exact: true }).check()
  await modal.getByRole('checkbox', { name: t('accountInfo.expiry'), exact: true }).check()
  await modal.getByRole('checkbox', { name: t('accountInfo.override'), exact: true }).uncheck()
  assert.equal(await modal.getByRole('checkbox', { name: t('accountInfo.expiry'), exact: true }).count(), 0)
  assert.deepEqual(state.errors, [])
  assert.deepEqual(state.unexpected, [])
  await run.context.close()
  for (const locale of ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko']) {
    const mobile = await setup(locale, 390)
    await mobile.card.scrollIntoViewIfNeeded()
    assert.equal(await mobile.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    await mobile.page.screenshot({ path: `${artifacts}/${locale}-mobile.png`, fullPage: true })
    assert.deepEqual(mobile.state.errors, [])
    assert.deepEqual(mobile.state.unexpected, [])
    await mobile.context.close()
  }
  console.log('Account information browser checks passed: independent saves, failure drafts, reload, profile override, six mobile locales')
} finally { await browser.close() }
