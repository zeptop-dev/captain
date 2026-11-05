// Local browser regression with synthetic API fixtures; no production services.
// Start Vite preview and supply ADMIN_TEST_URL / PLAYWRIGHT_MODULE as needed.
import assert from 'node:assert/strict'
import { mkdtemp, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
const base = process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5674/admin/'
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname))
const screenshots = await mkdtemp(join(tmpdir(), 'captain-bilingual-admin-'))
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const locales = ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']
const messages = Object.fromEntries(await Promise.all(locales.map(async locale => [locale, JSON.parse(await readFile(new URL(`../src/i18n/${locale}.json`, import.meta.url)))])))
const tr = (locale, key) => key.split('.').reduce((v, k) => v[k], messages[locale])
const node = { id: 1, name: 'Example server', hostname: 'server.example.com', public_addr: '192.0.2.10', internal_addr: '', domain: '', v6_addr: '', monitor_url: '', paired: true, online: true, proxy_allowed: true, mode: 'full', monitor_group: 'legacy-key', version: 'v0.89.0', traffic_today_bytes: 0, inbounds: 1, outdated: false, cert_problem: false }
const inbound = { ID: 1, NodeID: 1, Tag: 'fixture', Protocol: 'vless', Port: 443, Settings: {}, GroupID: null, Enabled: true, Sort: 1 }
const entry = { ID: 1, Name: 'Example node', InboundID: 1, DisplayHost: 'node.example.com', DisplayPort: 443, Rate: 1, Sort: 1, Enabled: true, Tags: [], Region: '', CategoryID: 1, ClientExtra: null }
async function setup(locale = 'en', width = 1440) {
  const context = await browser.newContext({ viewport: { width, height: 1100 }, serviceWorkers: 'block' })
  await context.addInitScript(language => localStorage.setItem('i18nextLng', language), locale)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { layout: { revision: 'layout-1', groups: ['legacy-key'], nodes: [{ id: 1, group: 'legacy-key' }] }, categories: [{ ID: 1, Name: 'legacy-category', Description: 'Keep this description', Sort: 1 }], writes: [], errors: [], unexpected: [] }
  await context.route('**/*', async route => {
    const r = route.request(), u = new URL(r.url()), path = u.pathname
    if (u.origin !== new URL(base).origin) return route.abort()
    if (!path.startsWith('/api/')) return route.continue()
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
    if (r.method() !== 'GET') {
      const body = r.postDataJSON(); state.writes.push({ method: r.method(), path, body })
      if (path === '/api/admin/nodes/layout' && r.method() === 'PUT') { state.layout = { ...body, revision: 'layout-2' }; return send(state.layout) }
      if (path === '/api/admin/nodes/1/monitor-group' && r.method() === 'PUT') { state.layout.nodes[0].group = body.group; return send({}) }
      if (path === '/api/admin/entry-categories' && r.method() === 'POST') { const category = { ID: state.categories.length + 1, Sort: state.categories.length + 1, ...body }; state.categories.push(category); return send(category) }
      if (path.startsWith('/api/admin/entry-categories/') && r.method() === 'PATCH') { const category = state.categories.find(c => c.ID === Number(path.split('/').at(-1))); Object.assign(category, body); return send(category) }
      state.unexpected.push(`${r.method()} ${path}`); return send({}, 500)
    }
    const names = state.layout.group_names?.[state.layout.nodes[0].group]
    const currentNode = { ...node, monitor_group: state.layout.nodes[0].group, monitor_group_zh: names?.zh, monitor_group_en: names?.en }
    switch (path) {
      case '/api/admin/me': return send({ id: 1, email: 'admin@example.com', role: 'admin', version: 'v1.55.0' })
      case '/api/site': return send({ name: 'Captain', branding: {}, theme: {} })
      case '/api/admin/dashboard': return send({})
      case '/api/admin/system/update': return send({ captain: { current: 'v1.55.0' } })
      case '/api/admin/monitoring': return send([{ id: 1, name: node.name, group: currentNode.monitor_group, group_zh: names?.zh, group_en: names?.en, paired: true, online: true, stale: false, info: {}, host: null, sampled_at: 0 }])
      case '/api/admin/nodes': return send([currentNode])
      case '/api/admin/nodes/1': return send({ node: currentNode, inbounds: [inbound], ingresses: [] })
      case '/api/admin/nodes/layout': return send(state.layout)
      case '/api/admin/entry-categories': return send(state.categories)
      case '/api/admin/entries': return send([entry])
      case '/api/admin/node-access': return send({ revision: 'r1', groups: [], plans: [], inbounds: [] })
      case '/api/admin/entries/tags': case '/api/admin/lines': case '/api/admin/groups': return send([])
      default: state.unexpected.push(`${r.method()} ${path}`); return send({}, 500)
    }
  })
  const page = await context.newPage(); page.setDefaultTimeout(10000)
  page.on('pageerror', error => state.errors.push(error.message))
  return { context, page, state }
}
async function finish(run) { assert.deepEqual(run.state.errors, []); assert.deepEqual(run.state.unexpected, []); await run.context.close() }
async function select(page, input, label) { await input.click(); await page.getByRole('option', { name: label, exact: true }).click() }
async function checkForm(page, locale, screenshot) {
  const form = page.getByRole('dialog').last()
  await form.getByLabel(tr(locale, 'bilingualName.zh'), { exact: true }).waitFor()
  await form.getByLabel(tr(locale, 'bilingualName.en'), { exact: true }).waitFor()
  const dimensions = await form.evaluate(el => ({ width: el.clientWidth, scroll: el.scrollWidth, inputs: [...el.querySelectorAll('input')].map(input => input.getBoundingClientRect().width) }))
  assert.ok(dimensions.scroll <= dimensions.width + 2, `${locale} dialog overflow`)
  assert.ok(dimensions.inputs.every(width => width > 180), `${locale} name input too narrow`)
  if (screenshot) await form.screenshot({ path: join(screenshots, screenshot) })
  return form
}
try {
  const run = await setup(), { page, state } = run
  await page.goto(`${base}nodes`)
  await page.getByRole('button', { name: tr('en', 'nodeLayout.title'), exact: true }).click()
  await page.getByRole('button', { name: tr('en', 'nodeLayout.rename'), exact: true }).click()
  let form = await checkForm(page, 'en')
  assert.equal(await form.getByLabel(tr('en', 'bilingualName.zh')).inputValue(), 'legacy-key')
  assert.equal(await form.getByLabel(tr('en', 'bilingualName.en')).inputValue(), '')
  await form.getByLabel(tr('en', 'bilingualName.zh')).fill('亚洲分组')
  await form.getByLabel(tr('en', 'bilingualName.en')).fill('Asia group')
  await form.getByRole('button', { name: tr('en', 'common.save'), exact: true }).click()
  for (const [zh, en] of [['', 'Europe group'], ['中文分组', '']]) {
    await page.getByRole('dialog').getByRole('button', { name: tr('en', 'nodeLayout.addGroup'), exact: true }).click()
    form = await checkForm(page, 'en', en ? 'group-desktop.png' : undefined)
    assert.equal(await form.getByRole('button', { name: tr('en', 'common.save'), exact: true }).isDisabled(), true)
    await form.getByLabel(tr('en', 'bilingualName.zh')).fill(zh)
    await form.getByLabel(tr('en', 'bilingualName.en')).fill(en)
    await form.getByRole('button', { name: tr('en', 'common.save'), exact: true }).click()
  }
  let dialog = page.getByRole('dialog')
  await select(page, dialog.getByRole('textbox', { name: tr('en', 'nodeLayout.groupNodes'), exact: true }), 'Asia group')
  await dialog.getByLabel('Example server', { exact: true }).check()
  await select(page, dialog.getByRole('textbox', { name: tr('en', 'nodeLayout.moveTo'), exact: true }), 'Europe group')
  await dialog.getByRole('button', { name: tr('en', 'nodeLayout.move'), exact: true }).click()
  await dialog.getByRole('button', { name: tr('en', 'common.save'), exact: true }).click()
  await dialog.waitFor({ state: 'hidden' })
  assert.deepEqual(state.layout.groups, ['legacy-key', 'Europe group', '中文分组'])
  assert.deepEqual(state.layout.group_names, { 'legacy-key': { zh: '亚洲分组', en: 'Asia group' }, 'Europe group': { zh: '', en: 'Europe group' }, '中文分组': { zh: '中文分组', en: '' } })
  assert.equal(state.layout.nodes[0].group, 'Europe group')
  await page.getByRole('button', { name: tr('en', 'nodeLayout.title'), exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: tr('en', 'nodeLayout.deleteGroup'), exact: true }).last().click()
  await page.getByRole('dialog').getByRole('button', { name: tr('en', 'common.save'), exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  assert.deepEqual(state.layout.groups, ['legacy-key', 'Europe group'])
  assert.equal('中文分组' in state.layout.group_names, false, 'deletion also removes translated labels')
  assert.equal(state.layout.nodes[0].group, 'Europe group')
  await select(page, page.getByRole('textbox', { name: tr('en', 'monitoring.group'), exact: true }), 'Europe group')
  assert.equal(await page.getByRole('link', { name: 'Example server', exact: true }).count(), 1)
  await page.getByRole('button', { name: 'language', exact: true }).click()
  await page.getByRole('menuitem', { name: '简体中文', exact: true }).click()
  assert.equal(await page.getByRole('textbox', { name: tr('zh-CN', 'monitoring.group'), exact: true }).inputValue(), 'Europe group')
  await select(page, page.getByRole('textbox', { name: tr('zh-CN', 'monitoring.group'), exact: true }), '亚洲分组')
  assert.equal(await page.getByRole('link', { name: 'Example server', exact: true }).count(), 0)
  await finish(run)

  const categories = await setup(), p = categories.page
  await p.goto(`${base}entries`)
  await p.getByRole('button', { name: tr('en', 'entryCategories.manage'), exact: true }).click()
  await p.getByRole('button', { name: 'Edit legacy-category', exact: true }).click()
  form = await checkForm(p, 'en')
  await form.getByLabel(tr('en', 'bilingualName.zh')).fill('亚洲节点')
  await form.getByLabel(tr('en', 'bilingualName.en')).fill('Asia nodes')
  await form.getByRole('button', { name: tr('en', 'common.save'), exact: true }).click()
  await form.getByRole('button', { name: 'Edit Asia nodes', exact: true }).waitFor()
  assert.deepEqual(categories.state.writes.at(-1).body, { Name: '亚洲节点', NameZH: '亚洲节点', NameEN: 'Asia nodes', Description: 'Keep this description' })
  for (const [zh, en] of [['', 'Europe nodes'], ['中文分类', '']]) {
    await form.getByRole('button', { name: tr('en', 'entryCategories.create'), exact: true }).click()
    assert.equal(await form.getByRole('button', { name: tr('en', 'common.save'), exact: true }).isDisabled(), true)
    await form.getByLabel(tr('en', 'bilingualName.zh')).fill(zh)
    await form.getByLabel(tr('en', 'bilingualName.en')).fill(en)
    await form.getByRole('button', { name: tr('en', 'common.save'), exact: true }).click()
    await form.getByRole('button', { name: `Edit ${en || zh}`, exact: true }).waitFor()
  }
  await form.getByRole('button', { name: tr('en', 'entryCategories.close'), exact: true }).click()
  await p.getByRole('row').filter({ hasText: 'Example node' }).getByText('Asia nodes', { exact: true }).waitFor()
  await p.getByRole('button', { name: 'language', exact: true }).click()
  await p.getByRole('menuitem', { name: '简体中文', exact: true }).click()
  await p.getByRole('row').filter({ hasText: 'Example node' }).getByText('亚洲节点', { exact: true }).waitFor()
  await select(p, p.getByRole('textbox', { name: tr('zh-CN', 'entryCategories.label'), exact: true }), '亚洲节点')
  assert.equal(await p.getByRole('row').filter({ hasText: 'Example node' }).count(), 1)
  await finish(categories)

  const monitoring = await setup(), mp = monitoring.page
  monitoring.state.layout.groups.push('Europe group')
  monitoring.state.layout.group_names = { 'legacy-key': { zh: '亚洲分组', en: 'Asia group' }, 'Europe group': { zh: '欧洲分组', en: 'Europe group' } }
  await mp.goto(`${base}monitoring`)
  await mp.getByRole('row').filter({ hasText: 'Example server' }).getByRole('button', { name: 'Asia group', exact: true }).waitFor()
  await mp.getByRole('textbox', { name: tr('en', 'monitoring.search'), exact: true }).fill('亚洲分组')
  assert.equal(await mp.getByRole('row').filter({ hasText: 'Example server' }).count(), 1)
  await select(mp, mp.getByRole('textbox', { name: tr('en', 'monitoring.group'), exact: true }), 'Asia group')
  await mp.getByRole('button', { name: 'language', exact: true }).click()
  await mp.getByRole('menuitem', { name: '简体中文', exact: true }).click()
  assert.equal(await mp.getByRole('textbox', { name: tr('zh-CN', 'monitoring.group'), exact: true }).inputValue(), '亚洲分组')
  await mp.getByRole('row').filter({ hasText: 'Example server' }).getByRole('button', { name: '亚洲分组', exact: true }).click()
  await select(mp, mp.getByRole('textbox', { name: `${tr('zh-CN', 'monitoring.group')} · Example server`, exact: true }), '欧洲分组')
  await mp.getByRole('button', { name: tr('zh-CN', 'common.save'), exact: true }).click()
  await mp.getByRole('textbox', { name: `${tr('zh-CN', 'monitoring.group')} · Example server`, exact: true }).waitFor({ state: 'hidden' })
  assert.deepEqual(monitoring.state.writes.at(-1).body, { group: 'Europe group' })
  await finish(monitoring)

  for (const locale of locales) {
    const run = await setup(locale, 390), { page } = run
    run.state.layout.group_names = { 'legacy-key': { zh: '亚洲分组', en: 'Asia group' } }
    Object.assign(run.state.categories[0], { NameZH: '亚洲节点', NameEN: 'Asia nodes' })
    await page.goto(`${base}nodes`)
    await page.getByRole('row').filter({ hasText: 'Example server' }).getByText(locale.startsWith('zh') ? '亚洲分组' : 'Asia group', { exact: true }).waitFor()
    await page.getByRole('button', { name: tr(locale, 'nodeLayout.title'), exact: true }).click()
    await page.getByRole('button', { name: tr(locale, 'nodeLayout.addGroup'), exact: true }).click()
    await checkForm(page, locale, locale === 'zh-CN' ? 'group-mobile.png' : undefined)
    await page.goto(`${base}entries`)
    await page.getByRole('row').filter({ hasText: 'Example node' }).getByText(locale.startsWith('zh') ? '亚洲节点' : 'Asia nodes', { exact: true }).waitFor()
    await page.getByRole('button', { name: tr(locale, 'entryCategories.manage'), exact: true }).click()
    await page.getByRole('button', { name: tr(locale, 'entryCategories.create'), exact: true }).click()
    await checkForm(page, locale, locale === 'zh-CN' ? 'category-mobile.png' : undefined)
    await finish(run)
  }
  console.log(`Bilingual admin browser regression passed: legacy editing, both single-language creations, language switching, stable group identities and filtering, six mobile locales. Screenshots: ${screenshots}`)
} finally { await browser.close() }
