// Local production-bundle UI regression. API fixtures only; no external requests.
// Start pnpm preview --host 127.0.0.1 --port 5674 first. Optional PLAYWRIGHT_MODULE,
// CHROME_EXECUTABLE, DUAL_STACK_TEST_URL and DUAL_STACK_ARTIFACTS overrides.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.DUAL_STACK_TEST_URL || 'http://127.0.0.1:5674/admin/')
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname))
const artifacts = process.env.DUAL_STACK_ARTIFACTS || '/tmp/captain-dual-stack-ui'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const formats = ['clash', 'stash', 'surge', 'surfboard', 'loon', 'qx', 'egern']
try {
  for (const scenario of [{ language: 'en', scheme: 'light', width: 1440 }, { language: 'zh-CN', scheme: 'dark', width: 390 }]) {
    const dict = JSON.parse(await readFile(resolve(root, 'src/i18n', `${scenario.language}.json`), 'utf8'))
    const t = key => key.split('.').reduce((value, part) => value[part], dict)
    const context = await browser.newContext({ viewport: { width: scenario.width, height: 1000 }, colorScheme: scenario.scheme, serviceWorkers: 'block' })
    await context.addInitScript(({ language, scheme }) => { localStorage.setItem('i18nextLng', language); localStorage.setItem('mantine-color-scheme-value', scheme) }, scenario)
    await context.routeWebSocket('**/*', socket => socket.close())
    let saved = { groups: [{ name: 'Proxy', type: 'select', members: ['@all'] }], rules: [], final: 'Proxy', enhancements: { regions: false } }
    const writes = [], unexpected = [], errors = []
    const support = design => formats.map(format => ({ format, supported: !design.enhancements?.node_dual_stack || format === 'clash', reasons: format === 'clash' || !design.enhancements?.node_dual_stack ? [] : ['Mihomo only'], reason_codes: format === 'clash' || !design.enhancements?.node_dual_stack ? [] : ['node_dual_stack_mihomo'] }))
    await context.route('**/*', async route => {
      const req = route.request(), url = new URL(req.url()), path = url.pathname
      if (url.origin !== base.origin) { unexpected.push(url.origin); return route.abort() }
      if (!path.startsWith('/api/')) return route.continue()
      const send = (value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
      if (path === '/api/admin/me') return send({ id: 1, email: 'admin@example.com', role: 'admin' })
      if (path === '/api/admin/settings/sub-design' && req.method() === 'GET') return send({ design: saved, catalogue: [], presets: [], tags: [], regions: [], formats })
      if (path === '/api/admin/settings/sub-design/claude-rules') return send({ builtin: { source: 'builtin', revision: 'fixture', rules: [] }, applied: { status: 'disabled' } })
      if (path === '/api/admin/settings/sub-design/inspect') { const { design } = req.postDataJSON(); return send({ effective_design: design, formats: support(design) }) }
      if (path === '/api/admin/settings/sub-design/preview') return send({ format: 'clash', text: 'ipv6: true\ntcp-concurrent: true\n' })
      if ((path === '/api/admin/settings/sub-design' && req.method() === 'PUT') || path === '/api/admin/settings/sub-design/apply') {
        const body = req.postDataJSON(); writes.push({ path, body }); saved = structuredClone(body.design)
        return send(path.endsWith('/apply') ? { templates: {}, defaults: {} } : saved)
      }
      if (req.method() === 'GET' && !path.includes('sub-design')) return send({})
      unexpected.push(`${req.method()} ${path}`); return send({ error: 'Unexpected fixture request' }, 500)
    })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(new URL('sub-templates', base).href)
    const option = page.getByRole('checkbox', { name: t('subDesign.nodeDualStackOption'), exact: true })
    await option.check()
    await page.getByText(t('subDesign.supportNodeDualStack'), { exact: true }).waitFor()
    assert.equal(await page.getByRole('checkbox', { name: 'Stash', exact: true }).isDisabled(), true)
    assert.equal(await page.getByRole('checkbox', { name: 'mihomo / Clash Meta', exact: true }).isChecked(), true)
    assert.equal(writes.length, 0, 'toggling an option must not publish')
    await page.getByRole('button', { name: t('subDesign.save'), exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('[data-loading="true"]'))
    await page.reload()
    await option.waitFor()
    assert.equal(await option.isChecked(), true, 'draft option survives reloading')
    assert.equal(writes.length, 1)
    assert.equal(writes[0].body.design.enhancements.node_dual_stack, true)
    const apply = page.getByRole('button', { name: t('subDesign.apply'), exact: true })
    await apply.click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: t('subDesign.apply'), exact: true }).click()
    await dialog.waitFor({ state: 'hidden' })
    assert.deepEqual(writes.at(-1).body.formats, ['clash'])
    assert.equal(writes.at(-1).body.design.enhancements.node_dual_stack, true)
    await option.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve(artifacts, `${scenario.language}-${scenario.scheme}.png`) })
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'mobile viewport must not overflow')
    await option.uncheck()
    await page.getByRole('button', { name: t('subDesign.save'), exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('[data-loading="true"]'))
    assert.equal(writes.filter(write => write.path.endsWith('/apply')).length, 1, 'saving disabled draft must not change published template')
    assert.deepEqual(unexpected, [])
    assert.deepEqual(errors, [])
    await context.close()
  }
  console.log('Dual-stack designer browser checks passed (desktop light, Chinese mobile dark).')
  console.log(artifacts)
} finally { await browser.close() }
