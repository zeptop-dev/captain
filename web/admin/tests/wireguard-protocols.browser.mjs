// Real built UI with synthetic API responses; backend/runtime tests are separate.
import assert from 'node:assert/strict'
import {readFile,mkdir} from 'node:fs/promises'
import {resolve,dirname} from 'node:path'
import {fileURLToPath} from 'node:url'
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..')
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright')
const base=process.env.WG_BROWSER_URL||'http://127.0.0.1:5679/admin/'
assert.equal(new URL(base).hostname,'127.0.0.1')
const artifacts=process.env.WG_BROWSER_ARTIFACTS||'/tmp/wg-protocols-browser';await mkdir(artifacts,{recursive:true})
const browser=await chromium.launch({headless:true,...(process.env.CHROME_EXECUTABLE?{executablePath:process.env.CHROME_EXECUTABLE}:{})})
try {
 for(const locale of ['en','zh-CN','zh-TW','ja','ko','ru']){
  const dictionary=JSON.parse(await readFile(resolve(root,`src/i18n/${locale}.json`),'utf8'))
  const t=key=>key.split('.').reduce((v,k)=>v[k],dictionary)
  const context=await browser.newContext({viewport:{width:locale==='en'?1440:390,height:950},serviceWorkers:'block'})
  await context.addInitScript(locale=>localStorage.setItem('i18nextLng',locale),locale)
  const nodes=[1,2].map(id=>({id,name:id===1?'Transit A':'Exit B',public_addr:id===1?'198.51.100.20':'203.0.113.30',paired:true,proxy_allowed:true,mode:'full',online:true}))
  const ib={ID:12,NodeID:1,Tag:'borrowed-socks',Protocol:'socks',Core:'singbox',Port:19444,Settings:{},Enabled:false}
  const connection={id:'fixture',version:1,exit_id:2,transit_id:1,name:'TLS entry',user_inbound_id:11,port:19443,tunnel_port:51820,endpoint:'198.51.100.20:51820',mtu:1380,server_name:'example.com',protocol:'vless',core:'xray',settings:{flow:'xtls-rprx-vision',tls:{mode:1,server_name:'example.com',auto_cert:true}},private_access:{mode:'off'},group_id:null,enabled:true,state:'connected',bindings:[]}
  const writes=[],errors=[],queries=[]
  await context.route('**/api/**',async route=>{
   const req=route.request(),url=new URL(req.url()),p=url.pathname;queries.push(url.href)
   const send=data=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(data)})
   if(req.method()!=='GET'){writes.push({path:p,method:req.method(),body:req.postDataJSON()});return send({id:'created',version:1,ok:true})}
   if(p==='/api/admin/me')return send({id:1,email:'admin@example.com',role:'admin'})
   if(p==='/api/site')return send({name:'Captain',branding:{},theme:{}})
   if(p==='/api/admin/nodes')return send(nodes)
   if(/^\/api\/admin\/nodes\/[12]$/.test(p))return send({node:nodes[Number(p.split('/').at(-1))-1],inbounds:p.endsWith('/1')?[ib]:[],ingresses:[]})
   if(p.endsWith('/wireguard-connections'))return send([connection])
   if(p.endsWith('/wireguard-readiness'))return send({ready:true,supported:true,fresh:true,checks:[],packages:[]})
   if(p.endsWith('/core-options'))return send({inventory_known:true,auto_core:'xray',options:['xray','singbox','singbox-extended'].map(name=>({name,compatible:true,available:true}))})
   if(p==='/api/admin/node-access')return send({groups:[],inbounds:[],entries:[],plans:[],categories:[]})
   return send([])
  })
  const page=await context.newPage();page.setDefaultTimeout(10000);page.on('pageerror',e=>errors.push(e.message))
  await page.goto(base+'entries?tab=lines&section=wireguard&server=2')
  await page.getByText('TLS entry',{exact:true}).waitFor()
  await page.getByRole('button',{name:t('common.edit'),exact:true}).click()
  let modal=page.getByRole('dialog').last()
  await modal.getByLabel(t('inbounds.protocol'),{exact:true}).waitFor()
  assert.equal(await modal.getByLabel(t('wireguard.entrySecurity'),{exact:true}).inputValue(),'TLS + Vision')
  await modal.getByRole('button',{name:t('common.cancel'),exact:true}).click()
  await page.getByRole('button',{name:t('wireguard.bindings'),exact:true}).click();modal=page.getByRole('dialog').last()
  await modal.getByLabel(t('wireguard.attachEntry'),{exact:true}).click();await page.getByRole('option',{name:'borrowed-socks · socks :19444',exact:true}).click()
  await modal.getByLabel(t('common.enabled'),{exact:true}).check()
  await modal.getByRole('button',{name:t('wireguard.attach'),exact:true}).click()
  await page.waitForFunction(()=>document.querySelectorAll('[role="dialog"]').length===0)
  assert.equal(writes.at(-1).body.inbound_id,12);assert.equal(writes.at(-1).body.enabled,true)
  await page.getByRole('button',{name:t('common.create'),exact:true}).last().click();modal=page.getByRole('dialog').last()
  await modal.getByLabel(t('entries.name'),{exact:false}).fill('Independent transport')
  await modal.getByLabel(t('wireguard.transit'),{exact:false}).click();await page.getByRole('option',{name:'Transit A',exact:true}).click()
  await modal.getByLabel(t('wireguard.independent'),{exact:false}).check()
  assert.equal(await modal.getByLabel(t('inbounds.protocol'),{exact:true}).count(),0)
  assert.equal(await modal.getByLabel(t('reverse.userPort'),{exact:true}).count(),0)
  await page.screenshot({path:resolve(artifacts,`${locale}-independent.png`),fullPage:true})
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false)
  await modal.getByRole('button',{name:t('common.save'),exact:true}).click()
  await page.waitForFunction(()=>document.querySelectorAll('[role="dialog"]').length===0)
  assert.equal(writes.at(-1).body.independent,true);assert.equal(writes.at(-1).body.transit_id,1)
  assert.ok(queries.some(q=>q.includes('wireguard_transit=true')))
  assert.deepEqual(errors,[])
  await context.close()
 }
 console.log('Six locales: TLS/Vision preservation, explicit binding enable, independent transport fields and payload, mobile overflow passed.')
}finally{await browser.close()}
