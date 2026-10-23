#!/usr/bin/env node
// Run after pnpm build. Fictitious public data exercises chart layout and API boundaries.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { readFile, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { chromium } from 'playwright'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const out = process.argv[2] || '/tmp/probe-storage-charts'
const privacyOnly = process.argv.includes('--privacy-only')
await mkdir(out, { recursive: true })
const fixture = JSON.parse(await readFile(path.join(root, 'scripts/glass-fixture.json'), 'utf8'))
const base = fixture.snapshot.nodes[0], now = fixture.now, gib = 1024 ** 3, tib = 1024 ** 4
const system = { id: 'storage-1', name: '', kind: 'system', total: 463 * gib, used: 4.1 * gib }
const data = { id: 'storage-2', name: '', kind: 'pool', total: 14.3 * tib, used: 2.1 * tib }
let scenario = 'dual', scheme = 'light', sections = null, endpoint = 'ok', historyNames = null, historyTruncated = false, overrideSelection = null, holdHistory = false, releaseHistory = null
const requests = []
const selection = () => overrideSelection ?? (scenario === 'single' ? [system] : scenario === 'empty' ? [] : scenario === 'legacy' ? undefined : scenario === 'many' ? [system, data, { ...data, id: 'storage-3', name: '归档', total: 5 * tib }, { ...data, id: 'storage-4', name: '备份', total: 8 * tib }] : [system, data])
const snapshot = () => ({ ...fixture.snapshot, now, beat_seconds: 3, show_globe: false,
  public_sections: sections ?? ['cpu', 'memory', 'system', 'network', 'traffic', 'disk', 'history'],
  appearance: { preset: 'glass', scheme }, nodes: [{ ...structuredClone(base), id: 1, name: '存储布局验收', storage: selection(), last_seen: new Date(now * 1000).toISOString(), host: { ...structuredClone(base.host), disk_used: system.used, disk_total: system.total } }],
})
const history = () => [...fixture.history['1'], { ...fixture.history['1'].at(-1), ts: now }].map(p => ({ ...p, tcp: 13.6666666666666, udp: 26.5, procs: 179.8333333333333 }))
const storageHistory = url => {
  const to = Number(url.searchParams.get('to')) || now
  const hours = { '1h': 1, '4h': 4, '24h': 24, '7d': 168, '30d': 720 }[url.searchParams.get('range')] || 1
  const from = Number(url.searchParams.get('from')) || to - hours * 3600
  const step = Math.max(60, Math.ceil((to - from) / 60))
  return { res: 'm', from, to, step, truncated: historyTruncated, volumes: endpoint === 'empty' ? [] : (selection() ?? [system]).map((volume, index) => ({ id: volume.id, name: historyNames?.[index] ?? volume.name, kind: volume.kind,
    points: Array.from({ length: 61 }, (_, i) => ({ ts: from + i * step, used: scenario === 'missing' || (index === 1 && i < 20) ? null : volume.used * (0.4 + 0.6 * i / 60), total: scenario === 'missing' || (index === 1 && i < 20) ? null : volume.total })),
  })) }
}
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.webp': 'image/webp', '.jpg': 'image/jpeg', '.woff2': 'font/woff2' }
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://example.com')
    if (url.pathname.startsWith('/api/')) {
      requests.push(url.pathname + url.search)
      if (url.pathname.endsWith('/storage-history')) {
        if (endpoint === '404' || endpoint === '500') { res.writeHead(Number(endpoint)); res.end(); return }
        const body = JSON.stringify(storageHistory(url))
        if (holdHistory) { holdHistory = false; await new Promise(resolve => { releaseHistory = resolve }) }
        res.writeHead(200, { 'content-type': 'application/json' }); res.end(body); return
      }
      const response = url.pathname === '/api/probe' ? snapshot() : url.pathname === '/api/site' ? {} : { points: url.pathname.endsWith('/history') ? history() : [], from: now - 30 * 86400, to: now, step: 60, res: 'm' }
      res.writeHead(200, { 'content-type': 'application/json' }); res.end(JSON.stringify(response)); return
    }
    let pathname = decodeURIComponent(url.pathname)
    if (pathname.endsWith('/')) pathname += 'index.html'
    const filename = path.join(root, 'dist', pathname)
    if (!filename.startsWith(path.join(root, 'dist') + path.sep)) { res.writeHead(400); res.end(); return }
    res.writeHead(200, { 'content-type': mime[path.extname(filename)] || 'application/octet-stream' }); res.end(await readFile(filename))
  } catch { res.writeHead(404); res.end() }
})
server.listen(0, '127.0.0.1'); await once(server, 'listening')
const origin = `http://127.0.0.1:${server.address().port}`
const browser = await chromium.launch({ headless: true })
async function open(width) {
  const context = await browser.newContext({ viewport: { width, height: 1000 }, locale: 'zh-CN', colorScheme: scheme, reducedMotion: 'reduce' })
  await context.addInitScript(theme => { localStorage.setItem('i18nextLng', 'zh-CN'); localStorage.setItem('themeMode', theme) }, scheme)
  const page = await context.newPage()
  const errors = []; page.on('pageerror', e => errors.push(e.message))
  await page.clock.install({ time: now * 1000 })
  await page.goto(origin + '/glass/#/instance/1')
  await page.locator('[data-load-chart-card="process"]').waitFor()
  await page.locator('.loading-cover').waitFor({ state: 'hidden' })
  return { context, page, errors }
}
async function capacityReady(page, count) {
  await page.waitForFunction(expected => {
    const cards = [...document.querySelectorAll('[data-storage-chart]')]
    return cards.length === expected && cards.every(card => /GB|TB/.test(card.querySelector('[data-storage-chart-capacity]')?.textContent || ''))
  }, count)
}
try {
  if (!privacyOnly) {
  for (scheme of ['light', 'dark']) for (scenario of ['single', 'dual', 'many']) for (const width of scenario === 'dual' ? [1440, 1024, 390, 320] : [1440, 390]) {
    const { context, page, errors } = await open(width)
    const count = selection().length
    await capacityReady(page, count)
    const host = page.locator('[data-load-chart-row="host"] > [data-load-chart-card]')
    assert.deepEqual(await host.evaluateAll(cards => cards.map(card => card.dataset.loadChartCard)), ['cpu', 'memory', 'process'])
    const lower = page.locator('[data-load-chart-row="storage-network"] > [data-load-chart-card]')
    assert.deepEqual(await lower.evaluateAll(cards => cards.map(card => card.dataset.loadChartCard)), [...Array(count).fill('disk'), 'network', 'connections'])
    assert.match(await page.locator('[data-load-chart-card="process"]').innerText(), /179\.8/)
    assert.match(await page.locator('[data-load-chart-card="connections"]').innerText(), /TCP: 13\.7.*UDP: 26\.5/s)
    const capacities = await page.locator('[data-storage-chart-capacity]').allTextContents()
    assert.match(capacities[0], /4\.1 GB.*463\.0 GB/)
    if (count > 1) assert.match(capacities[1], /2\.10 TB.*14\.30 TB/)
    const boxes = await lower.evaluateAll(cards => cards.map(card => { const r = card.getBoundingClientRect(); return { top: r.top, left: r.left, width: r.width, right: r.right } }))
    assert(boxes.every(box => box.right <= width && box.width > 230), `${width}: narrow or overflowing graph card`)
    if (width >= 1280) {
      const firstRowCount = count === 1 ? 3 : 4
      assert.equal(new Set(boxes.slice(0, firstRowCount).map(b => b.top)).size, 1)
      if (boxes.length > firstRowCount) assert(boxes[firstRowCount].top > boxes[0].top)
    } else if (width < 768) assert.equal(new Set(boxes.map(b => b.top)).size, boxes.length)
    const style = await page.locator('html').getAttribute('class')
    assert.equal(style?.includes('dark') ?? false, scheme === 'dark')
    await page.screenshot({ path: path.join(out, `${scenario}-${width}-${scheme}.png`), fullPage: true })
    assert.deepEqual(errors, [])
    console.log(JSON.stringify({ scenario, width, scheme, cards: boxes.length, checks: 'order/columns/capacity/counts/theme/no-overflow' }))
    await context.close()
  }
  // Authoritative metadata, empty selections, legacy-only root fallback, and range requests.
  scheme = 'light'; scenario = 'dual'; historyNames = ['历史响应的系统', '历史响应的数据']
  {
    const { context, page } = await open(1440)
    await capacityReady(page, 2)
    assert.match(await page.locator('[data-storage-chart]').nth(0).innerText(), /历史响应的系统/)
    assert.match(await page.locator('[data-storage-chart]').nth(1).innerText(), /历史响应的数据/)
    const range = page.locator('[data-load-chart-range]')
    const fourHourResponse = page.waitForResponse(r => r.url().includes('storage-history?range=4h'))
    await range.getByRole('tab', { name: '4 小时', exact: true }).click()
    await fourHourResponse
    await capacityReady(page, 2)
    await range.getByRole('tab', { name: '自定义', exact: true }).click()
    await page.getByLabel('负载图开始时间').fill('2026-10-01T12:00')
    await page.getByLabel('负载图结束时间').fill('2026-10-01T13:00')
    const response = page.waitForResponse(r => /storage-history\?from=\d+&to=\d+/.test(r.url()))
    await page.getByRole('button', { name: '应用', exact: true }).first().click()
    await response
    await context.close()
  }
  historyNames = null
  // A reordered snapshot must cancel the old ordinal-ID response, not relabel its values.
  overrideSelection = [{ ...system, name: '旧系统' }, { ...data, name: '旧数据' }]
  holdHistory = true
  {
    const { context, page } = await open(1440)
    assert.equal(typeof releaseHistory, 'function')
    overrideSelection = [{ ...data, id: 'storage-1', name: '新数据', used: 3 * tib }, { ...system, id: 'storage-2', name: '新系统' }]
    await page.waitForFunction(() => document.querySelector('[data-storage-chart]')?.textContent.includes('新数据'))
    await capacityReady(page, 2)
    releaseHistory()
    await page.waitForTimeout(200)
    assert.match(await page.locator('[data-storage-chart]').nth(0).innerText(), /新数据.*3\.00 TB/s)
    assert.doesNotMatch(await page.locator('[data-load-chart-layout]').innerText(), /旧系统|旧数据/)
    await context.close()
  }
  overrideSelection = null
  }
  for (const check of privacyOnly ? ['known404'] : ['empty', 'authoritative-empty', 'legacy', 'missing', 'failed', 'known404', 'truncated', 'hidden']) {
    scenario = check === 'authoritative-empty' || check === 'failed' || check === 'known404' || check === 'hidden' ? 'dual' : check
    endpoint = check === 'authoritative-empty' ? 'empty' : check === 'legacy' || check === 'known404' ? '404' : check === 'failed' ? '500' : 'ok'
    sections = check === 'hidden' ? ['cpu', 'memory', 'system', 'network', 'traffic', 'history'] : null
    historyTruncated = check === 'truncated'
    const before = requests.length
    const { context, page } = await open(390)
    if (check === 'legacy') { await capacityReady(page, 1); assert.match(await page.locator('[data-storage-chart]').innerText(), /磁盘/) }
    else if (check === 'truncated') {
      await capacityReady(page, 2)
      assert.equal(await page.locator('[data-storage-history-truncated]').count(), 1)
      assert.match(await page.locator('[data-storage-history-truncated]').innerText(), /部分磁盘历史数据不完整/)
    }
    else if (check === 'missing' || check === 'failed' || check === 'known404') {
      await page.getByText(check === 'failed' ? '加载失败' : '暂无数据', { exact: true }).first().waitFor()
      assert.equal(await page.locator('[data-storage-chart]').count(), 2)
      assert((await page.locator('[data-storage-chart-capacity]').allTextContents()).every(value => !/GB|TB/.test(value)))
    } else {
      await page.waitForFunction(() => document.querySelectorAll('[data-storage-chart]').length === 0)
      if (check === 'hidden') assert(!requests.slice(before).some(url => url.includes('storage-history')))
    }
    console.log(JSON.stringify({ check, checks: 'safe metadata / missing history / no root resurrection' }))
    await context.close()
  }
} finally { await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)) }
console.log(`Storage chart browser checks passed; screenshots: ${out}`)
