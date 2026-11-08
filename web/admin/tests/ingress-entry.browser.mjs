// Real browser / production bundle, mocked local APIs only. No remote requests.
import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const base = new URL(process.env.INGRESS_ENTRY_TEST_URL || 'http://127.0.0.1:5198/admin/')
assert.ok(['127.0.0.1','localhost','[::1]'].includes(base.hostname))
const artifacts = process.env.INGRESS_ENTRY_ARTIFACTS || '/tmp/captain-ingress-entry-ui'
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
async function setup({ locale = 'en', mobile = false, role = 'admin', existing = false, follow = false, failure = '', line = false, protocol = 'vless' } = {}) {
  const dict = JSON.parse(await readFile(resolve(root, `src/i18n/${locale}.json`), 'utf8'))
  const t = key => { const text = key.split('.').reduce((v,k) => v?.[k],dict); assert.equal(typeof text,'string',key); return text }
  const context = await browser.newContext({ viewport: { width: mobile ? 390 : 1440, height: 1000 }, serviceWorkers: 'block' })
  await context.addInitScript(locale => localStorage.setItem('i18nextLng', locale), locale)
  await context.routeWebSocket('**/*', socket => socket.close())
  const state = { writes: [], unexpected: [], errors: [], entries: existing ? [{ ID: 9, FollowIngress:follow, Name: 'Legacy node', InboundID: 1, DisplayHost: 'node.example.com', DisplayPort: 443, Rate: 1, Sort: 0, Enabled: true, Tags: [], ClientExtra: null, ...(line ? { LineID: 1, LineMemberID: 'front' } : {}) }] : [], supportFailure: false, holdSupport: false, releaseSupport: null }
  const node = { id: 1, name: 'Example server', mode: 'full', public_addr: '192.0.2.10', domain: 'node.example.com', online: true, paired: true, inbounds: 1 }
  const ingress={id:4,node_id:1,kind:'nat',name:'Example NAT',entry_host:'198.51.100.20',entry_domain:'entry.example.com',port_offset:10000,port_from:443,port_to:8443,bind_ip:'',line_ip:''}
  const inbound = { ID: 1, NodeID: 1, IngressID:4, Tag: 'Example inbound', Protocol: protocol, Port: 443, Listen: '', Enabled: true, Settings: {} }
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
    if(path==='/api/admin/nodes/1/ingresses' && method==='POST') { const body=request.postDataJSON(); state.writes.push({path,body});return send({ingress:{...ingress,id:5,entry_domain:body.EntryDomain,entry_host:node.public_addr}}) }
    if(path==='/api/admin/nodes/1/inbounds' && method==='POST') {const body=request.postDataJSON();state.writes.push({path,body});return send({...inbound,...body,ID:2,NodeID:1})}
    if (method !== 'GET') { state.unexpected.push(`${method} ${path}`); return send({error:'Unexpected write'},500) }
    switch (path) {
      case '/api/admin/domains':return send({domains:[{name:'example.com',provider:'cloudflare',auto_dns:true,has_token:true}],global_token:false})
      case '/api/admin/config-presets':case '/api/admin/groups':return send([])
      case '/api/admin/nodes/1/core-options':return send({options:[{name:'xray',compatible:true,available:true}],auto_core:'xray',inventory_known:true})
      case '/api/admin/me': return send({id:1,email:`${role}@example.com`,role})
      case '/api/site': return send({name:'Captain fixture',branding:{},theme:{}})
      case '/api/admin/system/update': return send({captain:{current:'v1.58.0',latest:'v1.58.0',has_update:false}})
      case '/api/admin/entries': return send(state.entries)
      case '/api/admin/entries/tags': case '/api/admin/entry-categories': return send([])
      case '/api/admin/nodes': return send([node])
      case '/api/admin/nodes/1': return send({node,inbounds:[inbound],ingresses:[ingress]})
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
  await page.screenshot({path:resolve(artifacts,`entry-${existing?'existing':'new'}.png`)})
  const finish=async()=>{assert.deepEqual(state.errors,[]);assert.deepEqual(state.unexpected,[]);await context.close()}
  return {page,dialog,choose,state,t,context,finish}
}
try {
  for(const existing of [false,true]) {
    const s=await setup({existing}),{dialog,t,state}=s
    const toggle=dialog.getByRole('switch',{name:t('ingress.followEndpoint')})
    assert.equal(await toggle.isChecked(),!existing)
    await dialog.getByRole('textbox',{name:t('entryFlow.connectHost'),exact:true}).fill('manual.example.net')
    assert.equal(await toggle.isChecked(),false)
    await toggle.check()
    assert.equal(await dialog.getByRole('textbox',{name:t('entryFlow.connectHost'),exact:true}).inputValue(),'entry.example.com')
    assert.equal(await dialog.getByRole('textbox',{name:t('entryFlow.connectPort'),exact:true}).inputValue(),'10443')
    await dialog.getByRole('button',{name:t('common.save'),exact:true}).click();await dialog.waitFor({state:'hidden'})
    assert.equal(state.writes[0].body.FollowIngress,true);await s.finish()
  }
  {
    const s=await setup({existing:true,follow:true}),{dialog,t,state}=s
    await dialog.getByRole('textbox',{name:t('entryFlow.connectPort'),exact:true}).fill('4443')
    assert.equal(await dialog.getByRole('switch',{name:t('ingress.followEndpoint')}).isChecked(),false)
    await dialog.getByRole('button',{name:t('common.save'),exact:true}).click();await dialog.waitFor({state:'hidden'})
    assert.equal(state.writes[0].body.FollowIngress,false);assert.equal(state.writes[0].body.DisplayPort,4443);await s.finish()
  }
  {
    const s=await setup(),{page,dialog,t,state}=s
    await dialog.getByRole('button',{name:t('entryFlow.newInbound'),exact:true}).click()
    const child=page.getByRole('dialog',{name:t('entryFlow.newInbound'),exact:true})
    await child.getByLabel(t('inbounds.tag')).fill('new-inbound')
    const pick=async(key,text)=>{await child.getByLabel(t(key),{exact:true}).click();await page.getByRole('option',{name:text,exact:true}).click()}
    await pick('inbounds.ingress',t('inbounds.ingressNew'))
    await child.getByLabel(t('ingress.name')).fill('Nested NAT')
    await pick('ingress.addressSource',t('ingress.addressIPv4'))
    await child.getByLabel(t('nodeDomain.prefix'),{exact:true}).fill('nested')
    assert.equal(await child.getByRole('button',{name:t('common.save'),exact:true}).isDisabled(),true)
    await pick('inbounds.ingress',t('inbounds.ingressDirect'))
    await pick('inbounds.ingress',t('inbounds.ingressNew'))
    assert.equal(await child.getByLabel(t('nodeDomain.prefix'),{exact:true}).inputValue(),'nested','Partial nested draft survives switching away and back')
    assert.equal(await child.getByRole('button',{name:t('common.save'),exact:true}).isDisabled(),true)
    await child.getByLabel(t('nodeDomain.zone'),{exact:true}).click();await page.getByRole('option').filter({has:page.getByText('example.com',{exact:true})}).click()
    await child.getByRole('button',{name:t('common.save'),exact:true}).click();await child.waitFor({state:'hidden'})
    assert.equal(state.writes[0].body.EntryAddressSource,'server_ipv4');assert.equal(state.writes[0].body.EntryDomain,'nested.example.com')
    assert.equal('DomainDraft' in state.writes[0].body,false);await s.finish()
  }
  console.log('PASS: subscription ingress following, legacy opt-out, manual override, inline NAT domain draft and source')
}finally{await browser.close()}
