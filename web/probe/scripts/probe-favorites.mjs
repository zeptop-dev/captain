#!/usr/bin/env node
// Browser regression for detail-only favorites, using fictitious public data.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { readFile, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)))
const out = process.argv[2] || '/tmp/probe-favorites'
await mkdir(out, { recursive: true })
const fixture = JSON.parse(await readFile(path.join(root, 'scripts/glass-fixture.json'), 'utf8'))
let scheme = 'light'
const snapshot = () => ({ ...fixture.snapshot, beat_seconds: 60, show_globe: false,
  public_sections: ['cpu', 'memory', 'disk', 'network', 'traffic', 'system', 'info'],
  appearance: { preset: 'glass', scheme }, appearance_scheme: scheme,
  nodes: ['Alpha', 'Beta', 'Gamma'].map((name, i) => ({ ...structuredClone(fixture.snapshot.nodes[0]),
    id: i + 1, name, group: i === 0 ? 'Primary' : 'Other', storage: [], recent: [],
  })),
})
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.webp': 'image/webp', '.jpg': 'image/jpeg', '.woff2': 'font/woff2' }
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://example.com')
    if (url.pathname.startsWith('/api/')) {
      const body = url.pathname === '/api/probe' ? snapshot() : { points: [], volumes: [] }
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
const filter = page => page.locator('[data-quick-control="favorite"]:visible')
const marker = page => page.locator('[data-node-favorite]:visible')
const node = (page, name) => page.getByRole('button', { name: `查看节点 ${name} 详情`, exact: true })
const add = page => page.getByRole('button', { name: '收藏当前节点', exact: true })
const remove = page => page.getByRole('button', { name: '取消收藏当前节点', exact: true })
const home = async page => {
  await page.getByRole('button', { name: /返回首页|Back.*Home/i }).click()
  await page.waitForURL('**/#/')
  await page.locator('.loading-cover').waitFor({ state: 'hidden' })
}
const waitCount = async (locator, expected) => {
  for (let i = 0; i < 40; i++) {
    if (await locator.count() === expected) return
    await locator.page().waitForTimeout(50)
  }
  assert.equal(await locator.count(), expected)
}
const openNode = async (page, name) => {
  await waitCount(node(page, name), 1) // Wait for outgoing filter animations.
  await node(page, name).click()
}

try {
  for (scheme of ['light', 'dark']) for (const width of [1440, 390, 320]) {
    const context = await browser.newContext({ viewport: { width, height: 1000 }, colorScheme: scheme, locale: 'zh-CN', reducedMotion: 'reduce' })
    await context.addInitScript(() => localStorage.setItem('i18nextLng', 'zh-CN'))
    const page = await context.newPage(), errors = []
    page.on('pageerror', e => errors.push(e.message))
    await page.goto(`${origin}/glass/#/`)
    await node(page, 'Alpha').waitFor()
    await page.locator('.loading-cover').waitFor({ state: 'hidden' })
    await page.evaluate(() => document.fonts.ready)
    await waitCount(filter(page), 0); await waitCount(marker(page), 0)
    assert.equal(await page.getByRole('button', { name: /^(取消)?收藏 / }).count(), 0)
    await page.screenshot({ path: path.join(out, `empty-${width}-${scheme}.png`), fullPage: true })

    // Detail is the only toggle. The homepage marker is not a nested button.
    await openNode(page, 'Alpha'); await add(page).click(); await remove(page).waitFor()
    await home(page); await filter(page).waitFor(); await waitCount(marker(page), 1)
    assert.match(await filter(page).innerText(), /1/)
    assert.equal(await marker(page).getAttribute('role'), 'img')
    assert.equal(await marker(page).getAttribute('tabindex'), null)
    assert(await marker(page).locator('svg path').evaluateAll(paths => paths.some(p => getComputedStyle(p).fill !== 'none')), 'favorite marker must be filled')
    await page.screenshot({ path: path.join(out, `saved-${width}-${scheme}.png`), fullPage: true })
    await page.reload(); await node(page, 'Alpha').waitFor(); await waitCount(marker(page), 1)

    // Selecting a group with no matching favorites must drop the hidden filter.
    await filter(page).click(); await waitCount(page.locator('.node-card:visible'), 1)
    await page.getByRole('tab', { name: 'Other', exact: true }).click()
    await node(page, 'Beta').waitFor(); await waitCount(filter(page), 0)
    await page.getByRole('tab', { name: '全部节点', exact: true }).click()
    await node(page, 'Gamma').waitFor(); await filter(page).waitFor()

    // Search changes use the same count as the filter's visibility.
    await filter(page).click()
    await page.getByRole('textbox', { name: /搜索节点|Search nodes/ }).fill('Beta')
    await node(page, 'Beta').waitFor(); await waitCount(filter(page), 0)
    await page.getByRole('button', { name: /清空搜索|Clear search/ }).click()
    await node(page, 'Gamma').waitFor(); await filter(page).waitFor()

    // List view also exposes a status marker only; clicking it opens details.
    await page.getByRole('button', { name: /列表视图|List view/ }).click()
    await waitCount(marker(page), 1)
    await marker(page).click(); await remove(page).waitFor()
    assert.deepEqual(await page.evaluate(() => JSON.parse(localStorage.getItem('theme:favorite-nodes:v1'))), ['1'])
    await home(page)
    await page.getByRole('button', { name: /卡片视图|Card view/ }).click()

    // Remove the last favorite while the cached home view has its filter active.
    await filter(page).click(); await openNode(page, 'Alpha')
    await remove(page).click(); await add(page).waitFor(); await home(page)
    await node(page, 'Gamma').waitFor(); await waitCount(filter(page), 0); await waitCount(marker(page), 0)

    // Real cross-tab storage events must also clear the invisible filter.
    await openNode(page, 'Alpha'); await add(page).click(); await home(page)
    await filter(page).click()
    const second = await context.newPage()
    await second.goto(`${origin}/glass/#/instance/1`)
    await remove(second).click(); await add(second).waitFor()
    await node(page, 'Beta').waitFor(); await waitCount(filter(page), 0); await waitCount(marker(page), 0)
    await second.close()
    // Saved IDs for unpublished/deleted nodes must not show a phantom count.
    await page.evaluate(() => localStorage.setItem('theme:favorite-nodes:v1', '["999"]'))
    await page.reload(); await node(page, 'Alpha').waitFor()
    await waitCount(filter(page), 0); await waitCount(marker(page), 0)
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    assert.deepEqual(errors, [])
    await context.close()
  }
  console.log('Favorites: six layouts; detail-only toggles, persistence, cards/list markers, empty filters, groups/search and cross-tab removal passed')
} finally {
  await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve))
}
