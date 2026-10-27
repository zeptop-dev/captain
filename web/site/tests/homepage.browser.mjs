// Local Vite only. APIs are mocked and external requests blocked.
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.SITE_TEST_URL || 'http://127.0.0.1:5623/'
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const site = { name: 'Example', homepage_enabled: false, tagline: 'Example service', description: '', features: [{ title: 'Feature', text: 'Description' }], faq: [{ q: 'Question', a: 'Answer' }], links: {}, show_plans: true, registration: true, service_status_url: '/service-status', probe_url: '/status', locations: [], hub: null, stats: { nodes: 0, online: 0, users: 0, locations: 0 } }
const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } })
const errors = []
context.on('page', page => page.on('pageerror', error => errors.push(error.message)))
await context.route('**/*', route => {
  const url = new URL(route.request().url())
  if (url.origin !== new URL(base).origin) return route.abort()
  const send = body => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
  if (url.pathname === '/api/site') return send(site)
  if (url.pathname === '/api/site/clients' || url.pathname === '/api/site/plans') return send([])
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
    assert.equal(await page.locator('.site-footer a[href="/status"]').count(), 1)
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
  assert.deepEqual(errors, [])
  console.log('PASS: standalone pages, disabled navigation, root invitation redirect, legacy enabled default')
} finally { await browser.close() }
