// Local UI regression: start `pnpm dev --host 127.0.0.1 --port 5198` first.
// Supply PLAYWRIGHT_MODULE when Playwright is installed outside this workspace.
import assert from 'node:assert/strict'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
const screenshots = await mkdtemp(join(tmpdir(), 'captain-node-access-ui-'))
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const base = process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5198/admin/'
const node = { id: 1, name: 'Test server', public_addr: '192.0.2.10', domain: 'node.example.com', paired: true, proxy_allowed: true }
const inbound = (ID, Tag, GroupID) => ({ ID, NodeID: 1, Tag, Protocol: 'vless', Port: 443 + ID, Settings: {}, GroupID, Enabled: true, Sort: ID })
const inbounds = [inbound(1, 'shared', 1), inbound(2, 'open', null), inbound(3, 'closed', null)]
const entry = (ID, Name, InboundID) => ({ ID, Name, InboundID, DisplayHost: 'node.example.com', DisplayPort: 443 + InboundID, Rate: 1, Sort: ID, Enabled: true, Tags: [], Region: '', CategoryID: null, ClientExtra: null })
const seedEntries = [entry(10, 'Shared first', 1), entry(11, 'Shared second', 1), entry(12, 'Open node', 2), entry(13, 'Closed node', 3)]
const groups = [{ id: 1, name: 'Group A' }, { id: 2, name: 'Group B' }]
const plans = [{ id: 1, name: 'Plan A', group_id: 1 }, { id: 2, name: 'Plan A annual', group_id: 1 }, { id: 3, name: 'Plan B', group_id: 2 }]
const seedDirectory = { revision: 'r1', groups, plans, inbounds: inbounds.map((ib, i) => ({ inbound_id: ib.ID, node_id: 1, node_name: node.name, tag: ib.Tag, protocol: ib.Protocol, port: ib.Port, policy: ['groups', 'all', 'none'][i], group_ids: i === 0 ? [1] : [], revision: 'r1', editable: true, legacy: false, entries: seedEntries.filter(e => e.InboundID === ib.ID).map(e => ({ id: e.ID, name: e.Name })), plan_ids: [] })) }
async function setup(role = 'admin') {
 const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } })
 const state = { directory: structuredClone(seedDirectory), entries: structuredClone(seedEntries), requests: [], accessReads: 0, failAccess: false, conflict: false }
 await context.addInitScript(() => localStorage.setItem('i18nextLng', 'en'))
 await context.route('**/*', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname
  if (url.origin !== new URL(base).origin) return route.abort()
  if (!path.startsWith('/api/')) return route.continue()
  const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
  if (req.method() !== 'GET') {
   const body = req.postDataJSON(); state.requests.push({ method: req.method(), path, body })
   if (state.conflict) return send({ error: 'Access revision conflict' }, 409)
   if (path.startsWith('/api/admin/node-access/')) return send({})
   if (path.startsWith('/api/admin/entries/')) return send({ ...state.entries.find(e => path.endsWith(`/${e.ID}`)), ...body })
   if (path === '/api/admin/entries') return send({ ID: 99, ...body })
   return send({})
  }
  if (path === '/api/admin/me') return send({ id: 1, email: 'admin@example.com', role })
  if (path === '/api/admin/node-access') { state.accessReads++; return state.failAccess ? send({ error: 'Unavailable' }, 503) : send(state.directory) }
  if (path === '/api/admin/nodes') return send([node])
  if (path === '/api/admin/nodes/1') return send({ node, inbounds, ingresses: [] })
  if (path === '/api/admin/entries') return send(state.entries)
  if (path === '/api/admin/groups') return send(groups.map(g => ({ ID: g.id, Name: g.name })))
  if (path === '/api/admin/plans') return send(plans.map(p => ({ ID: p.id, Name: p.name, GroupID: p.group_id, PriceCents: 1000, PeriodDays: 30, QuotaBytes: 0, DeviceLimit: 0, SpeedLimitMbps: 0, Enabled: true, ResetDays: 0, Prices: [] })))
  if (['/api/admin/entry-categories', '/api/admin/entries/tags', '/api/admin/lines'].includes(path)) return send([])
  return send({})
 })
 const page = await context.newPage(); const errors = []; page.on('pageerror', error => errors.push(error.message))
 return { context, page, state, errors }
}
async function select(page, input, option) { await input.click(); await page.getByRole('option', { name: option, exact: true }).click() }
async function closeDirty(page) { await page.getByRole('dialog').last().getByRole('button', { name: 'Cancel', exact: true }).click(); const discard = page.getByRole('button', { name: 'Discard', exact: true }); if (await discard.count()) await discard.click() }
try {
 const { context, page, state, errors } = await setup()
 await page.goto(`${base}entries`)
 await page.getByRole('row').filter({ hasText: 'Shared first' }).getByRole('button', { name: 'Edit', exact: true }).click()
 let dialog = page.getByRole('dialog').last()
 await select(page, dialog.getByRole('textbox', { name: 'Access scope', exact: true }), 'No access')
 assert.equal(await dialog.getByRole('button', { name: 'Save', exact: true }).isDisabled(), true)
 await page.screenshot({ path: join(screenshots, 'entry-access.png'), fullPage: true })
 await dialog.getByRole('checkbox', { name: 'I confirm changing access for these shared nodes' }).check()
 await dialog.getByRole('button', { name: 'Save', exact: true }).click()
 await page.getByRole('dialog').waitFor({ state: 'hidden' })
 assert.deepEqual(state.requests.at(-1).body.Access, { inbound_id: 1, policy: 'none', group_ids: [], revision: 'r1', confirm_shared: true })
 await page.getByRole('button', { name: 'Create node', exact: true }).click()
 dialog = page.getByRole('dialog').last()
 await select(page, dialog.getByRole('textbox', { name: 'Target inbound', exact: true }), 'Test server / shared (vless:444)')
 await select(page, dialog.getByRole('textbox', { name: 'Selected user groups', exact: true }), 'Group B')
 assert.equal(await dialog.getByRole('button', { name: 'Save', exact: true }).isDisabled(), true)
 await dialog.getByRole('checkbox', { name: 'I confirm changing access for these shared nodes' }).check()
 await dialog.getByRole('button', { name: 'Save', exact: true }).click()
 await page.getByRole('dialog').waitFor({ state: 'hidden' })
 assert.equal(state.requests.at(-1).path, '/api/admin/entries')
 assert.equal(state.requests.at(-1).body.Access.inbound_id, 1)
 assert.deepEqual(state.requests.at(-1).body.Access.group_ids, [1, 2])
 // Reusing another target drops the permission draft and does not send Access.
 await page.getByRole('row').filter({ hasText: 'Shared first' }).getByRole('button', { name: 'Edit', exact: true }).click()
 dialog = page.getByRole('dialog').last()
 await select(page, dialog.getByRole('textbox', { name: 'Access scope', exact: true }), 'No access')
 await select(page, dialog.getByRole('textbox', { name: 'Target inbound', exact: true }), 'Test server / open (vless:445)')
 assert.equal(await dialog.getByRole('textbox', { name: 'Access scope', exact: true }).inputValue(), 'All users with an active plan')
 await dialog.getByRole('button', { name: 'Save', exact: true }).click()
 await page.getByRole('dialog').waitFor({ state: 'hidden' })
 assert.equal(state.requests.at(-1).body.InboundID, 2); assert.equal('Access' in state.requests.at(-1).body, false)
 // Managed nodes share the same dedicated access dialog, and stale saves remain open.
 await page.getByRole('row').filter({ hasText: 'Shared first' }).getByRole('button', { name: 'Manage access', exact: true }).click()
 dialog = page.getByRole('dialog').last()
 await select(page, dialog.getByRole('textbox', { name: 'Access scope', exact: true }), 'No access')
 await dialog.getByRole('checkbox', { name: 'I confirm changing access for these shared nodes' }).check()
 state.conflict = true
 await dialog.getByRole('button', { name: 'Save', exact: true }).click()
 await dialog.getByText('Access revision conflict').waitFor()
 assert.equal(await dialog.isVisible(), true)
 state.conflict = false; await closeDirty(page)
 // Plan editor saves group access independently and never removes all-plan rows.
 await page.goto(`${base}plans`)
 await page.getByRole('button', { name: 'Add plan', exact: true }).click()
 dialog = page.getByRole('dialog').last()
 await select(page, dialog.getByRole('textbox', { name: 'Group after purchase', exact: true }), 'Group A')
 await dialog.getByRole('button', { name: 'Manage available nodes', exact: true }).click()
 dialog = page.getByRole('dialog').last()
 await dialog.getByText('Plan A / Plan A annual', { exact: true }).waitFor()
 await page.screenshot({ path: join(screenshots, 'group-access.png'), fullPage: true })
 const openCheck = dialog.getByRole('checkbox', { name: /Open node/ })
 assert.equal(await openCheck.isChecked(), true); assert.equal(await openCheck.isDisabled(), true)
 await dialog.getByRole('checkbox', { name: /Closed node/ }).check()
 assert.equal(await dialog.getByRole('button', { name: 'Save', exact: true }).isDisabled(), true)
 await dialog.getByRole('checkbox', { name: 'I confirm the changes for all plans in this group and nodes sharing these inbounds' }).check()
 await dialog.getByRole('button', { name: 'Save', exact: true }).click()
 await page.waitForFunction(() => document.querySelectorAll('[role="dialog"]').length === 1)
 assert.deepEqual(state.requests.at(-1), { method: 'PUT', path: '/api/admin/node-access/groups/1', body: { inbound_ids: [1, 2, 3], revision: 'r1', confirm_shared: true } })
 assert.equal(state.requests.some(r => r.path === '/api/admin/plans'), false)
 await page.screenshot({ path: join(screenshots, 'plan.png'), fullPage: true })
 assert.deepEqual(errors, [])
 await context.close()
 const failure = await setup()
 failure.state.failAccess = true
 await failure.page.goto(`${base}entries`)
 await failure.page.getByRole('row').filter({ hasText: 'Shared first' }).getByRole('button', { name: 'Edit', exact: true }).click()
 await failure.page.getByRole('dialog').getByText('Could not load access settings. Existing permissions are unchanged.').waitFor()
 assert.equal(await failure.page.getByRole('dialog').getByRole('button', { name: 'Save', exact: true }).isDisabled(), true)
 assert.equal(failure.state.requests.length, 0)
 await failure.context.close()
 const operator = await setup('operator')
 await operator.page.goto(`${base}entries`)
 await operator.page.getByText('Shared first', { exact: true }).waitFor()
 assert.equal(operator.state.accessReads, 0)
 assert.equal(await operator.page.getByRole('button', { name: 'Manage access', exact: true }).count(), 0)
 await operator.context.close()
 console.log(`Screenshots: ${screenshots}`)
 console.log('PASS: shared confirmation, atomic Entry create/update, target switch, stale conflict, plan group edit, all-plan preservation, failed directory, operator gating')
} finally { await browser.close() }
