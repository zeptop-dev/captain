// Real browser / production bundle, mocked local APIs only. No remote requests.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.ENTRY_IP_TEST_URL || 'http://127.0.0.1:5198/admin/')
assert.ok(['127.0.0.1','localhost','[::1]'].includes(base.hostname))
const artifacts = process.env.ENTRY_IP_ARTIFACTS || '/tmp/captain-entry-ip-ui'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
async function setup({ locale = 'en', mobile = false, role = 'admin', existing = false, failure = '', line = false } = {}) {
  const dict = JSON.parse(await readFile(resolve(root, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => { const text = key.split('.').reduce((v,k) => v?.[k],dict); assert.equal(typeof text,'string',key); return text }
  const context = await browser.newContext({ viewport: { width: mobile ? 390 : 1440, height: 1000 }, serviceWorkers: 'block' })
  await context.addInitScript(locale => localStorage.setItem('i18nextLng', locale), locale)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { writes: [], unexpected: [], errors: [], entries: existing ? [{ ID: 9, Name: 'Legacy node', InboundID: 1, DisplayHost: 'node.example.com', DisplayPort: 443, Rate: 1, Sort: 0, Enabled: true, Tags: [], ClientExtra: null, ...(line ? { LineID: 1, LineMemberID: 'front' } : {}) }] : [], supportFailure: false, holdSupport: false, releaseSupport: null }
  const node = { id: 1, name: 'Example server', mode: 'full', public_addr: '192.0.2.10', domain: 'node.example.com', online: true, paired: true, inbounds: 1 }
  const inbound = { ID: 1, NodeID: 1, Tag: 'Example inbound', Protocol: 'vless', Port: 443, Listen: '', Enabled: true, Settings: {} }
  const access = { revision: 'test', inbounds: [{ inbound_id: 1, node_id: 1, node_name: node.name, tag: inbound.Tag, protocol: 'vless', port: 443, policy: 'all', group_ids: [], legacy: false, editable: true, revision: 'test', entries: state.entries.map(e => ({ id: e.ID, name: e.Name })), plan_ids: [] }], groups: [], plans: [] }
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
    if (url.origin !== base.origin) { state.unexpected.push(url.origin); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const send = (value,status=200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
    if (path === '/api/admin/entries/ip-version-support') {
      if (state.supportFailure) return send({ error: 'Fixture unavailable' },503)
      const policy = url.searchParams.get('ip_version')
      if (state.holdSupport) await new Promise(resolve => state.releaseSupport = resolve)
      return send({ formats: [{ format:'clash',supported:true,reason:'' },{format:'uri',supported:policy==='dual',reason:policy==='dual'?'client_native':'unsupported_family_policy'}], template_conflicts: policy==='ipv6' && role==='admin'?['clash']:[], templates_checked: role==='admin' })
    }
    if ((path === '/api/admin/entries/variants' || path === '/api/admin/entries' || path === '/api/admin/entries/9') && method !== 'GET') {
      state.writes.push({ path, method, body: request.postDataJSON() })
      if (failure === 'network') return route.abort('failed')
      if (failure === 'server') return send({ error: 'Fixture unavailable' },502)
      if (failure === 'validation') return send({ error: 'Fixture validation error' },400)
      if (path.endsWith('/variants')) { state.entries = ['ipv4','ipv6','dual'].map((ip,i) => ({...request.postDataJSON(), ID: i+1, IPVersion:ip, Name:`Test · ${ip}`})); return send({ entries: state.entries }) }
      const entry = {...request.postDataJSON(),ID:9}; state.entries=[entry]; return send(entry)
    }
    if (method !== 'GET') { state.unexpected.push(`${method} ${path}`); return send({error:'Unexpected write'},500) }
    switch (path) {
      case '/api/admin/me': return send({id:1,email:`${role}@example.com`,role})
      case '/api/site': return send({name:'Captain fixture',branding:{},theme:{}})
      case '/api/admin/system/update': return send({captain:{current:'v1.58.0',latest:'v1.58.0',has_update:false}})
      case '/api/admin/entries': return send(state.entries)
      case '/api/admin/entries/tags': case '/api/admin/entry-categories': return send([])
      case '/api/admin/nodes': return send([node])
      case '/api/admin/nodes/1': return send({node,inbounds:[inbound],ingresses:[]})
      case '/api/admin/node-access': return send(access)
      case '/api/admin/lines': return send(line ? [{id:1,name:'Example line',target_inbound_id:1,endpoints:[{member_id:'front',node_id:1,host:'line.example.com',port:443}]}]:[])
      default: state.unexpected.push(`${method} ${path}`); return send({})
    }
  })
  const page=await context.newPage();page.on('pageerror',e=>state.errors.push(e.message))
  await page.goto(new URL(existing?'entries':'entries?create=node&inbound=1',base).href)
  if(existing) await page.getByRole('button',{name:t('common.edit'),exact:true}).click()
  const dialog=page.getByRole('dialog').first();await dialog.waitFor()
  const choose = async (key,value) => { await dialog.getByRole('textbox',{name:t(key),exact:true}).click(); await page.getByRole('option',{name:t(value),exact:true}).click() }
  const finish=async()=>{assert.deepEqual(state.errors,[]);assert.deepEqual(state.unexpected,[]);await context.close()}
  return {page,dialog,choose,state,t,context,finish}
}
try {
  for (const locale of ['en','zh-CN','zh-TW','ja','ko','ru']) {
    const s=await setup({locale,mobile:locale!=='en'}),{dialog,page,choose,state,t}=s
    assert.equal(await dialog.getByRole('textbox',{name:t('entryIP.label'),exact:true}).inputValue(),t('entryIP.options.dual'))
    await dialog.getByRole('button',{name:t('entryIP.supportTitle'),exact:true}).click()
    await dialog.getByText(t('entryIP.clientNative').replace('{{formats}}','uri'),{exact:true}).waitFor()
    await choose('entryIP.label','entryIP.options.ipv6')
    await dialog.getByText(t('entryIP.templateConflict').replace('{{formats}}','clash')).waitFor()
    await dialog.getByRole('textbox',{name:t('entryIP.label'),exact:true}).scrollIntoViewIfNeeded()
    await page.screenshot({path:resolve(artifacts,`entry-ip-${locale}.png`)})
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
    await dialog.getByRole('button',{name:t('common.save'),exact:true}).click()
    await dialog.waitFor({state:'hidden'});assert.equal(state.writes.length,1);assert.equal(state.writes[0].body.IPVersion,'ipv6');assert.equal(state.writes[0].body.DisplayHost,'node.example.com')
    await s.finish()
  }
  {
    const s=await setup({existing:true}),{dialog,choose,state,t}=s
    assert.equal(await dialog.getByRole('textbox',{name:t('entryIP.label'),exact:true}).inputValue(),t('entryIP.options.inherit'))
    await dialog.getByRole('textbox',{name:t('entries.name'),exact:true}).fill('Renamed legacy')
    await dialog.getByRole('button',{name:t('common.save'),exact:true}).click();await dialog.waitFor({state:'hidden'});assert.equal(state.writes[0].body.IPVersion,'')
    await s.finish()
  }
  {
    const s=await setup(),{dialog,page,choose,state,t}=s
    await dialog.getByRole('textbox',{name:t('entryFlow.connectHost'),exact:true}).fill('[2001:db8::20]')
    assert.equal(await dialog.getByRole('textbox',{name:t('entryIP.label'),exact:true}).inputValue(),t('entryIP.options.ipv6'))
    assert.equal(await dialog.getByRole('checkbox',{name:t('entryIP.variants')}).isDisabled(),true)
    await dialog.getByRole('textbox',{name:t('entryFlow.connectHost'),exact:true}).fill('node.example.com')
    await choose('entryIP.label','entryIP.options.ipv4')
    await dialog.getByRole('textbox',{name:t('entryFlow.connectHost'),exact:true}).fill('2001:db8::20')
    await dialog.getByText(t('entryIP.errors.familyMismatch'),{exact:true}).waitFor()
    assert.equal(await dialog.getByRole('button',{name:t('common.save'),exact:true}).isDisabled(),true)
    await dialog.getByRole('textbox',{name:t('entryFlow.connectHost'),exact:true}).fill('node.example.com')
    await dialog.getByRole('textbox',{name:t('entries.clientExtra'),exact:true}).fill('{"ip-version":"ipv6"}')
    await dialog.getByText(t('entryIP.errors.extraConflict'),{exact:true}).waitFor();assert.equal(state.writes.length,0)
    await s.finish()
  }
  {
    const s=await setup(),{dialog,state,t}=s
    await dialog.getByRole('checkbox',{name:t('entryIP.variants')}).check()
    await dialog.getByRole('button',{name:t('entryIP.createVariants'),exact:true}).click();await dialog.waitFor({state:'hidden'})
    assert.equal(state.writes.length,1);assert.equal(state.writes[0].path,'/api/admin/entries/variants');assert.equal(state.writes[0].body.InboundID,1)
    await s.finish()
  }
  {
    const s=await setup(),{dialog,choose,state,t}=s
    await dialog.getByRole('checkbox',{name:t('entryIP.variants')}).check()
    await choose('nodeAccess.title','nodeAccess.none')
    const submit=dialog.getByRole('button',{name:t('entryIP.createVariants'),exact:true})
    assert.equal(await submit.isDisabled(),true)
    await dialog.getByRole('checkbox',{name:t('nodeAccess.confirmShared'),exact:true}).check()
    await submit.click();await dialog.waitFor({state:'hidden'})
    assert.equal(state.writes.length,1);assert.equal(state.writes[0].body.Access.confirm_shared,true);assert.equal(state.writes[0].body.Access.policy,'none')
    await s.finish()
  }
  for(const failure of ['network','server']) {
    const s=await setup({failure}),{dialog,state,t}=s
    await dialog.getByRole('checkbox',{name:t('entryIP.variants')}).check();await dialog.getByRole('button',{name:t('entryIP.createVariants'),exact:true}).click()
    await dialog.getByText(t('entryIP.uncertain'),{exact:true}).waitFor();assert.equal(await dialog.getByRole('button',{name:t('entryIP.createVariants'),exact:true}).isDisabled(),true)
    await dialog.locator('form').evaluate(f=>f.requestSubmit());assert.equal(state.writes.length,1)
    assert.equal(await dialog.getByRole('button',{name:t('common.cancel'),exact:true}).isDisabled(),false)
    await s.finish()
  }
  {
    const s=await setup({role:'operator',existing:true,line:true}),{dialog,state,t}=s
    assert.equal(await dialog.getByRole('textbox',{name:t('entryIP.label'),exact:true}).isDisabled(),true)
    await dialog.getByRole('textbox',{name:t('entries.name'),exact:true}).fill('Renamed line')
    await dialog.getByRole('button',{name:t('common.save'),exact:true}).click();await dialog.waitFor({state:'hidden'})
    assert.equal(state.writes[0].body.IPVersion,'');assert.equal(state.writes[0].body.DisplayHost,'node.example.com');await s.finish()
  }
  {
    const s=await setup(),{dialog,choose,t}=s
    await choose('entryFlow.connection','entryFlow.clientChain')
    assert.equal(await dialog.getByRole('textbox',{name:t('entryIP.label'),exact:true}).inputValue(),t('entryIP.options.inherit'))
    assert.equal(await dialog.getByRole('textbox',{name:t('entryIP.label'),exact:true}).isDisabled(),true)
    await dialog.getByText(t('entryIP.chainHint'),{exact:true}).waitFor();await s.finish()
  }
  {
    const s=await setup(),{dialog,page,choose,state,t}=s
    await dialog.getByRole('button',{name:t('entryIP.supportTitle'),exact:true}).waitFor()
    state.holdSupport=true
    await choose('entryIP.label','entryIP.options.ipv6')
    for(let i=0;i<100 && !state.releaseSupport;i++) await new Promise(r=>setTimeout(r,20))
    assert.ok(state.releaseSupport)
    state.holdSupport=false
    await choose('entryIP.label','entryIP.options.ipv4')
    await dialog.getByRole('button',{name:t('entryIP.supportTitle'),exact:true}).waitFor()
    state.releaseSupport()
    await dialog.getByRole('button',{name:t('entryIP.supportTitle'),exact:true}).click()
    await dialog.getByText(t('entryIP.excluded').replace('{{formats}}','uri'),{exact:true}).waitFor()
    assert.equal(await dialog.getByText(t('entryIP.templateConflict').replace('{{formats}}','clash')).count(),0)
    await s.finish()
  }
  console.log('Entry IP browser checks passed: six locales, mobile, legacy, literal mismatch, extra conflicts, atomic variants, uncertain retries, operator and client chain.')
} finally {await browser.close()}
