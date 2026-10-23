// Behavioural companion to glass-diff.mjs: drives the same interaction
// sequence (group tabs, quick filters, favourites, list view + sort, search,
// detail page navigation, theme toggle) in a reference build and a candidate
// build and compares screenshot pixels, normalised DOM, URL hash and
// localStorage after each step. Animations are frozen and the globe hidden,
// so this is not a pixel gate; it exists to catch state and behaviour
// divergences the static gate cannot see.
//
//   node scripts/glass-interact.mjs --ref <dist> --cand <dist> [--lang zh-CN|en] [--mobile]
//
// Known benign DOM differences: Vue's KeepAlive detaches the hidden home view
// while React keeps it mounted with display:none; the FLIP move class lingers
// in both builds only because transitions are frozen here.
import { createRequire } from 'node:module'
import path from 'node:path'
import { readFile, stat, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { once } from 'node:events'
const argv = process.argv.slice(2)
const opt = (name, dflt) => { const i = argv.indexOf(name); return i === -1 ? dflt : argv[i + 1] }
const probe = path.dirname(path.dirname(new URL(import.meta.url).pathname))
const require = createRequire(path.join(probe, 'package.json'))
const { chromium } = require('playwright')
const pixelmatch = require('pixelmatch').default ?? require('pixelmatch')
const { PNG } = require('pngjs')
const fixture = JSON.parse(await readFile(path.join(probe, 'scripts/glass-fixture.json'), 'utf8'))
const lang = opt('--lang', 'zh-CN')
const mobile = argv.includes('--mobile')
const refDist = opt('--ref', null), candDist = opt('--cand', null)
if (!refDist || !candDist) { console.error('usage: glass-interact.mjs --ref <dist> --cand <dist> [--lang zh-CN|en] [--mobile]'); process.exit(2) }
function rangeWindow(range) {
  switch (range) { case '24h': return { res: 'm', window: 86400, step: 60 }; case '7d': return { res: 'h', window: 7 * 86400, step: 3600 }; case '30d': return { res: 'h', window: 30 * 86400, step: 3600 }; default: return { res: 'raw', window: 3600, step: 10 } }
}
function api(url) {
  const { now, snapshot, history, pings } = fixture
  if (url.pathname === '/api/probe') return snapshot
  const m = /^\/api\/probe\/nodes\/(\d+)\/(history|pings)$/.exec(url.pathname); if (!m) return null
  const table = m[2] === 'history' ? history : pings; if (!(m[1] in table)) return null
  let { res, window, step } = rangeWindow(url.searchParams.get('range') ?? '')
  if (m[2] === 'pings' && res === 'raw') { res = 'm'; step = 60 }
  if (m[2] === 'history' && res === 'raw') { const node = snapshot.nodes.find(n => String(n.id) === m[1]); return { res, points: node?.recent ?? [], from: now - window, to: now, step: snapshot.beat_seconds } }
  return { res, points: table[m[1]].filter(p => p.ts >= now - window && p.ts <= now), from: now - window, to: now, step }
}
async function serve(dist) {
  const root = path.resolve(probe, dist)
  const s = createServer(async (req, res) => {
    const u = new URL(req.url, 'http://x')
    if (u.pathname.startsWith('/api/')) { const b = api(u); if (b === null) { res.writeHead(404); res.end(); return } res.writeHead(200, { 'content-type': 'application/json' }); res.end(JSON.stringify(b)); return }
    let f = path.join(root, decodeURIComponent(u.pathname)); const st = await stat(f).catch(() => null)
    if (!st || st.isDirectory()) f = path.join(root, 'index.html')
    res.writeHead(200, { 'content-type': { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.webp': 'image/webp', '.jpg': 'image/jpeg', '.png': 'image/png' }[path.extname(f)] ?? 'application/octet-stream' })
    res.end(await readFile(f))
  })
  s.listen(0, '127.0.0.1'); await once(s, 'listening'); return s
}
const freeze = `*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important}canvas,.background-container,.earth-globe-host,.earth-fallback,.earth-label{visibility:hidden!important}[data-general-card-key="remainingValue"]{visibility:hidden!important}`
const normDom = () => {
  const out = []
  const walk = (n, d) => {
    if (n.nodeType === 3) { const t = n.textContent; if (t.trim()) out.push(' '.repeat(d) + JSON.stringify(t.replace(/\s+/g, ' '))); else if (t.length) out.push(' '.repeat(d) + '" "'); return }
    if (n.nodeType !== 1) return
    if (['SCRIPT', 'STYLE', 'CANVAS'].includes(n.tagName) || n.tagName === 'svg' || n.matches('[data-sonner-toaster],[data-radix-popper-content-wrapper],[data-reka-popper-content-wrapper]')) return
    const attrs = [...n.attributes].filter(a => !a.name.startsWith('data-v-') && a.name !== '_echarts_instance_' && a.name !== 'style').map(a => `${a.name}=${JSON.stringify(a.value.replace(/reka-[a-z-]+-(?:v-\d+|_r_[0-9a-z]+_|«[^»]*»)[-\d]*/g, 'ID').split(/\s+/).sort().join(' '))}`).sort()
    out.push(' '.repeat(d) + '<' + n.tagName.toLowerCase() + (attrs.length ? ' ' + attrs.join(' ') : '') + '>')
    for (const c of n.childNodes) walk(c, d + 1)
  }
  walk(document.getElementById('app'), 0)
  return out.join('\n')
}
const steps = [
  ['group-core', async p => { await p.getByRole('tab', { name: 'Core' }).click() }],
  ['group-all', async p => { await p.getByRole('tab', { name: /全部节点|All nodes/ }).click() }],
  ['quick-offline', async p => { await p.getByRole('button', { name: /切换到.*离线|offline/i }).first().click() }],
  ['quick-offline-off', async p => { await p.getByRole('button', { name: /切换到.*离线|offline/i }).first().click() }],
  ['favorite', async p => {
    await p.getByRole('button', { name: /查看节点 Tokyo Edge 01 详情|View node Tokyo Edge 01/ }).click()
    await p.getByRole('button', { name: /收藏当前节点|Favorite current node/i }).click()
    await p.getByRole('button', { name: /返回首页|Back to Home|Back home|Home/i }).click()
    await p.waitForSelector('.node-card', { timeout: 15000 })
  }],
  ['quick-favorite', async p => { await p.getByRole('button', { name: /切换到.*收藏|favorite/i }).first().click() }],
  ['quick-favorite-off', async p => { await p.getByRole('button', { name: /切换到.*收藏|favorite/i }).first().click() }],
  ['list-view', async p => { await p.getByRole('button', { name: /列表视图|List view/ }).click() }],
  ['sort-name', async p => { await p.getByText(/^(节点|Node)( ↑| ↓)?$/).first().click() }],
  ['sort-name-desc', async p => { await p.getByText(/^(节点|Node)( ↑| ↓)?$/).first().click() }],
  ['search', async p => { await p.getByRole('textbox', { name: /搜索节点|Search nodes/ }).fill('frank'); await p.waitForTimeout(600) }],
  ['search-clear', async p => { await p.getByRole('button', { name: /清空搜索|Clear search/ }).click(); await p.waitForTimeout(600) }],
  ['card-view', async p => { await p.getByRole('button', { name: /卡片视图|Card view/ }).click() }],
  ['open-detail', async p => { await p.getByRole('button', { name: /查看节点 Tokyo Edge 01 详情|View node Tokyo Edge 01/ }).click(); await p.waitForSelector('[data-load-chart-card]', { timeout: 15000 }); await p.waitForTimeout(1500) }],
  ['detail-next', async p => { await p.getByRole('button', { name: /下一个节点|Next node/ }).click(); await p.waitForTimeout(1500) }],
  ['detail-select', async p => { await p.getByRole('combobox', { name: /切换节点|Switch node|Select node/i }).selectOption('3'); await p.waitForTimeout(1500) }],
  ['detail-range-1d', async p => { await p.locator('[data-load-chart-range]').getByRole('tab', { name: /1 天|1 day/ }).click(); await p.waitForTimeout(1500) }],
  ['back-home', async p => { await p.getByRole('button', { name: /返回首页|Back to Home|Back home|Home/i }).click(); await p.waitForSelector('.node-card', { timeout: 15000 }); await p.waitForTimeout(800) }],
  ['theme-toggle', async p => { await p.getByRole('button', { name: /点击切换|click to switch/i }).click(); await p.waitForTimeout(300) }],
  ['theme-toggle-2', async p => { await p.getByRole('button', { name: /点击切换|click to switch/i }).click(); await p.waitForTimeout(300) }],
]
const outDir = path.resolve(opt('--out', '/tmp/glass-interact'))
await (await import('node:fs/promises')).mkdir(outDir, { recursive: true })
const browser = await chromium.launch({ args: ['--hide-scrollbars', '--force-device-scale-factor=1', '--enable-unsafe-swiftshader'] })
const results = {}
for (const [name, dist] of [['vue', refDist], ['react', candDist]]) {
  const s = await serve(dist)
  const ctx = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }, isMobile: mobile, hasTouch: mobile, locale: lang, timezoneId: 'UTC', colorScheme: 'light', reducedMotion: 'reduce' })
  await ctx.addInitScript((l) => { localStorage.setItem('themeMode', 'light'); localStorage.setItem('nodeViewMode', 'card'); localStorage.setItem('i18nextLng', l) }, lang)
  const page = await ctx.newPage()
  const errors = []
  page.on('pageerror', e => errors.push(String(e)))
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text().slice(0, 150)) })
  await page.goto(`http://127.0.0.1:${s.address().port}/`)
  await page.waitForSelector('.node-card', { timeout: 20000 })
  await page.waitForTimeout(1500)
  await page.addStyleTag({ content: freeze })
  results[name] = {}
  for (const [step, fn] of steps) {
    try { await fn(page) } catch (e) { results[name][step] = { error: String(e).split('\n')[0] }; continue }
    await page.addStyleTag({ content: freeze })
    await page.waitForTimeout(250)
    const dom = await page.evaluate(normDom)
    const shot = path.join(outDir, `${name}-${step}-${lang}${mobile ? '-m' : ''}.png`)
    await page.screenshot({ path: shot, fullPage: true, animations: 'disabled', caret: 'hide' })
    await writeFile(shot.replace('.png', '.dom.txt'), dom)
    results[name][step] = { dom, shot, url: page.url().split('#')[1] ?? '', storage: await page.evaluate(() => JSON.stringify(Object.fromEntries(Object.keys(localStorage).sort().map(k => [k, localStorage.getItem(k)])))) }
  }
  results[name]._errors = errors
  await ctx.close(); s.close()
}
await browser.close()
let bad = 0
for (const [step] of steps) {
  const v = results.vue[step], r = results.react[step]
  if (v?.error || r?.error) { console.log(`${step}: ERROR vue=${v?.error ?? '-'} react=${r?.error ?? '-'}`); bad++; continue }
  const pa = PNG.sync.read(await readFile(v.shot)), pb = PNG.sync.read(await readFile(r.shot))
  let px = 'size-differs'
  if (pa.width === pb.width && pa.height === pb.height) { px = pixelmatch(pa.data, pb.data, null, pa.width, pa.height, { threshold: 0 }) }
  const domSame = v.dom === r.dom, urlSame = v.url === r.url, stSame = v.storage === r.storage
  if (px !== 0 || !domSame || !urlSame || !stSame) bad++
  console.log(`${step}: px=${px} dom=${domSame ? 'same' : 'DIFF'} url=${urlSame ? 'same' : `DIFF(${v.url}|${r.url})`} storage=${stSame ? 'same' : 'DIFF'}`)
  if (!domSame) { const a = v.dom.split('\n'), b = r.dom.split('\n'); let i = 0; while (i < a.length && a[i] === b[i]) i++; console.log('   first dom diff @' + i + '\n   vue  : ' + (a[i] ?? '').slice(0, 160) + '\n   react: ' + (b[i] ?? '').slice(0, 160)) }
  if (!stSame) console.log('   storage vue=' + v.storage + '\n   storage react=' + r.storage)
}
console.log('errors vue:', results.vue._errors.filter(e => !/WebGL|THREE/.test(e)), 'react:', results.react._errors.filter(e => !/WebGL|THREE/.test(e)))
process.exit(bad ? 1 : 0)
