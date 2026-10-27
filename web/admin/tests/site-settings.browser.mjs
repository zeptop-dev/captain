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
const state = { settings: structuredClone(original), writes: [], conflict: false, failSave: false }
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
  if (state.failSave) return send({ error: 'Temporary save failure' }, 503)
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
 const homepage = page.locator('#site-content > details')
 const homepageAdvanced = homepage.locator('details')
 const homepageSummary = page.locator('#site-content > details > summary')
 const enabled = page.getByRole('switch', { name: /^Enable public homepage/ })
 const features = page.getByRole('textbox', { name: 'Features (JSON array: title, text, icon)', exact: true })
 const tagline = page.getByRole('textbox', { name: 'Tagline', exact: true })
 const waitForSave = async (action) => {
  const response = page.waitForResponse(response => response.url().endsWith('/api/admin/settings/site') && response.request().method() === 'PUT')
  await action()
  await response
  await page.waitForFunction(() => !document.querySelector('fieldset[aria-busy="true"]'))
 }
 // Legacy settings are enabled, and disabling cannot conceal a missing node reference.
 assert.equal(await enabled.isChecked(), true)
 assert.notEqual(await homepage.getAttribute('open'), null)
 assert.equal(await advanced.getAttribute('open'), null)
 assert.equal(await save.isDisabled(), true)
 await enabled.uncheck()
 assert.equal(await homepage.getAttribute('open'), null)
 assert.equal(await save.isDisabled(), true)
 await page.getByRole('button', { name: 'Homepage content', exact: true }).click()
 // A deleted selection remains visible even with manual map locations selected.
 const nodes = page.getByRole('textbox', { name: 'Nodes shown on the homepage', exact: true })
 await nodes.click()
 await page.getByRole('option', { name: /99/ }).click()
 await page.keyboard.press('Escape')
 await save.waitFor({ state: 'visible' })
 assert.equal(await save.isEnabled(), true)
 await tagline.fill('Updated tagline')
 await enabled.check()
 await waitForSave(() => save.click())
 assert.deepEqual(state.writes.at(-1), { ...original, homepage_enabled: true, tagline: 'Updated tagline', map_entry_ids: [11] })
 // Disabling folds only homepage settings and retains both content and unsaved drafts.
 await tagline.fill('Homepage draft retained')
 await enabled.uncheck()
 assert.equal(await homepage.getAttribute('open'), null)
 assert.equal(await tagline.isVisible(), false)
 assert.equal(await page.getByRole('textbox', { name: 'Brand name', exact: true }).isVisible(), true)
 assert.equal(await page.getByRole('textbox', { name: 'Telegram', exact: true }).isVisible(), true)
 assert.equal(await page.locator('#site-theme').isVisible(), true)
 assert.equal(await page.locator('#site-map').isVisible(), true)
 await page.getByRole('textbox', { name: 'Telegram', exact: true }).fill('https://example.com/new-chat')
 await page.getByRole('textbox', { name: 'Brand name', exact: true }).fill('Updated brand')
 await page.getByRole('textbox', { name: 'Service status map source', exact: true }).click()
 await page.getByRole('option', { name: 'Manual map locations', exact: true }).click()
 const disabledExpected = {
  ...original, homepage_enabled: false, map_revision: 18, map_entry_ids: [11],
  name: 'Updated brand', tagline: 'Homepage draft retained', status_map_source: 'manual',
  links: { ...original.links, telegram: 'https://example.com/new-chat' },
 }
 await waitForSave(() => save.click())
 assert.deepEqual(state.writes.at(-1), disabledExpected)
 await page.reload()
 await page.getByRole('textbox', { name: 'Brand name', exact: true }).waitFor()
 await page.waitForFunction(() => document.querySelector('#site-brand input').value === 'Updated brand')
 assert.equal(await enabled.isChecked(), false)
 assert.equal(await homepage.getAttribute('open'), null)
 assert.equal(await page.getByRole('link', { name: 'Open website entry', exact: true }).getAttribute('href'), '/')
 // Both collapsed levels reopen for invalid homepage JSON even while disabled.
 await homepageSummary.click()
 await homepageAdvanced.locator('summary').click()
 await features.fill('{')
 await homepageSummary.click()
 await save.click()
 await page.waitForFunction(() => document.querySelector('#site-content > details').open && document.querySelector('#site-content details details').open)
 assert.equal(state.writes.length, 2)
 assert.equal(await enabled.isChecked(), false)
 await features.fill(JSON.stringify(original.features))
 assert.equal(await tagline.inputValue(), 'Homepage draft retained')
 await enabled.check()
 assert.equal(await tagline.inputValue(), 'Homepage draft retained')
 await enabled.uncheck()
 // Shared map JSON and injection stay available; invalid shared JSON also reopens.
 await page.locator('#site-advanced summary').click()
 const locations = page.getByRole('textbox', { name: 'Manual locations (JSON array: name, lat, lng)', exact: true })
 assert.equal(await locations.inputValue(), JSON.stringify(original.locations, null, 2))
 assert.equal(await page.locator('#site-advanced textarea').count(), 3)
 await locations.fill('{')
 await page.locator('#site-advanced summary').click()
 await save.click()
 await page.waitForFunction(() => document.querySelector('#site-advanced details').open)
 assert.equal(state.writes.length, 2)
 await locations.fill(JSON.stringify(original.locations))
 await page.locator('#site-advanced summary').click()
 await page.getByRole('textbox', { name: 'Logo on light backgrounds', exact: true }).fill('javascript:alert(1)')
 await save.click()
 assert.equal(state.writes.length, 2)
 await page.getByRole('textbox', { name: 'Logo on light backgrounds', exact: true }).fill('')
 // A failed write keeps the disabled toggle and drafts; retry sends the same values.
 state.failSave = true
 await waitForSave(() => save.click())
 assert.deepEqual(state.writes.at(-1), { ...disabledExpected, map_revision: 19 })
 assert.equal(await enabled.isChecked(), false)
 state.failSave = false
 await waitForSave(() => save.click())
 assert.deepEqual(state.writes.at(-1), state.writes.at(-2))
 // Concurrent map changes keep the current draft and prohibit an accidental retry.
 state.conflict = true
 await homepageSummary.click()
 await tagline.fill('Unsaved conflict draft')
 await waitForSave(() => save.click())
 await page.getByText('Map settings or the node list changed. Your draft has not been saved. Reload and review the selection.', { exact: true }).waitFor()
 assert.equal(await save.isDisabled(), true)
 assert.equal(await tagline.inputValue(), 'Unsaved conflict draft')
 assert.equal(state.writes.at(-1).map_revision, 20)
 assert.equal(state.writes.at(-1).homepage_enabled, false)
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
 console.log('PASS: legacy enabled default, disabled hydration, folded draft retention, shared settings, full payload, nested validation, retry, stale CAS, deleted-ID removal, mobile layout')
} finally { await browser.close() }
