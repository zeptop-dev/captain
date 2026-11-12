import assert from 'node:assert/strict'
import {readFile,mkdir} from 'node:fs/promises'
const {chromium}=await import('../../probe/node_modules/playwright/index.mjs')
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_EXECUTABLE||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'})
const base='http://127.0.0.1:5682/admin/'
await mkdir('/tmp/captain-plan-access-browser',{recursive:true})
async function setup(lang='en',width=1440) {
 const d=JSON.parse(await readFile(new URL('../src/i18n/'+lang+'.json',import.meta.url)));const t=k=>k.split('.').reduce((x,k)=>x[k],d)
 const context=await browser.newContext({viewport:{width,height:1000},serviceWorkers:'block'});await context.addInitScript(lang=>localStorage.setItem('i18nextLng',lang),lang)
 const plans=[{ID:1,Name:'Monthly',PriceCents:1000,PeriodDays:30,QuotaBytes:100<<20,ResetDays:0,DeviceLimit:0,SpeedLimitMbps:0,GroupID:1,Enabled:true},{ID:2,Name:'Annual',PriceCents:10000,PeriodDays:365,QuotaBytes:100<<20,ResetDays:0,DeviceLimit:0,SpeedLimitMbps:0,GroupID:1,Enabled:true}]
 const directory={revision:'r1',conversion:{at:1,inbounds:2,plans:2},categories:[{id:1,name:'Premium'}],groups:[{id:1,name:'Ordinary',category_ids:[1]}],plans:[{id:1,name:'Monthly',group_id:1,independent:false},{id:2,name:'Annual',group_id:1,independent:false}],inbounds:[{inbound_id:1,node_id:1,node_name:'Server',tag:'Private',policy:'groups',group_ids:[1],static_group_ids:[],editable:true,entries:[{id:1,name:'London',category_id:1}],plan_ids:[1,2],revision:'r1'},{inbound_id:2,node_id:1,node_name:'Server',tag:'Public',policy:'none',group_ids:[],static_group_ids:[],editable:true,entries:[{id:2,name:'Public'}],plan_ids:[1,2],revision:'r1'}]}
 const state={writes:[],fail:false,nameConflict:false,errors:[]}
 await context.routeWebSocket('**/*',s=>s.close())
 await context.route('**/*',async route=>{
  const req=route.request(),u=new URL(req.url()),p=u.pathname
  if(u.origin!==new URL(base).origin)return route.abort()
  if(!p.startsWith('/api/'))return route.continue()
  const send=(body,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(body)})
  if(req.method()!=='GET'){state.writes.push({path:p,body:req.postDataJSON()});return state.nameConflict?send({error:'plan name is already in use'},409):state.fail?send({error:'Save failed'},503):send({...req.postDataJSON(),ID:3})}
  switch(p){case '/api/admin/me':return send({id:1,role:'admin',email:'admin@example.com'});case '/api/site':return send({name:'Captain',branding:{},theme:{}});case '/api/admin/plans':return send(plans);case '/api/admin/groups':return send([{ID:1,Name:'Ordinary'}]);case '/api/admin/node-access':return send(directory);default:return send({})}
 })
 const page=await context.newPage();page.on('pageerror',e=>state.errors.push(e.message));await page.goto(base+'plans');await page.getByText('Monthly',{exact:true}).waitFor()
 return {page,context,state,t}
}
try{
 const r=await setup('zh-CN');const {page,state,t}=r
 await page.getByRole('button',{name:t('plans.create'),exact:true}).click()
 const modal=page.getByRole('dialog');await modal.getByRole('textbox',{name:t('planAccess.mode'),exact:true}).waitFor()
 assert.equal(await modal.getByRole('textbox',{name:t('planAccess.mode'),exact:true}).inputValue(),t('planAccess.independent'))
 await modal.getByRole('textbox',{name:t('plans.name'),exact:true}).fill(' monthly ')
 await modal.getByRole('checkbox',{name:t('planAccess.confirm'),exact:true}).check()
 await modal.getByRole('button',{name:t('common.save'),exact:true}).click()
 await modal.getByText(t('planNames.duplicate'),{exact:true}).waitFor()
 assert.equal(state.writes.length,0)
 await modal.getByRole('textbox',{name:t('plans.name'),exact:true}).fill('New plan')
 const follow=modal.getByRole('checkbox',{name:t('planSelection.follow'),exact:true})
 await follow.check()
 assert.ok(await modal.getByRole('checkbox',{name:/London/}).isChecked())
 assert.ok(await modal.getByRole('checkbox',{name:/London/}).isDisabled())
 await follow.uncheck()
 assert.equal(await modal.getByRole('checkbox',{name:/London/}).isChecked(),false)
 await modal.getByRole('button',{name:t('planSelection.selectCurrent'),exact:true}).click()
 assert.ok(await modal.getByRole('checkbox',{name:/London/}).isChecked())
 assert.equal(state.writes.length,0)
 const save=modal.getByRole('button',{name:t('common.save'),exact:true});assert.ok(await save.isDisabled())
 await modal.getByRole('checkbox',{name:t('planAccess.confirm'),exact:true}).check()
 state.fail=true
 await Promise.all([page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/plans')),save.click()]);await page.getByText('Save failed',{exact:true}).waitFor()
 assert.equal(await modal.getByRole('textbox',{name:t('plans.name'),exact:true}).inputValue(),'New plan')
 state.fail=false;state.nameConflict=true
 await Promise.all([page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/plans')),save.click()])
 await modal.getByText(t('planNames.duplicate'),{exact:true}).waitFor()
 assert.equal(await modal.getByRole('textbox',{name:t('plans.name'),exact:true}).inputValue(),'New plan')
 state.nameConflict=false
 await modal.getByRole('textbox',{name:t('plans.name'),exact:true}).fill('New plan revised')
 await Promise.all([page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/plans')),save.click()]);await modal.waitFor({state:'hidden'})
 const body=state.writes.at(-1).body;assert.equal(body.Access.mode,'independent');assert.deepEqual(body.Access.inbound_ids,[1]);assert.deepEqual(body.Access.category_ids,[]);assert.ok(!('GroupID'in body));assert.equal(body.PriceCents,1000)
 // Existing plans expose the shared scope and its other consumers.
 await page.locator('tr').filter({hasText:'Monthly'}).getByRole('button').first().click()
 await modal.getByText(t('planAccess.impact')+': Monthly / Annual',{exact:true}).waitFor()
 await modal.getByRole('textbox',{name:t('planAccess.mode'),exact:true}).click();await page.getByRole('option',{name:t('planAccess.independent'),exact:true}).click()
 assert.equal(state.writes.length,3)
 await modal.screenshot({path:'/tmp/captain-plan-access-browser/desktop.png'})
 assert.deepEqual(state.errors,[]);await r.context.close()
 for(const lang of ['en','zh-CN','zh-TW','ja','ru','ko']){
  const m=await setup(lang,390);await m.page.getByRole('button',{name:m.t('plans.create'),exact:true}).click();const dialog=m.page.getByRole('dialog');await dialog.getByRole('textbox',{name:m.t('planAccess.mode'),exact:true}).waitFor()
  assert.ok(await m.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await dialog.screenshot({path:'/tmp/captain-plan-access-browser/'+lang+'.png'});assert.deepEqual(m.state.errors,[]);await m.context.close()
 }
 console.log('Plan access browser checks passed: default independent, one atomic save, confirmation, failed draft, sharing impact, six mobile locales')
}finally{await browser.close()}
