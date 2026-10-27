// Run against the local Vite server; all API traffic is mocked and external requests are blocked.
// PLAYWRIGHT_MODULE and CHROME_EXECUTABLE may point to an existing local browser installation.
import assert from 'node:assert/strict'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const base = process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5618/admin/'
const screenshots = await mkdtemp(join(tmpdir(), 'captain-site-settings-ui-'))
const original = {
 map_revision: 17, map_source: 'manual', status_map_source: 'entries', map_entry_ids: [11, 99],
 branding: { logo_light: '', logo_dark: '', favicon: '', title: 'Example tab' },
 name: 'Example', tagline: 'Original tagline', description: 'Original description', show_plans: true,
 links: { telegram: 'https://example.com/chat', tos: '', download: '', github: 'https://example.com/source' },
 hub: { name: 'Hub', lat: 0, lng: 135 }, locations: [{ name: 'Point', lat: 35, lng: 0 }],
 features: [{ title: 'Custom feature', text: 'Keep this text', icon: 'world' }], faq: [{ q: 'Custom question', a: 'Custom answer' }],
 theme: { primary: 'teal', radius: 'sm', scheme: 'dark', site_scheme: 'auto', font_family: 'sans-serif', portal_title: 'Members' },
 inject_head: '<meta name="example" content="head">', inject_body: '<div id="example-widget"></div>',
}
const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } })
const state = { settings: structuredClone(original), writes: [], conflict: false }
await context.addInitScript(() => localStorage.setItem('i18nextLng', 'en'))
await context.route('**/*', async route => {
 const req = route.request(), url = new URL(req.url())
 if (url.origin !== new URL(base).origin) return route.abort()
 if (!url.pathname.startsWith('/api/')) return route.continue()
 const send = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
 if (url.pathname === '/api/admin/settings/site') {
  if (req.method() === 'GET') return send(state.settings)
  state.writes.push(req.postDataJSON())
  if (state.conflict) return send({ error: 'Map revision conflict' }, 409)
  state.settings = { ...req.postDataJSON(), map_revision: state.settings.map_revision + 1 }
  return send(state.settings)
 }
 if (url.pathname === '/api/admin/entries') return send([{ ID: 11, Name: 'Example node' }])
 if (url.pathname === '/api/admin/me') return send({ id: 1, email: 'admin@example.com', role: 'admin' })
 return send({})
})
const page = await context.newPage(), errors = []
page.on('pageerror', error => errors.push(error.message))
try {
 await page.goto(`${base}site`)
 await page.getByRole('textbox', { name: 'Brand name', exact: true }).waitFor()
 const save = page.getByRole('button', { name: 'Save Site & branding', exact: true })
 const advanced = page.locator('#site-advanced details')
 assert.equal(await advanced.getAttribute('open'), null)
 assert.equal(await save.isDisabled(), true)
 // A deleted selection remains visible even with manual map locations selected.
 const nodes = page.getByRole('textbox', { name: 'Nodes shown on the homepage', exact: true })
 await nodes.click()
 await page.getByRole('option', { name: /99/ }).click()
 await page.keyboard.press('Escape')
 await save.waitFor({ state: 'visible' })
 assert.equal(await save.isEnabled(), true)
 await page.getByRole('textbox', { name: 'Tagline', exact: true }).fill('Updated tagline')
 await save.click()
 await page.waitForFunction(() => !document.querySelector('fieldset[aria-busy="true"]'))
 await page.getByText('Saved', { exact: true }).first().waitFor()
 assert.deepEqual(state.writes.at(-1), { ...original, tagline: 'Updated tagline', map_entry_ids: [11] })
 // Closing advanced settings must not hide a validation failure indefinitely.
 await page.locator('#site-advanced summary').click()
 await page.getByRole('textbox', { name: 'Features (JSON array: title, text, icon)', exact: true }).fill('{')
 await page.locator('#site-advanced summary').click()
 await save.click()
 await page.waitForFunction(() => document.querySelector('#site-advanced details').open)
 assert.equal(state.writes.length, 1)
 await page.getByRole('textbox', { name: 'Features (JSON array: title, text, icon)', exact: true }).fill(JSON.stringify(original.features))
 await page.locator('#site-advanced summary').click()
 await page.getByRole('textbox', { name: 'Logo on light backgrounds', exact: true }).fill('javascript:alert(1)')
 await save.click()
 assert.equal(state.writes.length, 1)
 await page.getByRole('textbox', { name: 'Logo on light backgrounds', exact: true }).fill('')
 // Concurrent map changes keep the current draft and prohibit an accidental retry.
 state.conflict = true
 await page.getByRole('textbox', { name: 'Tagline', exact: true }).fill('Unsaved conflict draft')
 await save.click()
 await page.getByText('Map settings or the node list changed. Your draft has not been saved. Reload and review the selection.', { exact: true }).waitFor()
 assert.equal(await save.isDisabled(), true)
 assert.equal(await page.getByRole('textbox', { name: 'Tagline', exact: true }).inputValue(), 'Unsaved conflict draft')
 assert.equal(state.writes.at(-1).map_revision, 18)
 await page.screenshot({ path: join(screenshots, 'desktop.png'), fullPage: true })
 await page.setViewportSize({ width: 390, height: 844 })
 await page.locator('#site-brand').scrollIntoViewIfNeeded()
 assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), true)
 const name = await page.getByRole('textbox', { name: 'Brand name', exact: true }).boundingBox()
 const title = await page.getByRole('textbox', { name: 'Browser tab title', exact: true }).boundingBox()
 assert.ok(title.y > name.y)
 await page.screenshot({ path: join(screenshots, 'mobile.png'), fullPage: true })
 assert.deepEqual(errors, [])
 console.log(`Screenshots: ${screenshots}`)
 console.log('PASS: full payload, folded validation, brand validation, stale CAS, explicit deleted-ID removal, mobile layout')
} finally { await browser.close() }
