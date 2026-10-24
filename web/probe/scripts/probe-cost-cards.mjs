#!/usr/bin/env node
// Run after pnpm build. Uses only fictitious public snapshots on localhost.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { readFile, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)))
const out = process.argv[2] || '/tmp/probe-cost-cards'
await mkdir(out, { recursive: true })
const fixture = JSON.parse(await readFile(path.join(root, 'scripts/glass-fixture.json'), 'utf8'))
const now = Date.parse('2026-10-23T00:00:00Z')
const allSections = ['cpu', 'memory', 'disk', 'network', 'traffic', 'system', 'history', 'info']
let sections = allSections, scheme = 'light', doubled = false, allMissing = false, extreme = false
let currency, missingRates = false, staleRates = false
const exchangeRates = () => ({ base: 'USD', source: 'Frankfurter', fetched_at: now / 1000, stale: staleRates,
  rates: { USD: 1, EUR: 2, AUD: 4, CNY: 8 },
  dates: { USD: '2026-10-23', EUR: '2026-10-22', AUD: '2026-10-23', CNY: '2026-10-23' },
})
const infos = () => [
  { billing: { amount: extreme ? '999999999999.9999' : doubled ? '240' : '120', currency: 'USD', cycle: 'annual' }, expires_at: '2027-10-23' },
  { billing: { amount: '30', currency: 'EUR', cycle: 'quarterly' }, expires_at: '2027-01-23' },
  { billing: { amount: '10', currency: 'AUD', cycle: 'monthly' }, expires_at: '2026-11-23' },
  { price: 'USD 9999 legacy note', expires_at: '2027-10-23' },
  { billing: { amount: '0', currency: 'EUR', cycle: 'annual' } },
  { billing: { amount: '36', currency: 'USD', cycle: 'custom', years: '1.5' }, expires_at: '2027-10-23' },
]
const snapshot = () => ({ ...fixture.snapshot, now: now / 1000, beat_seconds: 5, show_globe: true,
  finance_currency: currency, exchange_rates: missingRates ? undefined : exchangeRates(),
  public_sections: sections, appearance: { preset: 'glass', scheme }, appearance_scheme: scheme,
  // Leave info in hidden-section fixtures to additionally exercise UI gating.
  nodes: infos().map((info, i) => ({ ...structuredClone(fixture.snapshot.nodes[0]), id: i + 1,
    name: `Cost example ${i + 1}`, group: i === 0 ? 'Cost A' : 'Cost B',
    info: allMissing ? {} : info, last_seen: new Date(now).toISOString(), storage: [], recent: [],
  })),
})
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.webp': 'image/webp', '.jpg': 'image/jpeg', '.woff2': 'font/woff2' }
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://example.com')
    if (url.pathname.startsWith('/api/')) {
      const body = url.pathname === '/api/probe' ? snapshot() : url.pathname === '/api/site' ? {} : { points: [], volumes: [], from: now / 1000 - 3600, to: now / 1000, step: 60, res: 'm' }
      res.writeHead(200, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); return
    }
    let pathname = decodeURIComponent(url.pathname)
    if (pathname.endsWith('/')) pathname += 'index.html'
    const filename = path.join(root, 'dist', pathname)
    if (!filename.startsWith(path.join(root, 'dist') + path.sep)) { res.writeHead(400); res.end(); return }
    const content = await readFile(filename)
    res.writeHead(200, { 'content-type': mime[path.extname(filename)] || 'application/octet-stream' }); res.end(content)
  } catch { res.writeHead(404); res.end() }
})
server.listen(0, '127.0.0.1'); await once(server, 'listening')
const origin = `http://127.0.0.1:${server.address().port}`
const browser = await chromium.launch({ headless: true })
const homeCard = page => page.locator('[data-general-card-key="remainingValue"]')
const detail = (page, key) => page.locator(`[data-detail-card-key="${key}"]`)
async function open(width, route = '/', lang = 'zh-CN') {
  const context = await browser.newContext({ viewport: { width, height: 1000 }, locale: lang, colorScheme: scheme, reducedMotion: 'reduce' })
  await context.addInitScript(({ theme, lang }) => { localStorage.setItem('i18nextLng', lang); localStorage.setItem('themeMode', theme) }, { theme: scheme, lang })
  const page = await context.newPage(), errors = [], external = []
  page.on('pageerror', e => errors.push(e.message))
  page.on('request', r => { if (/^https?:/.test(r.url()) && !r.url().startsWith(origin + '/')) external.push(r.url()) })
  await page.clock.install({ time: now })
  await page.goto(`${origin}/glass/#${route}`)
  await page.locator('.loading-cover').waitFor({ state: 'hidden' })
  await page.evaluate(() => document.fonts.ready)
  return { context, page, errors, external }
}
async function noPageOverflow(page) {
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'horizontal page overflow')
}
async function snapshotShot(page, name) {
  await page.screenshot({ path: path.join(out, name + '.png'), fullPage: true, animations: 'disabled' })
}
try {
  for (scheme of ['light', 'dark']) for (const width of [1440, 390, 320]) {
    const { context, page, errors, external } = await open(width)
    await homeCard(page).locator('[data-finance-value]').waitFor()
    assert.match(await homeCard(page).innerText(), /1,100\s*CNY/)
    assert.equal(await homeCard(page).locator('[data-finance-coverage], [data-finance-rows]').count(), 0)
    assert(!/9999|USD|EUR|AUD/.test(await homeCard(page).innerText()), 'unconverted or legacy amount included')
    await noPageOverflow(page)
    await snapshotShot(page, `home-${width}-${scheme}`)
    await page.goto(`${origin}/glass/#/instance/1`)
    await detail(page, 'nodePrice').waitFor()
    assert.match(await detail(page, 'nodePrice').innerText(), /USD\s*120/)
    assert.match(await detail(page, 'monthlyCost').innerText(), /10/)
    assert.match(await detail(page, 'monthlyCost').innerText(), /USD/)
    assert.match(await detail(page, 'remainingValue').innerText(), /120/)
    await noPageOverflow(page)
    await snapshotShot(page, `detail-${width}-${scheme}`)
    assert.deepEqual(errors, []); assert.deepEqual(external, [])
    await context.close()
  }
  scheme = 'light'
  {
    const { context, page } = await open(390)
    await homeCard(page).locator('[data-finance-value]').waitFor()
    await page.getByRole('tab', { name: 'Cost A', exact: true }).click()
    await page.waitForFunction(() => {
      const card = document.querySelector('[data-general-card-key="remainingValue"]')
      return card && !card.querySelector('[data-finance-rows]') && /960\s*CNY/.test(card.textContent)
    })
    assert.match(await homeCard(page).innerText(), /960/)
    assert(!/EUR|AUD/.test(await homeCard(page).innerText()), 'group total includes other groups')
    await context.close()
  }
  // Currency and cached rates refresh without navigation; missing quotes must
  // never silently turn a mixed-currency total into a partial sum.
  {
    const { context, page } = await open(390)
    await homeCard(page).locator('[data-finance-value]').waitFor()
    currency = 'USD'
    await page.waitForFunction(() => /137.5\s*USD/.test(document.querySelector('[data-general-card-key="remainingValue"]')?.textContent || ''))
    missingRates = true
    await page.waitForFunction(() => /—/.test(document.querySelector('[data-general-card-key="remainingValue"]')?.textContent || ''))
    await homeCard(page).locator('[data-slot="data-tooltip"]').focus()
    await homeCard(page).getByRole('tooltip').filter({ hasText: '暂缺 AUD, EUR, USD 的汇率' }).waitFor({ state: 'visible' })
    await page.getByRole('tab', { name: 'Cost A', exact: true }).click()
    await page.waitForFunction(() => /120\s*USD/.test(document.querySelector('[data-general-card-key="remainingValue"]')?.textContent || ''))
    await context.close(); currency = undefined; missingRates = false
  }
  staleRates = true
  {
    const { context, page } = await open(1440)
    await homeCard(page).locator('[data-finance-value]').waitFor()
    assert.match(await homeCard(page).innerText(), /1,100\s*CNY/)
    await homeCard(page).locator('[data-slot="data-tooltip"]').focus()
    await page.getByText(/正在使用较早的缓存汇率/).waitFor({ state: 'visible' })
    assert.match(await page.locator('body').innerText(), /Frankfurter · 2026-10-22/)
    await snapshotShot(page, 'home-cached-rates-tooltip')
    assert(await homeCard(page).getByRole('tooltip').evaluate(el => {
      const r = el.getBoundingClientRect(); return r.top >= 0 && r.left >= 0 && r.right <= innerWidth
    }), 'full rates tooltip must fit the viewport')
    await context.close()
  }
  staleRates = false
  {
    const { context, page } = await open(390, '/instance/4')
    await detail(page, 'nodePrice').waitFor()
    assert.match(await detail(page, 'nodePrice').innerText(), /USD 9999 legacy note/)
    assert.match(await detail(page, 'monthlyCost').innerText(), /—/)
    assert.match(await detail(page, 'remainingValue').innerText(), /—/)
    await page.goto(`${origin}/glass/#/instance/6`)
    await detail(page, 'nodePrice').waitFor()
    assert.match(await detail(page, 'monthlyCost').innerText(), /2\s*USD|USD\s*2/)
    assert.match(await detail(page, 'remainingValue').innerText(), /—/)
    await page.goto(`${origin}/glass/#/instance/5`)
    await detail(page, 'nodePrice').waitFor()
    assert.match(await detail(page, 'nodePrice').innerText(), /免费/)
    assert.match(await detail(page, 'remainingValue').innerText(), /0/)
    await context.close()
  }
  // A fresh snapshot must replace the displayed price without navigating away.
  {
    const { context, page } = await open(390, '/instance/1')
    await detail(page, 'monthlyCost').waitFor()
    doubled = true
    await page.waitForFunction(() => /20/.test(document.querySelector('[data-detail-card-key="monthlyCost"]')?.textContent || ''))
    sections = allSections.filter(s => s !== 'info')
    await page.waitForFunction(() => !document.querySelector('[data-detail-card-key="nodePrice"]'))
    assert.equal(await detail(page, 'remainingValue').count(), 0)
    await page.goto(`${origin}/glass/#/`)
    await page.locator('.node-card').first().waitFor()
    assert.equal(await homeCard(page).count(), 0)
    await context.close(); doubled = false
  }
  sections = ['info']
  {
    const { context, page } = await open(390, '/instance/1')
    await detail(page, 'nodePrice').waitFor()
    assert.match(await detail(page, 'monthlyCost').innerText(), /USD/)
    await context.close()
  }
  sections = allSections; allMissing = true
  {
    const { context, page } = await open(390)
    await homeCard(page).waitFor()
    assert.match(await homeCard(page).innerText(), /—/)
    assert.equal(await homeCard(page).locator('[data-finance-currency]').count(), 0)
    await context.close()
  }
  allMissing = false
  extreme = true
  {
    const { context, page } = await open(320)
    await homeCard(page).waitFor()
    await page.getByRole('tab', { name: 'Cost A', exact: true }).click()
    await page.waitForFunction(() => {
      const card = document.querySelector('[data-general-card-key="remainingValue"]')
      return card && !card.querySelector('[data-finance-rows]')
    })
    assert.match(await homeCard(page).innerText(), /CNY/)
    await noPageOverflow(page)
    await snapshotShot(page, 'home-320-large-amount')
    assert(await homeCard(page).evaluate(card => {
      const bounds = card.getBoundingClientRect()
      const amount = card.querySelector('[data-finance-value]')?.getBoundingClientRect()
      return amount && amount.top >= bounds.top && amount.bottom <= bounds.bottom && amount.right <= bounds.right
    }), 'large amount and currency must fit the overview card')
    await context.close()
  }
  extreme = false
  for (const lang of ['en', 'zh-TW', 'ja', 'ko', 'ru']) {
    const { context, page, errors } = await open(390, '/instance/1', lang)
    await detail(page, 'monthlyCost').waitFor()
    assert.match(await detail(page, 'monthlyCost').innerText(), /USD/)
    await noPageOverflow(page)
    assert.deepEqual(errors, [])
    await context.close()
  }
  console.log('Cost cards: six layouts, converted totals, currency/rate refresh, cached/missing rates, original detail currencies, unknown/free/custom prices, privacy and six locales passed')
} finally {
  await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve))
}
