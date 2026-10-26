#!/usr/bin/env node
// Built-asset regression for resolved probe brands, including independent hosts.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { mkdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)))
const out = process.argv[2] || '/tmp/probe-branding'
await mkdir(out, { recursive: true })
const fixture = JSON.parse(await readFile(path.join(root, 'scripts/glass-fixture.json'), 'utf8'))
let preset = 'inherit', scheme = 'light'
let brand
const reset = () => { brand = { title: 'Example Network', page_title: 'Network availability', description: 'Operations status.', logo: '/brand-assets/light.svg', logo_dark: '/brand-assets/dark.svg', favicon: '/brand-assets/icon.svg' } }
const snapshot = () => ({ ...fixture.snapshot, ...brand, beat_seconds: 1, show_globe: false, appearance: { preset, scheme }, appearance_scheme: scheme, public_sections: ['cpu', 'memory'], nodes: [fixture.snapshot.nodes[0]] })
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.webp': 'image/webp', '.jpg': 'image/jpeg', '.woff2': 'font/woff2' }
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://example.com')
    if (url.pathname.startsWith('/api/')) {
      // Decoy main-site branding must never override a resolved independent probe.
      const body = url.pathname === '/api/probe' ? snapshot() : url.pathname === '/api/site' ? { name: 'Different main brand', theme: { site_scheme: scheme }, branding: { logo_light: '/brand-assets/decoy.svg' } } : { points: [] }
      res.writeHead(200, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); return
    }
    if (url.pathname.startsWith('/brand-assets/')) {
      if (url.pathname.includes('broken')) { res.writeHead(404); res.end(); return }
      const fill = url.pathname.includes('dark') ? '#dddddd' : '#333333'
      res.writeHead(200, { 'content-type': 'image/svg+xml' }); res.end(`<svg xmlns="http://www.w3.org/2000/svg" width="80" height="40"><rect width="80" height="40" rx="6" fill="${fill}"/></svg>`); return
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
const waitTitle = (page, value) => page.waitForFunction(title => document.title === title, value)
const waitLogo = (page, filename) => page.waitForFunction(name => [...document.images].some(img => img.getAttribute('src') === `/brand-assets/${name}.svg` && img.naturalWidth > 0), filename)
const metadata = page => page.evaluate(() => ({ description: document.head.querySelector('meta[name="description"]')?.content || '', icons: [...document.head.querySelectorAll('link[rel~="icon"]')].map(link => link.href.startsWith('data:') ? link.href : new URL(link.href).pathname) }))

try {
  for (preset of ['inherit', 'aurora', 'paper', 'terminal', 'glass']) {
    scheme = 'light'; reset()
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light', locale: 'en-US', reducedMotion: 'reduce' })
    const page = await context.newPage(), errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(`${origin}/${preset === 'glass' ? 'glass/' : ''}`)
    await waitTitle(page, brand.page_title); await waitLogo(page, 'light')
    assert.deepEqual(await metadata(page), { description: brand.description, icons: ['/brand-assets/icon.svg'] })
    if (preset === 'glass') {
      // Glass's local choice overrides the managed theme: logos follow the actual UI.
      const toggle = () => page.getByRole('button', { name: /主题.*点击切换|theme.*点击切换/i })
      await toggle().click() // auto -> light
      await toggle().click() // light -> dark
      await waitLogo(page, 'dark')
      assert(await page.locator('html').evaluate(element => element.classList.contains('dark')))
      await page.screenshot({ path: path.join(out, 'glass-dark.png') })
      await toggle().click() // dark -> auto (managed light)
      await waitLogo(page, 'light')
    } else {
      scheme = 'dark'
      await waitLogo(page, 'dark')
      assert.equal(await page.locator('html').getAttribute('data-mantine-color-scheme'), 'dark')
    }
    // An operator switches off inheritance. New metadata replaces the main brand.
    brand = { title: 'Independent Probe', page_title: '', description: '', logo: '/brand-assets/own.svg', logo_dark: '', favicon: '' }
    await waitTitle(page, 'Independent Probe'); await waitLogo(page, 'own')
    const independent = await metadata(page)
    assert.equal(independent.description, '')
    assert.equal(independent.icons.length, 1)
    assert.match(independent.icons[0], /^(?:data:image\/svg\+xml|.*captain-mark)/)
    assert.equal(await page.locator('img[src="/brand-assets/decoy.svg"]').count(), 0)
    await page.getByText('Independent Probe', { exact: true }).waitFor()
    if (preset === 'glass' || preset === 'inherit') await page.screenshot({ path: path.join(out, `${preset}-independent.png`) })
    // No brand image must retain the existing text/avatar fallback, without a broken icon.
    brand = { ...brand, title: 'Broken image fallback', logo: '/brand-assets/broken.svg' }
    await waitTitle(page, brand.title)
    await page.waitForFunction(() => ![...document.images].some(img => img.getAttribute('src') === '/brand-assets/broken.svg' && img.getClientRects().length > 0))
    if (preset === 'glass') assert.equal(await page.locator('[data-slot="avatar-fallback"]').innerText(), 'B')
    await page.getByText('Broken image fallback', { exact: true }).waitFor()
    assert.deepEqual(errors, [])
    await context.close()
  }
  // The standard preset also responds to the operating system's auto mode.
  preset = 'inherit'; scheme = 'auto'; reset()
  const context = await browser.newContext({ colorScheme: 'light', viewport: { width: 390, height: 844 } })
  const page = await context.newPage()
  await page.goto(origin); await waitLogo(page, 'light')
  await page.emulateMedia({ colorScheme: 'dark' }); await waitLogo(page, 'dark')
  await page.screenshot({ path: path.join(out, 'mobile-auto-dark.png') })
  await context.close()
  console.log('Probe branding: all five presets; real light/dark/auto themes; Glass local choice; independent metadata replacement; one-logo and broken-image fallback passed')
} finally {
  await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve))
}
