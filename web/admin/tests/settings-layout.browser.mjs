// Mocked-API browser regression. Start the integrated admin Vite app first:
//   pnpm dev --host 127.0.0.1 --port 5634
// Run with node tests/settings-layout.browser.mjs. Optional environment:
// ADMIN_TEST_URL, PLAYWRIGHT_MODULE, CHROME_EXECUTABLE, SETTINGS_SOURCE_ROOT.
// SETTINGS_SOURCE_ROOT points to web/admin when testing another worktree.
// All API requests are fulfilled locally; external requests and WebSockets
// are blocked. This never logs into or writes to a running Captain backend.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = process.env.SETTINGS_SOURCE_ROOT || resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5634/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Use a local Vite server only')
const locales = ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko']
const dictionaries = Object.fromEntries(await Promise.all(locales.map(async locale => [locale, JSON.parse(await readFile(resolve(source, `src/i18n/${locale}.json`), 'utf8'))])))
const categories = ['site', 'account', 'subscription', 'business', 'proxy', 'monitoring', 'integrations', 'maintenance']
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })

const initialSubscription = { urls: ['https://sub.example.com'], short_links: true, auto_flags: true, single_plan: false, info_lines: ['{{PLAN}}'], hwid: { enabled: true, require: true, fallback_limit: 3, announce: 'Device limit' } }
const initialMail = { provider: 'smtp', from_name: 'Captain', from_address: 'mail@example.com', smtp: { host: 'smtp.example.com', port: 587, username: 'example', password: '', security: 'starttls' }, resend: { api_key: '' }, verify_registration: false, reminders: true, traffic_thresholds: [90], language: 'en' }
const initialTelegram = { bot_token: '', bot_username: 'example_bot', admin_chat_id: 123456, notify_orders: true, notify_tickets: true }
const initialBackup = { keep: 7, hour: 3, remote: '', remote_keep: 30, webdav: { url: '', username: '', password: '' }, s3: { endpoint: '', region: 'auto', bucket: '', prefix: '', access_key: '', secret_key: '', path_style: false }, encrypt: { mode: '', recipient: '', passphrase: '' } }
const update = { captain: { current: 'v1.45.0', latest: 'v1.45.0', release_build: true, has_update: false, has_backup: false, in_container: false } }

async function setup(locale = 'en', options = {}) {
  const context = await browser.newContext({ locale, viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' })
  await context.addInitScript(language => { localStorage.setItem('i18nextLng', language); localStorage.setItem('mantine-color-scheme-value', 'light') }, locale)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = {
    requests: [], unexpected: [], external: [], errors: [], failMailReads: !!options.failMailReads, failMailWrites: false,
    subscription: structuredClone(initialSubscription), mail: structuredClone(initialMail), telegram: structuredClone(initialTelegram), backup: structuredClone(initialBackup),
  }
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
    if (url.origin !== base.origin) { state.external.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) {
      if (method === 'GET' || method === 'HEAD') return route.continue()
      state.unexpected.push({ method, path }); return route.abort()
    }
    const body = request.postData() ? request.postDataJSON() : undefined
    state.requests.push({ method, path, body })
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
    if (method === 'PUT') {
      if (path === '/api/admin/settings/mail') {
        if (state.failMailWrites) return send({ error: 'Simulated mail save failure' }, 503)
        state.mail = structuredClone(body); return send({})
      }
      if (path === '/api/admin/settings/telegram') { state.telegram = structuredClone(body); return send({}) }
      if (path === '/api/admin/settings/subscription') {
        state.subscription = { ...state.subscription, ...body }
        if (Object.hasOwn(body, 'URLs')) { state.subscription.urls = body.URLs; delete state.subscription.URLs }
        return send(state.subscription)
      }
      if (path === '/api/admin/settings/backup') { state.backup = structuredClone(body); return send({}) }
    }
    if (method !== 'GET') { state.unexpected.push({ method, path }); return send({ error: 'Unexpected mocked write' }, 500) }
    switch (path) {
      case '/api/admin/me': return send({ id: 1, email: 'admin@example.com', role: 'admin', version: 'v1.45.0' })
      case '/api/site': return send({ name: 'Captain', branding: {}, theme: {} })
      case '/api/admin/system/update': return send(update)
      case '/api/admin/settings/mail': return state.failMailReads ? send({ error: 'Simulated mail load failure' }, 503) : send({ settings: state.mail, has_smtp_password: true, has_resend_key: false })
      case '/api/admin/settings/telegram': return send({ settings: state.telegram, has_token: true })
      case '/api/admin/settings/webhooks': return send({ settings: { endpoints: [] }, events: ['order.paid'] })
      case '/api/admin/settings/login-clients': return send({ clients: [], base_url: 'https://panel.example.com' })
      case '/api/admin/settings/subscription': return send(state.subscription)
      case '/api/admin/settings/client-catalog': return send({ clients: [], storage: { used_bytes: 0, max_bytes: 1024 ** 3, max_asset_bytes: 1024 ** 2 } })
      case '/api/admin/settings/clients': return send({ items: [] })
      case '/api/admin/settings/surplus': return send({ enabled: true })
      case '/api/admin/settings/invite': return send({ enabled: false, percent: 10, first_order_only: false, multi_level: false, level2: 0, level3: 0, payout: 'balance', min_withdraw_cents: 0, withdraw_methods: [] })
      case '/api/admin/settings/backup': return send({ available: true, settings: state.backup, has_webdav_password: false, has_s3_secret: false, has_encrypt_passphrase: false, status: {}, files: [], dir: '/var/lib/captain/backups' })
      case '/api/admin/admin-log': return send([])
      default: state.unexpected.push({ method, path }); return send({ error: 'Unexpected mocked read' }, 500)
    }
  })
  const page = await context.newPage()
  page.setDefaultTimeout(10_000)
  page.on('pageerror', error => state.errors.push(error.message))
  const t = (key, values = {}) => {
    const value = key.split('.').reduce((part, name) => part?.[name], dictionaries[locale])
    assert.equal(typeof value, 'string', `Missing ${locale} translation: ${key}`)
    return value.replace(/\{\{(\w+)\}\}/g, (_, name) => values[name] ?? `{{${name}}}`)
  }
  return { context, page, state, t }
}

function categoryNav(run) { return run.page.getByRole('navigation', { name: run.t('settingsLayout.categoriesLabel'), exact: true }) }
function section(run, id) { return run.page.locator(`section#${id}`) }
function save(run, id, title) { return section(run, id).getByRole('button', { name: run.t('settingsLayout.saveSection', { section: run.t(title) }), exact: true }) }
function writes(run, path) { return run.state.requests.filter(request => request.method !== 'GET' && (!path || request.path === path)) }
async function eventually(read, check, message) {
  const deadline = Date.now() + 10_000
  let value
  do { value = await read(); if (check(value)) return value; await new Promise(resolve => setTimeout(resolve, 25)) } while (Date.now() < deadline)
  assert.fail(`${message}: ${JSON.stringify(value)}`)
}
async function gotoCategory(run, category) {
  await categoryNav(run).getByRole('link', { name: run.t(`settingsLayout.categories.${category}`), exact: true }).click()
}
async function assertCategoryTabs(run) {
  const nav = categoryNav(run)
  await nav.waitFor()
  assert.equal(await nav.getByRole('link').count(), 8)
  for (const category of categories) await nav.getByRole('link', { name: run.t(`settingsLayout.categories.${category}`), exact: true }).waitFor()
}
function assertOnlyIntegrationReads(run) {
  const allowed = new Set(['/api/admin/me', '/api/site', '/api/admin/system/update', ...['mail', 'telegram', 'webhooks', 'login-clients'].map(id => `/api/admin/settings/${id}`)])
  assert.deepEqual(run.state.requests.filter(request => request.method === 'GET' && !allowed.has(request.path)), [], 'Hidden categories must not fetch settings')
}
async function finish(run) {
  assert.deepEqual(run.state.errors, [], 'Runtime JavaScript errors')
  assert.deepEqual(run.state.unexpected, [], 'Unmocked API calls are never sent to the backend')
  await run.context.close()
}

try {
  const run = await setup()
  const { page, t } = run
  await page.goto(new URL('settings/mail', base).href)
  await page.waitForURL('**/settings/category/integrations#mail')
  await assertCategoryTabs(run)
  const mailName = section(run, 'mail').getByRole('textbox', { name: t('mail.fromName'), exact: true })
  const chat = section(run, 'telegram').getByRole('textbox', { name: t('telegram.adminChat'), exact: true })
  await mailName.fill('Unsaved mail name')
  await chat.fill('654321')
  await page.getByRole('navigation', { name: t('settingsLayout.jumpTo'), exact: true }).getByRole('link', { name: t('telegram.title'), exact: true }).click()
  await page.waitForURL('**/settings/category/integrations#telegram')
  assert.equal(await page.getByRole('dialog').count(), 0, 'Hash navigation within a category should keep drafts without a leave dialog')
  assert.equal(await mailName.inputValue(), 'Unsaved mail name')
  assert.equal(await chat.inputValue(), '654321')
  await save(run, 'mail', 'mail.title').click()
  await eventually(() => writes(run, '/api/admin/settings/mail').length, count => count === 1, 'Mail saved once')
  await eventually(() => save(run, 'mail', 'mail.title').isEnabled(), Boolean, 'Save completed')
  assert.equal(run.state.mail.from_name, 'Unsaved mail name')
  assert.equal(await chat.inputValue(), '654321', 'Saving mail must not clear the Telegram draft')
  assert.equal(writes(run, '/api/admin/settings/telegram').length, 0)
  await page.getByRole('status').filter({ hasText: t('workspace.unsaved') }).waitFor()
  assertOnlyIntegrationReads(run)
  await gotoCategory(run, 'business')
  let dialog = page.getByRole('dialog', { name: t('workspace.unsaved'), exact: true })
  await dialog.getByRole('button', { name: t('workspace.stay'), exact: true }).click()
  assert.equal(new URL(page.url()).hash, '#telegram')
  assert.equal(await chat.inputValue(), '654321')
  await gotoCategory(run, 'business')
  await dialog.getByRole('button', { name: t('workspace.discard'), exact: true }).click()
  await page.waitForURL('**/settings/category/business')
  await section(run, 'purchase').getByRole('switch', { name: t('common.enabled'), exact: true }).check()
  await save(run, 'purchase', 'settings.singlePlan').click()
  await eventually(() => writes(run, '/api/admin/settings/subscription').length, count => count === 1, 'Purchase saved once')
  assert.deepEqual(writes(run, '/api/admin/settings/subscription')[0].body, { single_plan: true })
  assert.deepEqual(run.state.subscription, { ...initialSubscription, single_plan: true })
  await eventually(() => page.getByRole('status').filter({ hasText: t('workspace.unsaved') }).count(), count => count === 0, 'Purchase draft cleared')
  await gotoCategory(run, 'subscription')
  await page.waitForURL('**/settings/category/subscription')
  await section(run, 'subscription').locator('textarea').first().fill('https://new-sub.example.com')
  assert.equal(await section(run, 'subscription').getByText(t('settings.singlePlan'), { exact: true }).count(), 0)
  await save(run, 'subscription', 'settings.subUrls').click()
  await eventually(() => writes(run, '/api/admin/settings/subscription').length, count => count === 2, 'Subscription saved once')
  const payload = writes(run, '/api/admin/settings/subscription')[1].body
  assert.equal(Object.hasOwn(payload, 'single_plan'), false)
  assert.deepEqual(payload.URLs, ['https://new-sub.example.com'])
  assert.deepEqual(payload.hwid, initialSubscription.hwid)
  assert.deepEqual(payload.info_lines, initialSubscription.info_lines)
  assert.equal(payload.short_links, true)
  assert.equal(run.state.subscription.single_plan, true)
  await eventually(() => page.getByRole('status').filter({ hasText: t('workspace.unsaved') }).count(), count => count === 0, 'Subscription draft cleared')
  // Searching another category changes route and focuses the matching editor.
  await page.getByRole('textbox', { name: t('workspace.search'), exact: true }).fill('SMTP')
  await page.getByRole('region', { name: t('settingsLayout.searchResults'), exact: true }).getByRole('link').filter({ hasText: t('mail.title') }).click()
  await page.waitForURL('**/settings/category/integrations#mail')
  await section(run, 'mail').getByRole('textbox', { name: t('mail.fromName'), exact: true }).waitFor()
  assert.equal(await section(run, 'telegram').getByRole('textbox', { name: t('telegram.adminChat'), exact: true }).inputValue(), '123456', 'Discarded sibling draft must not return')
  await finish(run)

  const failure = await setup('en', { failMailReads: true })
  await failure.page.goto(new URL('settings/category/integrations', base).href)
  const failureChat = section(failure, 'telegram').getByRole('textbox', { name: failure.t('telegram.adminChat'), exact: true })
  await failureChat.fill('777777')
  await section(failure, 'mail').getByRole('button', { name: failure.t('workspace.retry'), exact: true }).waitFor()
  assert.ok((await section(failure, 'mail').innerText()).includes(failure.t('workspace.loadError')))
  assert.equal(await section(failure, 'mail').getByRole('textbox').count(), 0, 'Failed loads must not expose editable empty defaults')
  failure.state.failMailReads = false
  await section(failure, 'mail').getByRole('button', { name: failure.t('workspace.retry'), exact: true }).click()
  await section(failure, 'mail').getByRole('textbox', { name: failure.t('mail.fromName'), exact: true }).fill('Retry draft')
  assert.equal(await failureChat.inputValue(), '777777', 'Retrying a sibling query must preserve other drafts')
  failure.state.failMailWrites = true
  await save(failure, 'mail', 'mail.title').click()
  await failure.page.getByText('Simulated mail save failure', { exact: true }).waitFor()
  assert.equal(await failureChat.inputValue(), '777777')
  assert.equal(await section(failure, 'mail').getByRole('textbox', { name: failure.t('mail.fromName'), exact: true }).inputValue(), 'Retry draft')
  failure.state.failMailWrites = false
  await save(failure, 'mail', 'mail.title').click()
  await eventually(() => failure.state.mail.from_name, name => name === 'Retry draft', 'Mail write retry completed')
  await failure.page.getByRole('status').filter({ hasText: failure.t('workspace.unsaved') }).waitFor()
  assert.equal(await failureChat.inputValue(), '777777')
  assert.equal(writes(failure, '/api/admin/settings/telegram').length, 0)
  assertOnlyIntegrationReads(failure)
  await finish(failure)

  const maintenance = await setup()
  await maintenance.page.goto(new URL('settings/category/maintenance', base).href)
  const restart = section(maintenance, 'update').getByRole('button', { name: maintenance.t('update.restart'), exact: true })
  const reset = section(maintenance, 'reset').getByRole('button', { name: maintenance.t('siteReset.open'), exact: true })
  await eventually(() => restart.isEnabled(), Boolean, 'Restart initially available')
  assert.equal(await reset.isEnabled(), true)
  await section(maintenance, 'backup').getByRole('textbox', { name: maintenance.t('backup.keep'), exact: true }).fill('14')
  await eventually(() => restart.isDisabled(), Boolean, 'Unsaved backup disables restart')
  assert.equal(await reset.isDisabled(), true, 'Unsaved backup disables site reset')
  await restart.evaluate(button => button.click())
  await reset.evaluate(button => button.click())
  assert.equal(await maintenance.page.getByRole('dialog').count(), 0)
  assert.deepEqual(writes(maintenance), [], 'Disabled restart/reset must not submit any action')
  await save(maintenance, 'backup', 'backup.title').click()
  await eventually(() => restart.isEnabled(), Boolean, 'Saving the backup draft re-enables restart')
  assert.equal(await reset.isEnabled(), true)
  assert.deepEqual(writes(maintenance).map(({ method, path }) => ({ method, path })), [{ method: 'PUT', path: '/api/admin/settings/backup' }])
  await finish(maintenance)

  for (const locale of locales) {
    const translated = await setup(locale)
    await translated.page.goto(new URL('settings/mail', base).href)
    await translated.page.waitForURL('**/settings/category/integrations#mail')
    await assertCategoryTabs(translated)
    await save(translated, 'mail', 'mail.title').waitFor()
    await eventually(() => section(translated, 'mail').evaluate(element => element === document.activeElement), Boolean, 'Legacy anchor focused before typing a search')
    await translated.page.getByRole('textbox', { name: translated.t('workspace.search'), exact: true }).fill('SMTP')
    const matches = translated.page.getByRole('region', { name: translated.t('settingsLayout.searchResults'), exact: true }).getByRole('link')
    await matches.first().waitFor()
    assert.equal(await matches.count(), 1, `${locale} SMTP search should match mail`)
    await matches.click()
    await translated.page.waitForURL('**/settings/category/integrations#mail')
    assertOnlyIntegrationReads(translated)
    await finish(translated)
  }
  console.log('PASS: 8 categories, six locales, SMTP search, legacy redirect, hash drafts, cancel/discard, isolated saves, query isolation, failure/retry, and maintenance action guards')
} finally {
  await browser.close()
}
