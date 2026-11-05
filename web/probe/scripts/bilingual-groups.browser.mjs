// Run after building portal, site and probe. Only local assets and mocked APIs are used.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { readFile, stat } from 'node:fs/promises'
import { dirname, extname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const probeDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const fixture = JSON.parse(await readFile(join(probeDir, 'scripts/glass-fixture.json'), 'utf8'))
const label = 'Shared, name | ;'
const categories = [
  { id: 1, name: 'stable-west', name_zh: '西区', name_en: label, description: 'Original description', sort: 1 },
  { id: 2, name: 'stable-east', name_zh: '东区', name_en: label, description: '', sort: 2 },
  { id: 3, name: 'legacy', name_zh: '仅中文', name_en: '', description: '', sort: 3 },
]
const servers = categories.map((category, i) => ({ name: `Node ${i + 1}`, host: '192.0.2.10', port: 443 + i, protocol: 'vless', uri: '', category }))
const site = { name: 'Example', homepage_enabled: false, service_status_url: '/service-status', features: [], faq: [], links: {}, locations: [], hub: null, stats: { nodes: 3, online: 3, users: 0, locations: 0 } }
const probe = { ...fixture.snapshot, show_globe: false, beat_seconds: 5, public_sections: ['system', 'info'], nodes: categories.map((category, i) => ({ ...structuredClone(fixture.snapshot.nodes[0]), id: i + 1, name: `Node ${i + 1}`, group: `stable-${i};key,part|pool`, group_zh: category.name_zh, group_en: category.name_en })) }
const status = () => {
  const now = Math.floor(Date.now() / 1000)
  return { generated_at: now, refresh_seconds: 60, entries: servers.map((server, i) => ({ id: i + 1, name: server.name, category: server.category, rate: 1, region: '', state: 'healthy', reason: 'healthy', evidence: ['inbound'], observed_at: now, maintenance: null, end_to_end: false })) }
}
const errors = []
const services = []
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
async function serve(root, prefix = '/') {
  const server = createServer(async (request, response) => {
    const path = new URL(request.url, 'http://localhost').pathname
    let file = join(root, path.startsWith(prefix) ? path.slice(prefix.length) : path)
    if (!(await stat(file).catch(() => null))?.isFile()) file = join(root, 'index.html')
    response.setHeader('content-type', { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.webp': 'image/webp', '.png': 'image/png', '.jpg': 'image/jpeg' }[extname(file)] || 'application/octet-stream')
    response.end(await readFile(file))
  })
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  services.push(server)
  return `http://127.0.0.1:${server.address().port}${prefix}`
}
async function pageFor(base) {
  const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } })
  context.on('page', page => page.on('pageerror', error => errors.push(error.message)))
  await context.addInitScript(() => {
    if (location.protocol === 'http:' && !localStorage.getItem('i18nextLng')) localStorage.setItem('i18nextLng', 'en')
  })
  await context.route('**/*', route => {
    const url = new URL(route.request().url())
    if (url.origin !== new URL(base).origin) return route.abort()
    if (!url.pathname.startsWith('/api/')) return route.continue()
    const send = body => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
    if (url.pathname === '/api/site') return send(site)
    if (url.pathname === '/api/portal/me') return send({ id: 1, email: 'user@example.com', balance_cents: 0, subscriptions: [], subscription_url: '', online_devices: 0 })
    if (url.pathname === '/api/portal/notice') return send({ enabled: false })
    if (url.pathname === '/api/portal/me/lang') return send({})
    if (url.pathname === '/api/portal/servers') return send(servers)
    if (url.pathname.endsWith('/service-status')) return send(status())
    if (url.pathname === '/api/probe') return send(probe)
    return send([])
  })
  return context.newPage()
}
async function assertNode(page, shown, hidden) {
  await page.getByText(new RegExp(`^${shown}(?:×1|1×)?$`)).first().waitFor({ state: 'visible' })
  assert.equal(await page.getByText(new RegExp(`^${hidden}(?:×1|1×)?$`)).filter({ visible: true }).count(), 0)
}
async function portalLanguage(page, language) {
  await page.getByRole('button', { name: /language|语言|語言|言語|언어|Язык/i }).click()
  await page.getByRole('menuitem', { name: language, exact: true }).click()
}
try {
  // Portal category filters hold the same ID when the selected label changes.
  const portalBase = await serve(resolve(probeDir, '../portal/dist'), '/portal/')
  const portal = await pageFor(portalBase)
  await portal.goto(portalBase + 'servers')
  await portal.getByRole('textbox', { name: 'Category', exact: true }).click()
  assert.equal(await portal.getByRole('option', { name: label, exact: true }).count(), 2)
  await portal.getByRole('option', { name: label, exact: true }).nth(1).click()
  await assertNode(portal, 'Node 2', 'Node 1')
  await portalLanguage(portal, '中文')
  await portal.getByText('东区', { exact: true }).first().waitFor()
  await assertNode(portal, 'Node 2', 'Node 1')
  await portalLanguage(portal, '日本語')
  await portal.getByText(label, { exact: true }).first().waitFor()
  await assertNode(portal, 'Node 2', 'Node 1')
  await portalLanguage(portal, 'English')
  await portal.goto(portalBase + 'status')
  await portal.getByRole('button', { name: /Shared, name/ }).nth(1).click()
  await assertNode(portal, 'Node 2', 'Node 1')
  await portalLanguage(portal, '繁體中文')
  await portal.getByRole('button', { name: /东区/ }).waitFor()
  await assertNode(portal, 'Node 2', 'Node 1')
  await portal.close()

  // Public service status shares fallback and stable category ID behaviour.
  const siteBase = await serve(resolve(probeDir, '../site/dist'))
  const publicPage = await pageFor(siteBase)
  await publicPage.goto(siteBase + 'service-status')
  await publicPage.getByRole('button', { name: /Shared, name/ }).nth(1).click()
  await assertNode(publicPage, 'Node 2', 'Node 1')
  await publicPage.locator('.site-language-selector select').selectOption('zh-TW')
  await publicPage.getByRole('button', { name: /东区/ }).waitFor()
  await assertNode(publicPage, 'Node 2', 'Node 1')
  await publicPage.locator('.site-language-selector select').selectOption('ru')
  await publicPage.getByRole('button', { name: /仅中文/ }).click()
  await assertNode(publicPage, 'Node 3', 'Node 1')
  await publicPage.close()

  // Classic monitoring retains the opaque group key across live language switches.
  const classicBase = await serve(join(probeDir, 'dist'))
  const classic = await pageFor(classicBase)
  await classic.goto(classicBase)
  await classic.getByRole('textbox', { name: 'Group', exact: true }).click()
  assert.equal(await classic.getByRole('option', { name: label, exact: true }).count(), 2)
  await classic.getByRole('option', { name: label, exact: true }).nth(1).click()
  await assertNode(classic, 'Node 2', 'Node 1')
  await classic.getByRole('textbox', { name: 'Search nodes', exact: true }).fill('东区')
  await assertNode(classic, 'Node 2', 'Node 1')
  await classic.getByRole('textbox', { name: 'Search nodes', exact: true }).fill('stable-1;key,part|pool')
  await assertNode(classic, 'Node 2', 'Node 1')
  await classic.getByRole('textbox', { name: 'Search nodes', exact: true }).fill('')
  await classic.getByRole('textbox', { name: 'Language', exact: true }).click()
  await classic.getByRole('option', { name: '简体中文', exact: true }).click()
  await classic.getByText(/东区/).first().waitFor()
  await assertNode(classic, 'Node 2', 'Node 1')
  await classic.getByRole('textbox', { name: '语言', exact: true }).click()
  await classic.getByRole('option', { name: '日本語', exact: true }).click()
  await assertNode(classic, 'Node 2', 'Node 1')
  await classic.close()

  // Glass must treat punctuation as part of one identity and keep duplicate labels separate.
  probe.layout = 'compact'
  const glassBase = await serve(join(probeDir, 'dist/glass'))
  const glass = await pageFor(glassBase)
  await glass.goto(glassBase)
  await glass.getByRole('tab', { name: label, exact: true }).first().waitFor()
  assert.equal(await glass.getByRole('tab', { name: label, exact: true }).count(), 2)
  await glass.getByRole('tab', { name: label, exact: true }).nth(1).click()
  await assertNode(glass, 'Node 2', 'Node 1')
  await glass.locator('select').selectOption('zh-CN')
  await glass.getByRole('tab', { name: '东区', exact: true }).waitFor()
  await assertNode(glass, 'Node 2', 'Node 1')
  await glass.locator('select').selectOption('ja')
  await glass.getByRole('tab', { name: label, exact: true }).first().waitFor()
  await assertNode(glass, 'Node 2', 'Node 1')
  probe.nodes[1].group_en = 'Updated name'
  await glass.getByRole('tab', { name: 'Updated name', exact: true }).waitFor({ timeout: 12_000 })
  await assertNode(glass, 'Node 2', 'Node 1')
  await glass.setViewportSize({ width: 390, height: 844 })
  assert.ok(await glass.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile overflow')
  await glass.close()
  assert.deepEqual(errors, [])
  console.log('PASS: portal, public status, classic and glass bilingual filters; locale changes, fallback, duplicate names, punctuation and live rename preserve identity')
} finally {
  await browser.close()
  for (const server of services) server.close()
}
