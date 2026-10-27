// Local Vite only. APIs are mocked and external requests blocked.
import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.SITE_TEST_URL || 'http://127.0.0.1:5623/'
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const site = { name: 'Example', homepage_enabled: false, tagline: 'Example service', description: '', features: [{ title: 'Feature', text: 'Description' }], faq: [{ q: 'Question', a: 'Answer' }], links: { download: 'https://example.com/download', telegram: 'https://example.com/contact', tos: 'https://example.com/terms', github: 'https://example.com/project' }, show_plans: true, registration: true, service_status_url: '/service-status', probe_url: '/status', locations: [], hub: null, stats: { nodes: 0, online: 0, users: 0, locations: 0 } }
const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } })
const errors = []
context.on('page', page => page.on('pageerror', error => errors.push(error.message)))
await context.route('**/*', route => {
  const url = new URL(route.request().url())
  if (url.origin !== new URL(base).origin) return route.abort()
  const send = body => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
  if (url.pathname === '/api/site') return send(site)
  if (url.pathname === '/api/site/clients' || url.pathname === '/api/portal/plans') return send([])
  if (url.pathname === '/api/portal/ref') return send({})
  if (url.pathname === '/api/site/service-status') return send({ generated_at: Math.floor(Date.now() / 1000), refresh_seconds: 60, entries: [], summary: {} })
  if (url.pathname.startsWith('/portal/')) return route.fulfill({ contentType: 'text/html', body: '<h1>Portal destination</h1>' })
  if (url.pathname.startsWith('/api/')) throw new Error('Unexpected API ' + url.pathname)
  return route.continue()
})
try {
  const page = await context.newPage()
  for (const path of ['downloads', 'service-status']) {
    await page.goto(base + path)
    await page.locator('.site-brand[aria-label="Example user center"]').waitFor()
    assert.equal(await page.locator('.site-brand').getAttribute('href'), '/portal/')
    assert.equal(await page.locator('.site-footer-brand').getAttribute('href'), '/portal/')
    assert.equal(await page.locator('a[href="/"], a[href^="/#"]').count(), 0)
    assert.equal(await page.locator('.site-footer a[href="/portal/plans"]').count(), 1)
    assert.equal(await page.locator('.site-footer a[href="/service-status"]').count(), 1)
    assert.equal(await page.locator('a[href="/status"], a[href="/portal/account"]').count(), 0)
    assert.equal(new URL(page.url()).pathname, '/' + path)
  }
  // The fallback protects a stale, already loaded SPA as well as fresh HTML.
  await page.goto(base + '?ref=INVITE&next=https://example.com')
  await page.getByRole('heading', { name: 'Portal destination' }).waitFor()
  assert.equal(new URL(page.url()).pathname + new URL(page.url()).search, '/portal/?ref=INVITE')
  delete site.homepage_enabled
  await page.goto(base + 'downloads')
  await page.locator('.site-brand[aria-label="Example home"]').waitFor()
  assert.equal(await page.locator('.site-brand').getAttribute('href'), '/')
  assert.equal(await page.locator('.site-footer a[href="/#plans"]').count(), 1)
  // Customer actions are direct links; only help opens a submenu. Footer-only
  // service/terms links must not reappear as top-level dropdowns.
  await page.goto(base)
  await page.locator('.site-footer a[href="/#plans"]').waitFor()
  const nav = page.locator('#site-navigation')
  assert.deepEqual(await nav.locator(':scope > a.site-nav-link').evaluateAll(links => links.map(link => link.getAttribute('href'))), ['/#plans', 'https://example.com/download', '/service-status'])
  assert.equal(await nav.locator('.site-nav-button').count(), 1)
  assert.equal(await page.locator('.site-footer-columns > div').count(), 3)
  assert.equal(await page.locator('a[href="/status"], a[href="/portal/account"]').count(), 0)
  assert.equal(await page.locator('.site-account-link:visible').count(), 1)
  const help = nav.locator('.site-nav-button')
  await help.focus()
  await page.keyboard.press('Enter')
  await page.locator('#site-nav-support a').first().focus()
  await page.keyboard.press('Escape')
  assert.equal(await help.getAttribute('aria-expanded'), 'false')
  assert.ok(await help.evaluate(button => document.activeElement === button))
  await help.click()
  await page.locator('.site-footer-brand').click()
  await page.locator('.site-brand').waitFor()
  assert.equal(await help.getAttribute('aria-expanded'), 'false')
  const screenshotDir = process.env.SITE_TEST_SCREENSHOTS
  if (screenshotDir) await mkdir(screenshotDir, { recursive: true })
  for (const language of ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko']) {
    await page.locator('.site-language-selector select').selectOption(language)
    await page.waitForFunction(value => document.documentElement.lang === value, language)
    await page.setViewportSize({ width: 1200, height: 900 })
    await page.evaluate(async () => {
      window.scrollTo({ top: 0, behavior: 'instant' })
      await document.fonts.ready
      await new Promise(requestAnimationFrame)
      await new Promise(requestAnimationFrame)
    })
    assert.equal(await page.locator('.site-header').count(), 1)
    assert.ok(await nav.isVisible())
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), language + ' desktop overflow')
    const bounds = await page.locator('.site-header-inner').evaluate(el => ({ left: el.getBoundingClientRect().left, right: el.getBoundingClientRect().right, children: [...el.children].flatMap(child => [...child.children]).filter(child => child.getBoundingClientRect().width > 0).map(child => ({ left: child.getBoundingClientRect().left, right: child.getBoundingClientRect().right })) }))
    assert.ok(bounds.children.every(child => child.left >= bounds.left - 1 && child.right <= bounds.right + 1), language + ' header overflow')
    if (screenshotDir && language === 'zh-CN') {
      await page.screenshot({ path: join(screenshotDir, 'desktop-light.png') })
      await page.locator('.site-theme-selector').click()
      await page.waitForFunction(() => document.documentElement.getAttribute('data-mantine-color-scheme') === 'dark')
      await page.screenshot({ path: join(screenshotDir, 'desktop-dark.png') })
      await page.locator('.site-theme-selector').click()
      await page.locator('.site-footer').screenshot({ path: join(screenshotDir, 'footer.png') })
      await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }))
    }
    await page.setViewportSize({ width: 390, height: 844 })
    assert.ok(!(await nav.isVisible()))
    await page.locator('.site-menu-toggle').click()
    await help.click()
    assert.equal(await page.locator('.site-account-link:visible').count(), 1)
    assert.ok(await page.locator('#site-nav-support a[href="/portal/connect"]').isVisible())
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), language + ' mobile overflow')
    if (screenshotDir && language === 'zh-CN') await page.screenshot({ path: join(screenshotDir, 'mobile-menu.png') })
    await page.keyboard.press('Escape')
    assert.ok(await nav.isVisible())
    await page.keyboard.press('Escape')
    assert.ok(!(await nav.isVisible()))
    assert.ok(await page.locator('.site-menu-toggle').evaluate(button => document.activeElement === button))
  }
  delete site.service_status_url
  site.links = {}
  site.faq = []
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(base + 'downloads')
  await page.locator('.site-footer a[href="/#plans"]').waitFor()
  assert.equal(await nav.locator('.site-nav-link').count(), 1)
  assert.equal(await page.locator('a[href="/status"], a[href="/service-status"], a[href="/downloads"]').count(), 0)
  await help.click()
  assert.equal(await page.locator('#site-nav-support a').count(), 1)
  assert.deepEqual(errors, [])
  console.log('PASS: standalone pages, homepage compatibility, direct customer navigation, optional links, six languages, desktop/mobile, keyboard, no server-monitoring links')
} finally { await browser.close() }
