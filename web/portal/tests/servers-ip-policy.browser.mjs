// Run against a locally built portal preview. All API responses are fixtures;
// external requests and writes are blocked. Supports PLAYWRIGHT_MODULE and CHROME_EXECUTABLE.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.PORTAL_IP_TEST_URL || 'http://127.0.0.1:5201/portal/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Local preview required')
const artifacts = process.env.PORTAL_IP_ARTIFACTS || '/tmp/captain-entry-ip-portal'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const modes = ['ipv4', 'ipv6', 'dual']
const names = [
  'Example long server name · IPv4',
  'Example-node-with-a-very-long-unbroken-identifier-0123456789 · IPv6',
  'Example long server name · Dual',
]
const servers = modes.map((mode, index) => ({
  name: names[index], host: 'node.example.com', port: 443, protocol: 'shadowsocks',
  tags: [], ip_version: mode, uri: mode === 'dual' ? 'ss://example' : '',
}))

try {
  for (const locale of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const dict = JSON.parse(await readFile(resolve(root, `src/i18n/${locale}.json`), 'utf8'))
    for (const width of [390, 1440]) {
      const errors = []
      const context = await browser.newContext({ viewport: { width, height: 1000 }, serviceWorkers: 'block' })
      await context.addInitScript(locale => {
        localStorage.setItem('i18nextLng', locale)
        window.copiedServerLinks = []
        Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async text => window.copiedServerLinks.push(text) } })
      }, locale)
      await context.routeWebSocket('**/*', socket => { errors.push('Unexpected WebSocket'); socket.close() })
      await context.route('**/*', async route => {
        const request = route.request(), url = new URL(request.url())
        if (url.origin !== base.origin) { errors.push(`External request: ${url.origin}`); return route.abort() }
        if (!url.pathname.startsWith('/api/')) return route.continue()
        const send = value => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value) })
        if (request.method() !== 'GET') { errors.push(`Unexpected write: ${request.method()} ${url.pathname}`); return route.abort() }
        switch (url.pathname) {
          case '/api/site': return send({ name: 'Example', homepage_enabled: false })
          case '/api/portal/me': return send({ id: 1, email: 'user@example.com', subscriptions: [] })
          case '/api/portal/servers': return send(servers)
          default: errors.push(`Unexpected API: ${url.pathname}`); return send({})
        }
      })
      try {
        const page = await context.newPage()
        page.on('pageerror', error => errors.push(error.message))
        await page.goto(new URL('servers', base).href)
        await page.getByText(names[2], { exact: true }).waitFor()
        const copy = page.getByRole('button', { name: dict.servers.copyLink, exact: true })
        assert.equal(await copy.count(), 1, `${locale}/${width}: forced family has no share link`)
        assert.equal(await page.getByText(dict.servers.ipVersionShareHint, { exact: true }).count(), 2)
        for (const mode of modes) assert.equal(await page.getByText(dict.servers.ipVersion[mode], { exact: true }).count(), 1)
        const clipped = await page.locator('.mantine-Badge-label').evaluateAll(labels => labels.filter(label => label.scrollWidth > label.clientWidth + 1).map(label => label.textContent))
        assert.deepEqual(clipped, [], `${locale}/${width}: protocol and IP labels must remain fully readable`)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `${locale}/${width}: no horizontal page overflow`)
        for (const name of names) {
          const bounds = await page.getByText(name, { exact: true }).evaluate(element => ({ visible: element.clientWidth, content: element.scrollWidth, right: element.getBoundingClientRect().right }))
          assert.ok(bounds.content <= bounds.visible + 1 && bounds.right <= width, `${locale}/${width}: long name wraps inside the card`)
        }
        await copy.click()
        assert.deepEqual(await page.evaluate(() => window.copiedServerLinks), ['ss://example'], 'Copy only the published compatible URI')
        await page.screenshot({ path: resolve(artifacts, `servers-${locale}-${width}.png`), fullPage: true })
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  }
  console.log('Portal server badges passed: six locales, 390/1440px, long names, full labels and share-link scope.')
} finally { await browser.close() }
