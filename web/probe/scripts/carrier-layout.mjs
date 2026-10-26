#!/usr/bin/env node
// Synthetic browser regression for carrier cards and the fixed-height list.
// Usage: node scripts/carrier-layout.mjs --out <screenshots> [--dist <glass-dist>]
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = fileURLToPath(new URL('..', import.meta.url))
let dist = path.join(root, 'dist/glass'), out = ''
for (let i = 2; i < process.argv.length; i += 2) {
  const flag = process.argv[i], value = process.argv[i + 1]
  if (!value || !['--out', '--dist'].includes(flag)) throw Error('Expected --out <directory> [--dist <glass-dist>]')
  if (flag === '--out') out = path.resolve(value)
  else dist = path.resolve(value)
}
if (!out) throw Error('--out is required')
const base = JSON.parse(await readFile(path.join(root, 'scripts/glass-fixture.json'), 'utf8'))
base.snapshot.carrier_ping = true
base.snapshot.show_globe = false
base.snapshot.nodes = base.snapshot.nodes.slice(0, 3)
let mode = 'dual'
function fixture() {
  const f = structuredClone(base)
  for (const [index, node] of f.snapshot.nodes.entries()) {
    node.online = true
    node.name = ['Dual sample', 'Missing IPv6', 'Old agent'][index]
    node.host ??= {}
    node.host.carrier_probe_version = index === 2 ? 0 : 1
    node.ping_tasks = ['CT', 'CU', 'CM'].flatMap(name => (mode === 'dual' ? ['ipv4', 'ipv6'] : [mode]).map(family => ({
      id: 0, name, family, series: `${name}-${family}`, type: 'icmp',
      ...(index === 2 ? { state: 'unsupported', reason: 'agent_upgrade_required' }
        : index === 1 && family === 'ipv6' ? { state: 'unknown', reason: 'waiting' } : {}),
    })))
    node.host.pings = node.ping_tasks.filter(task => !task.state).map(task => ({ ...task, task_id: 0, latency_ms: task.family === 'ipv4' ? 161 : 155, loss: 0, at: f.now }))
    f.pings[node.id] = node.ping_tasks.filter(task => !task.state).flatMap(task => [60, 120, 180, 300, 500].map(age => ({ ...task, task_id: 0, ts: f.now - age, n: 10, lost: 0, avg_ms: task.family === 'ipv4' ? 161 : 155 })))
  }
  return f
}
const mime = { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.jpg': 'image/jpeg', '.webp': 'image/webp', '.woff2': 'font/woff2' }
const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url, 'http://localhost'), f = fixture()
    if (url.pathname.startsWith('/api/')) {
      const match = url.pathname.match(/nodes\/(\d+)\/(pings|history)$/)
      const data = url.pathname === '/api/probe' ? f.snapshot : match ? { from: f.now - 3600, to: f.now, step: 60, points: f[match[2]][match[1]] ?? [] } : {}
      response.setHeader('Content-Type', 'application/json')
      response.end(JSON.stringify(data))
      return
    }
    const file = path.resolve(dist, `.${url.pathname === '/' ? '/index.html' : url.pathname}`)
    if (!file.startsWith(`${dist}/`)) throw Error('Invalid path')
    response.setHeader('Content-Type', mime[path.extname(file)] ?? 'application/octet-stream')
    response.end(await readFile(file))
  } catch {
    response.statusCode = 404
    response.end('not found')
  }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const browser = await chromium.launch({ headless: true, args: ['--enable-unsafe-swiftshader'] })
const url = `http://127.0.0.1:${server.address().port}`
await mkdir(out, { recursive: true })
async function settle(page) {
  await page.waitForTimeout(400)
  for (let tick = 0; tick < 25; tick++) await page.clock.runFor(100)
  await page.waitForTimeout(400)
  await page.clock.runFor(100)
}
try {
  for (const width of [1280, 390]) for (mode of ['dual', 'ipv4', 'ipv6']) {
    const errors = []
    const context = await browser.newContext({ viewport: { width, height: 1100 }, reducedMotion: 'reduce' })
    await context.addInitScript(() => {
      localStorage.setItem('i18nextLng', 'zh-CN')
      localStorage.setItem('themeMode', 'light')
      if (!localStorage.getItem('nodeViewMode')) localStorage.setItem('nodeViewMode', 'card')
    })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    await page.clock.install({ time: base.now * 1000 })
    await page.clock.pauseAt(base.now * 1000)
    await page.goto(url)
    await page.waitForSelector('.node-card [data-carrier-family]')
    await settle(page)
    await page.waitForFunction(() => /1(?:61|55) ms/.test(document.querySelector('.node-card')?.textContent ?? ''))
    const cards = page.locator('.node-card'), first = cards.first()
    assert.equal(await cards.count(), 3)
    assert.equal(await first.locator('[data-carrier-family]').count(), mode === 'dual' ? 12 : 6)
    if (mode === 'dual') {
      assert((await first.textContent()).includes('v4 161 ms / v6 155 ms'))
      assert((await cards.nth(1).textContent()).includes('v6 —'))
    }
    assert(!(await cards.nth(2).textContent()).includes(' ms'))
    const overflow = await first.locator('[data-carrier-metric]').evaluateAll(items => items.filter(element => element.scrollWidth > element.clientWidth + 1).map(element => element.textContent))
    assert.deepEqual(overflow, [])
    await page.screenshot({ path: path.join(out, `${mode}-${width}.png`), fullPage: true, animations: 'disabled' })
    // Keep the stored choice on reload; overwriting it in addInitScript would
    // silently retest the card view instead of exercising the list layout.
    await page.evaluate(() => localStorage.setItem('nodeViewMode', 'list'))
    await page.reload()
    await page.waitForSelector('[data-carrier-family]')
    await settle(page)
    assert.equal(await page.locator('.node-card').count(), 0)
    const rows = page.getByRole('button', { name: /^查看节点/ })
    assert.equal(await rows.count(), 3)
    assert.deepEqual(await rows.evaluateAll(rows => rows.map(row => row.getBoundingClientRect().height)), [64, 64, 64])
    const clipped = await rows.evaluateAll(rows => rows.flatMap(row => {
      const bounds = row.getBoundingClientRect()
      return [...row.querySelectorAll('[data-carrier-family], [data-carrier-metric]')].filter(element => {
        const box = element.getBoundingClientRect()
        // Single-stack wrappers use display:contents and have no painted box.
        return box.width > 0 && box.height > 0 && (box.top < bounds.top - .5 || box.bottom > bounds.bottom + .5 || box.left < bounds.left - .5 || box.right > bounds.right + .5)
      }).map(element => ({ row: row.getAttribute('aria-label'), metric: element.getAttribute('data-carrier-metric') }))
    }))
    assert.deepEqual(clipped, [])
    assert.deepEqual(errors, [])
    // A narrow screen scrolls the existing list horizontally; reveal the
    // uptime/latency column before capturing the actual bars.
    await rows.first().locator('[data-carrier-metric]').first().scrollIntoViewIfNeeded()
    await page.screenshot({ path: path.join(out, `${mode}-${width}-list.png`), fullPage: true, animations: 'disabled' })
    console.log(`PASS ${mode} ${width}px card/list`)
    await context.close()
  }
} finally {
  await browser.close()
  await new Promise(resolve => server.close(resolve))
}
