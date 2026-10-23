#!/usr/bin/env node
// Run after pnpm build. Only fictitious public API data and this worktree's dist.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { readFile, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { chromium } from 'playwright'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const out = process.argv[2] || '/tmp/probe-storage-public'
await mkdir(out, { recursive: true })
const fixture = JSON.parse(await readFile(path.join(root, 'scripts/glass-fixture.json'), 'utf8'))
const base = fixture.snapshot.nodes[0]
const gib = 1024 ** 3, tib = 1024 ** 4
const system = { id: 'public-root', name: '', kind: 'system', total: 463 * gib, used: 4 * gib }
const pool = { id: 'public-pool', name: '', kind: 'pool', total: 14.32 * tib, used: 8.17 * 1024 ** 2 }
const longName = '一个很长的公开存储名称用于检查手机显示和完整名称查看 Archive volume with a long public name'
const selections = [[system], [system, pool], [system, { ...pool, name: longName }, { ...pool, id: 'archive', kind: 'volume', name: '归档' }, { ...pool, id: 'backup', kind: 'volume', name: '备份' }], [{ ...system, used: 1023 * gib, total: 1023 * gib }, { ...pool, total: null, used: null }], [], undefined]
let hidden = false, theme = 'glass', layout = 'grid', scheme = 'light'
const snapshot = () => ({ ...fixture.snapshot, now: Math.floor(Date.now() / 1000), beat_seconds: 3, show_globe: false,
  layout, appearance: { preset: theme === 'glass' ? 'glass' : 'aurora', scheme },
  public_sections: ['cpu','memory','system','network','traffic', ...(hidden ? [] : ['disk'])],
  nodes: selections.map((storage, i) => ({ ...structuredClone(base), id: i + 1, name: ['单盘','双盘','多盘','缺测','隐藏全部','旧服务端'][i], last_seen: new Date().toISOString(), storage, host: { ...structuredClone(base.host), disk_used: 4 * gib, disk_total: 463 * gib } })),
})
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.webp': 'image/webp', '.jpg': 'image/jpeg', '.woff2': 'font/woff2' }
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://example.com')
    if (url.pathname.startsWith('/api/')) {
      const data = url.pathname === '/api/probe' ? snapshot() : url.pathname === '/api/site' ? {} : { points: [], from: fixture.now - 86400, to: fixture.now, step: 60, res: 'm' }
      res.writeHead(200, { 'content-type': 'application/json' }); res.end(JSON.stringify(data)); return
    }
    let pathname = decodeURIComponent(url.pathname)
    if (pathname.endsWith('/')) pathname += 'index.html'
    const filename = path.join(root, 'dist', pathname)
    if (!filename.startsWith(path.join(root, 'dist') + path.sep)) { res.writeHead(400); res.end(); return }
    const body = await readFile(filename)
    res.writeHead(200, { 'content-type': mime[path.extname(filename)] || 'application/octet-stream' }); res.end(body)
  } catch { res.writeHead(404); res.end() }
})
server.listen(0, '127.0.0.1'); await once(server, 'listening')
const origin = `http://127.0.0.1:${server.address().port}`
const browser = await chromium.launch({ headless: true })
try {
  for (theme of ['glass', 'standard']) for (scheme of ['light', 'dark']) for (const width of [1280, 390, 320]) {
    hidden = false
    layout = 'grid'
    const context = await browser.newContext({ viewport: { width, height: 1000 }, locale: 'zh-CN', colorScheme: scheme, reducedMotion: 'reduce' })
    await context.addInitScript(scheme => { localStorage.setItem('i18nextLng', 'zh-CN'); localStorage.setItem('themeMode', scheme); localStorage.setItem('mantine-color-scheme-value', scheme); localStorage.setItem('nodeViewMode', 'card') }, scheme)
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    const url = origin + (theme === 'glass' ? '/glass/#/' : '/')
    await page.goto(url)
    await page.locator('[data-storage-summary]').last().waitFor()
    await page.locator('.loading-cover').waitFor({ state: 'hidden' })
    await page.evaluate(() => document.fonts.ready)
    assert.equal(await page.locator('html').getAttribute(theme === 'glass' ? 'class' : 'data-mantine-color-scheme').then(value => theme === 'glass' ? !!value?.includes('dark') : value === 'dark'), scheme === 'dark')
    assert.equal(await page.locator('[data-storage-summary]').count(), 6)
    assert.equal(await page.locator('[data-storage-summary]').nth(0).locator('[data-storage-volume]').count(), 1)
    assert.equal(await page.locator('[data-storage-summary]').nth(1).locator('[data-storage-volume]').count(), 2)
    assert.match(await page.locator('[data-storage-summary]').nth(2).innerText(), /另 2 个/)
    assert.match(await page.locator('[data-storage-summary]').nth(3).innerText(), /—/)
    assert.doesNotMatch(await page.locator('[data-storage-summary]').nth(4).innerText(), /4G|463G/)
    assert.match(await page.locator('[data-storage-summary]').nth(5).innerText(), /4G \/ 463G/)
    const sizes = await page.locator(theme === 'glass' ? '.node-card' : '.monitor-node').evaluateAll(nodes => nodes.map(n => n.getBoundingClientRect().height))
    assert.equal(new Set(sizes).size, 1, `${theme} ${width}: card heights ${sizes}`)
    const bounds = await page.locator('[data-storage-summary]').evaluateAll(nodes => nodes.map(n => ({ width: n.getBoundingClientRect().width, scroll: n.scrollWidth, right: n.getBoundingClientRect().right })))
    for (const rect of bounds) { assert(rect.scroll <= rect.width + 1); assert(rect.right <= width + 1) }
    if (theme === 'glass') {
      const dual = page.locator('[data-storage-summary]').nth(1)
      assert.deepEqual(await dual.locator('[data-storage-label]').allTextContents(), ['系统', '数据'])
      assert.equal(await dual.locator('[data-node-metric-icon="disk"]').count(), 2)
      const typography = await dual.locator('[data-storage-label], [data-storage-capacity]').evaluateAll(nodes => nodes.map(n => ({ label: n.hasAttribute('data-storage-label'), font: getComputedStyle(n).fontSize, width: n.clientWidth, scroll: n.scrollWidth })))
      for (const item of typography) {
        assert.equal(item.font, item.label ? '12px' : '11px')
        assert(item.scroll <= item.width + 1, `clipped default label ${JSON.stringify(item)}`)
      }
      assert.equal(await page.locator('[data-storage-summary]').nth(0).locator('[data-storage-label]').innerText(), '磁盘')
      assert.equal(await page.locator('[data-storage-summary]').nth(3).locator('[data-storage-percent]').first().innerText(), '100%')
      const spacing = await page.locator('.node-card').evaluateAll(cards => cards.map(card => {
        const summary = card.querySelector('[data-storage-summary]'), next = card.querySelector('[data-node-expiry]')
        return { bottom: Math.max(...Array.from(summary.querySelectorAll('*')).filter(n => getComputedStyle(n).visibility !== 'hidden').map(n => n.getBoundingClientRect().bottom)), nextTop: next.getBoundingClientRect().top }
      }))
      for (const rect of spacing) assert(rect.bottom <= rect.nextTop + 0.1, `storage/metadata overlap ${JSON.stringify(rect)}`)
      const rows = await page.locator('[data-storage-summary] [data-storage-volume]').evaluateAll(nodes => nodes.flatMap(n => {
        const label = n.querySelector('[data-storage-label]'), percent = n.querySelector('[data-storage-percent]'), capacity = n.querySelector('[data-storage-capacity]')
        if (!label || !percent || !capacity) return []
        const a = label.getBoundingClientRect(), b = percent.getBoundingClientRect(), c = capacity.getBoundingClientRect(), box = n.getBoundingClientRect()
        return [{ labelBottom: a.bottom, percentTop: b.top, percentBottom: b.bottom, capacityTop: c.top, percentRight: b.right, boxRight: box.right, percentWidth: percent.clientWidth, percentScroll: percent.scrollWidth }]
      }))
      for (const row of rows) {
        assert(row.labelBottom <= row.percentTop + 0.1, `label/percentage overlap ${JSON.stringify(row)}`)
        assert(row.percentBottom <= row.capacityTop + 0.1, `percentage/capacity overlap ${JSON.stringify(row)}`)
        assert(row.percentRight <= row.boxRight + 0.1)
        assert(row.percentScroll <= row.percentWidth + 1)
      }

    }
    const capacityBounds = await page.locator('[data-storage-summary] [data-storage-capacity]').evaluateAll(nodes => nodes.map(n => { const range = document.createRange(); range.selectNodeContents(n); return { text: n.textContent, width: n.getBoundingClientRect().width, scroll: range.getBoundingClientRect().width } }))
    for (const rect of capacityBounds) assert(rect.scroll <= rect.width + 0.1, `${theme} ${width}: clipped capacity ${JSON.stringify(rect)}`)
    await page.screenshot({ path: path.join(out, `${theme}-${scheme}-${width}.png`), fullPage: true })
    const initialURL = page.url()
    await page.locator('[data-storage-open]').nth(2).click()
    const dialog = page.getByRole('dialog')
    await dialog.waitFor()
    assert.equal(await dialog.locator('[data-storage-volume]').count(), 4)
    assert(await dialog.getByText(longName, { exact: true }).isVisible())
    assert.equal(page.url(), initialURL)
    await page.screenshot({ path: path.join(out, `${theme}-${scheme}-${width}-expanded.png`), fullPage: true })
    await page.keyboard.press('Escape')
    await dialog.waitFor({ state: 'hidden' })
    await page.locator('[data-storage-open]').nth(2).focus()
    await page.keyboard.press('Enter')
    await dialog.waitFor()
    assert.equal(await page.getByRole('dialog').count(), 1)
    assert.equal(page.url(), initialURL)
    await page.keyboard.press('Escape')
    await dialog.waitFor({ state: 'hidden' })
    if (theme === 'glass') {
      await page.evaluate(() => localStorage.setItem('nodeViewMode', 'list'))
      await page.reload()
      await page.locator('[data-storage-summary]').last().waitFor()
    await page.locator('.loading-cover').waitFor({ state: 'hidden' })
      await page.locator('[data-storage-open]').nth(2).click()
      await page.getByRole('dialog').waitFor()
      assert.equal(await page.getByRole('dialog').locator('[data-storage-volume]').count(), 4)
      await page.keyboard.press('Escape')
      await page.goto(origin + '/glass/#/instance/3')
      await page.locator('[data-storage-volumes]').waitFor()
      assert.equal(await page.locator('[data-storage-volumes] [data-storage-volume]').count(), 4)
      await page.screenshot({ path: path.join(out, `${theme}-${scheme}-${width}-detail.png`), fullPage: true })
    } else {
      await page.locator('.monitor-node').nth(2).click()
      await page.getByRole('dialog').waitFor()
      assert.equal(await page.getByRole('dialog').locator('[data-storage-volume]').count(), 4)
      await page.keyboard.press('Escape')
      layout = 'compact'
      await page.reload()
      await page.locator('[data-storage-summary]').last().waitFor()
      assert.equal(await page.locator('.monitor-compact').count(), 6)
      const compactBounds = await page.locator('[data-storage-capacity]').evaluateAll(nodes => nodes.map(n => { const range = document.createRange(); range.selectNodeContents(n); return { text: n.textContent, width: n.getBoundingClientRect().width, scroll: range.getBoundingClientRect().width } }))
      for (const rect of compactBounds) assert(rect.scroll <= rect.width + 0.1, `compact ${width}: clipped capacity ${JSON.stringify(rect)}`)
      await page.screenshot({ path: path.join(out, `${theme}-${scheme}-${width}-compact.png`), fullPage: true })
    }
    hidden = true
    await page.goto(url)
    await page.reload()
    await page.getByText('多盘', { exact: true }).first().waitFor()
    assert.equal(await page.locator('[data-storage-summary]').count(), 0)
    assert.equal(await page.locator('[data-storage-volume]').count(), 0)
    assert.deepEqual(errors, [])
    console.log(JSON.stringify({ theme, scheme, width, cardHeight: sizes[0], checks: 'single/dual/many/missing/empty/legacy/detail/more/keyboard/hidden/list-or-compact' }))
    await context.close()
  }
} finally {
  await browser.close()
  server.closeAllConnections(); await new Promise(resolve => server.close(resolve))
}
console.log(`Storage browser checks passed; screenshots: ${out}`)
