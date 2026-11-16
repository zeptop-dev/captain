// Browser workflow against local production UI with bounded mocked APIs.
// Real Captain/bosun TLS and journal migration are tested separately.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
const base = process.env.ADMIN_TEST_URL || 'http://127.0.0.1:5669/admin/'
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname))
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const en = JSON.parse(await readFile(new URL('../src/i18n/en.json', import.meta.url)))
const original = { id: 'old', name: 'Old entry', enabled: true, url: 'https://old.example.com:2096', listen: ':2096', certificate_id: 1, revision: 'r1', default: true, users: 2, configured_users: 2, unknown_users: 0, pending: 0 }
const nodes = [1, 2].map(id => ({ id, name: `node-${id}`, mode: 'monitor', reported_mode: 'monitor', paired: true, online: true, hostname: `host-${id}`, public_addr: '198.51.100.20', domain: '', version: 'v0.98.0', platform: 'linux/amd64', inbounds: 0, traffic_today_bytes: 0, agent_url: original.url, effective_agent_url: original.url, download_source: 'github', monitor_group: '', last_seen_at: new Date().toISOString() }))
const state = { entries: [{ ...original }], checks: [], previews: [], migrations: [], tasks: [], writes: [], errors: [], unexpected: [], failReads: false, conflict: false }
const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, serviceWorkers: 'block' })
await context.addInitScript(() => { localStorage.setItem('i18nextLng', 'en') })
await context.routeWebSocket('**/*', socket => socket.close())
await context.route('**/*', async route => {
 const r = route.request(), u = new URL(r.url())
 if (u.origin !== new URL(base).origin) return route.abort()
 if (!u.pathname.startsWith('/api/')) return route.continue()
 const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
 const path = u.pathname
 if(path==='/api/admin/agent-connections/detect'&&r.method()==='POST'){const input=r.postDataJSON();state.previews.push(input);return send({batch_id:'probe-batch-0123456789012345'})}
 if(path==='/api/admin/agent-connections/detect/probe-batch-0123456789012345'&&state.failReads)return send({error:'Detection read failed'},503)
 if(path==='/api/admin/agent-connections/detect/probe-batch-0123456789012345')return send({revision:'detected-preview',target_url:'https://target.example.com:2096',ready:true,trust_proxy:true,nodes:nodes.map(n=>({node_id:n.id,name:n.name,url:original.url,status:'trusted-proxy',paired:true,error:''}))})
 if (path === '/api/admin/settings/agent-endpoints' && r.method() === 'GET') return state.failReads ? send({ error: 'Read failed' }, 503) : send({ entries: state.entries, statuses: Object.fromEntries(state.entries.map(e => [e.id, { state: 'listening', active_url: e.url }])) })
 if (path === '/api/admin/settings/agent-endpoints' && r.method() === 'POST') { const input = r.postDataJSON(); state.writes.push(input); state.entries.push({ ...input, id: 'new', revision: 'r2', users: 0, configured_users: 0, unknown_users: 0, pending: 0 }); return send(state.entries.at(-1)) }
 if (path.match(/^\/api\/admin\/settings\/agent-endpoints\/[^/]+\/check$/)) { const input = r.postDataJSON(), id = path.split('/').at(-2), entry = state.entries.find(e => e.id === id); assert.equal(input.revision, entry.revision); state.checks.push(input); return send({ status: 'ok', revision: entry.revision, url: entry.url }) }
 if (path.match(/^\/api\/admin\/settings\/agent-endpoints\/[^/]+$/) && r.method() === 'PUT') { const input = r.postDataJSON(); state.writes.push(input); if (state.conflict) return send({ error: 'Entry changed' }, 409); return send(input) }
 if (path.match(/^\/api\/admin\/settings\/agent-endpoints\/[^/]+$/) && r.method() === 'DELETE') return send({ error: 'Still referenced; migrate nodes first' }, 400)
 if (path === '/api/admin/nodes/agent-https') return send({ enabled: true, url: original.url, ready: true, entries: state.entries.map(e => ({ ...e, ready: true })) })
 if (path === '/api/admin/agent-connections/preview') { const input = r.postDataJSON(); state.previews.push(input); return send({ revision: `preview-${state.previews.length}`, target_url: 'https://agent.example.com:2096', nodes: input.node_ids.map(id => ({ node_id: id, name: `node-${id}`, url: original.url, target_url: 'https://agent.example.com:2096', capable: true, paired: true })) }) }
 if (path === '/api/admin/agent-connections/migrate') { if(state.conflict)return send({error:'connection settings changed; reload and retry'},409); const input = r.postDataJSON(); state.migrations.push(input); assert.equal(input.revision, 'detected-preview'); state.tasks = input.node_ids.map(id => ({ id: `migration-${id}`, batch_id: 'batch', node_id: id, name: `node-${id}`, source_url: original.url, target_url: 'https://agent.example.com:2096', status: 'queued' })); return send({ batch_id: 'batch' }) }
 if (path === '/api/admin/agent-connections') return send({ migrations: state.tasks })
 if (path.startsWith('/api/admin/agent-connections/entries/')) return send({ nodes: nodes.map(n => ({ node_id: n.id, name: n.name, url: original.url, target_url: original.url, capable: true, paired: true })), truncated: false })
 if (r.method() !== 'GET') { state.unexpected.push(`${r.method()} ${path}`); return send({}, 500) }
 switch (path) {
 case '/api/admin/me': return send({ id: 1, email: 'admin@example.com', role: 'admin', version: 'v1.67.0' })
 case '/api/site': return send({ name: 'Captain', branding: {}, theme: {} })
 case '/api/admin/system/update': return send({ captain: { current: 'v1.67.0' } })
 case '/api/admin/dashboard': return send({})
 case '/api/admin/certificates': return send({ certificates: [{ id: 1, name: 'Fixture certificate', names: ['*.example.com'], not_after: '2030-01-01T00:00:00Z' }] })
 case '/api/admin/nodes': return send([...nodes].reverse())
 case '/api/admin/nodes/layout': return send({ revision: 'layout', groups: [], nodes: [] })
 case '/api/admin/nodes/domain-check': return send({ domain: '', valid: true, unchanged: true, conflicts: [] })
 case '/api/admin/settings/acme': return send({ email: '', has_cloudflare_token: true })
 case '/api/admin/settings/connlog': return send({ enabled: false, retention_days: 7, max_per_user: 100 })
 case '/api/admin/settings/audit': return send({ enabled: false })
 case '/api/admin/settings/dynlimit': return send({ enabled: false, rules: [] })
 case '/api/admin/audit-rules': case '/api/admin/audit-log': return send([])
 case '/api/admin/domains': return send({ domains: [], can_issue: true, global_token: true })
 default: state.unexpected.push(`${r.method()} ${path}`); return send({ error: 'Unexpected fixture request' }, 500)
 }
})
const page=await context.newPage();page.setDefaultTimeout(10000);page.on('pageerror',e=>state.errors.push(e.message))
try{
 await page.goto(`${base}nodes`)
 await page.getByRole('checkbox',{name:en.agentEntries.selectAll,exact:true}).check()
 await page.getByRole('button',{name:`${en.agentEntries.migrate} (2)`,exact:true}).click()
 const d=page.getByRole('dialog')
 assert.equal(await d.getByRole('checkbox').count(),0,'extra trust checkbox remained')
 assert.equal(await d.getByLabel(en.agentEntries.verification,{exact:true}).isVisible(),false,'advanced method required before detection')
 const confirm=d.getByRole('button',{name:en.agentEntries.confirmMigrate,exact:true});assert.equal(await confirm.isDisabled(),true)
 await d.getByRole('button',{name:en.agentEntries.detectPreview,exact:true}).click()
 const trust=d.getByRole('button',{name:en.agentEntries.confirmTrustMigrate,exact:true});await trust.waitFor();assert.equal(await trust.isDisabled(),false)
 assert.equal(state.migrations.length,0,'detection migrated automatically')
 state.failReads=true;await page.getByText(en.agentEntries.loadError,{exact:true}).first().waitFor();assert.equal(await d.getByRole('button',{name:en.agentEntries.confirmMigrate,exact:true}).isDisabled(),true,'failed polling retained confirmation');state.failReads=false;await trust.waitFor()
 await d.getByText(en.agentEntries.finalTrustHint,{exact:true}).waitFor()
 await d.getByLabel(en.agentEntries.mode,{exact:true}).click();await page.getByRole('option',{name:en.agentEntries.custom,exact:true}).click()
 await d.getByLabel(en.agentConnection.url,{exact:true}).fill('https://target.example.com:2096')
 assert.equal(await d.getByRole('button',{name:en.agentEntries.confirmMigrate,exact:true}).isDisabled(),true,'changed target retained old detection')
 await d.getByRole('button',{name:en.agentEntries.detectPreview,exact:true}).click();await trust.waitFor()
 if(process.env.AGENT_HTTPS_SCREENSHOTS){await mkdir(process.env.AGENT_HTTPS_SCREENSHOTS,{recursive:true});await d.screenshot({path:process.env.AGENT_HTTPS_SCREENSHOTS+'/auto-dialog.png'});await page.setViewportSize({width:390,height:844});await page.waitForTimeout(350);await d.screenshot({path:process.env.AGENT_HTTPS_SCREENSHOTS+'/auto-dialog-mobile.png'});await page.setViewportSize({width:1440,height:1100});await page.waitForTimeout(350)}
 state.conflict=true
 await trust.click()
 await page.getByText(en.agentEntries.previewChanged,{exact:true}).waitFor()
 assert.equal(state.migrations.length,0,'conflict retried migration automatically')
 assert.equal(await d.getByRole('button',{name:en.agentEntries.confirmMigrate,exact:true}).isDisabled(),true,'conflict retained stale confirmation')
 state.conflict=false
 await d.getByRole('button',{name:en.agentEntries.detectPreview,exact:true}).click();await trust.waitFor()
 await trust.click();await d.waitFor({state:'hidden'})
 assert.equal(state.migrations.length,1);assert.deepEqual(state.migrations[0].node_ids,[2,1]);assert.equal(state.migrations[0].verification_mode,'auto');assert.equal(state.migrations[0].detection_id,'probe-batch-0123456789012345');assert.equal(state.migrations[0].trust_proxy,true)
 assert.deepEqual(state.errors,[]);assert.deepEqual(state.unexpected,[])
 console.log('PASS: anonymous detection preview, no auto migration/checkbox, final explicit trust, target change invalidation and desktop/mobile')
}finally{await context.close();await browser.close()}
