import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
const { chromium }=await import('../../probe/node_modules/playwright/index.mjs')
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_EXECUTABLE||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'})
const base='http://127.0.0.1:5676/admin/',output='/tmp/captain-mail-browser'
await mkdir(output,{recursive:true})
const kinds=['register','reset','expiry','traffic','notice','test']
const defaults=Object.fromEntries(['en','zh-CN','zh-TW','ja','ru','ko'].map(lang=>[lang,Object.fromEntries(kinds.map(kind=>[kind,{subject:'{{site}} '+kind,body:'Example '+kind,button:'Open'}]))]))
async function setup(locale,width) {
 const dict=JSON.parse(await readFile(new URL(`../src/i18n/${locale}.json`,import.meta.url)))
 const t=key=>key.split('.').reduce((v,k)=>v[k],dict)
 const context=await browser.newContext({viewport:{width,height:1000},serviceWorkers:'block'})
 await context.addInitScript(lang=>{if(window===window.top)localStorage.setItem('i18nextLng',lang)},locale)
 await context.routeWebSocket('**/*',socket=>socket.close())
 const state={design:{name:'',logo:'',color:'',footer:'',templates:{}},error:false,errors:[],writes:[],samples:[]}
 await context.route('**/*',async route=>{
  const req=route.request(),url=new URL(req.url()),path=url.pathname
  if(url.origin!==new URL(base).origin)return route.abort()
  if(!path.startsWith('/api/'))return route.continue()
  const send=(body,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(body)})
  if(path==='/api/admin/settings/mail/templates') {
   if(req.method()==='PUT'){state.writes.push(req.postDataJSON());if(state.error)return send({error:'Unavailable'},503);state.design=req.postDataJSON()}
   return send({design:state.design,defaults,variables:[]})
  }
  if(path==='/api/admin/settings/mail/preview'){const b=req.postDataJSON();state.samples.push(b);return send({subject:'[TEST] '+b.design.name,html:'<html><body><h2>'+b.design.name+'</h2><p>123456</p></body></html>'})}
  if(path==='/api/admin/settings/mail/test'){state.samples.push(req.postDataJSON());return send({sent:true})}
  if(req.method()!=='GET')throw new Error('Unexpected write '+path)
  switch(path){
   case '/api/admin/me':return send({id:1,role:'admin',email:'admin@example.com'})
   case '/api/site':return send({name:'Captain',branding:{},theme:{}})
   case '/api/admin/settings/login-clients':return send({clients:[],base_url:'https://example.com'})
   case '/api/admin/settings/mail':return send({settings:{provider:'',smtp:{},resend:{},language:'en'}})
   case '/api/admin/settings/telegram':return send({settings:{},status:{}})
   case '/api/admin/settings/webhooks':return send({settings:{endpoints:[]},events:[]})
   case '/api/admin/settings/heartbeat':return send({settings:{},status:{}})
   default:return send({})
  }
 })
 const page=await context.newPage();page.on('pageerror',e=>state.errors.push(e.message))
 await page.goto(base+'settings/category/integrations#mail')
 const card=page.locator('.mantine-Card-root').filter({has:page.getByRole('heading',{name:t('mailTemplates.title'),exact:true})})
 try {await card.getByLabel(t('mailTemplates.name'),{exact:true}).waitFor({timeout:10000})} catch(e) { console.log(state.errors, await page.locator('body').innerText()); await page.screenshot({path:output+'/failure.png',fullPage:true});throw e }
 return {context,page,card,state,t}
}
try {
 const {context,page,card,state,t}=await setup('zh-CN',1440)
 const name=card.getByLabel(t('mailTemplates.name'),{exact:true})
 const label=t('settingsLayout.saveSection').replace('{{section}}',t('mailTemplates.title'))
 const save=card.getByRole('button',{name:label,exact:true})
 await name.fill('Example Brand');await card.getByLabel(t('mailTemplates.body'),{exact:true}).fill('Hello {{site}}')
 state.error=true
 await Promise.all([page.waitForResponse(r=>r.url().endsWith('/mail/templates')&&r.status()===503),save.click()])
 await page.getByText('Unavailable',{exact:true}).waitFor();assert.equal(await name.inputValue(),'Example Brand')
 state.error=false
 await Promise.all([page.waitForResponse(r=>r.url().endsWith('/mail/templates')&&r.request().method()==='PUT'&&r.status()===200),save.click()])
 await page.waitForFunction(label=>[...document.querySelectorAll('button')].find(b=>b.textContent===label)?.disabled,label)
 await name.fill('Unsaved Brand')
 await card.getByRole('button',{name:t('mailTemplates.preview'),exact:true}).click()
 await card.getByText('[TEST] Unsaved Brand',{exact:true}).waitFor()
 const frame=card.locator('iframe');assert.equal(await frame.getAttribute('sandbox'),'');assert.equal(await frame.getAttribute('referrerpolicy'),'no-referrer');assert.ok((await frame.getAttribute('srcdoc')).includes("default-src 'none'"))
 await card.getByLabel(t('mail.testTo'),{exact:true}).fill('sample@example.com')
 await Promise.all([page.waitForResponse(r=>r.url().endsWith('/mail/test')),card.getByRole('button',{name:t('mailTemplates.send'),exact:true}).click()])
 assert.equal(state.samples.at(-1).design.name,'Unsaved Brand');assert.equal(state.writes.length,2)
 await card.getByRole('button',{name:t('mailTemplates.restore'),exact:true}).click()
 assert.equal(await card.getByLabel(t('mailTemplates.body'),{exact:true}).inputValue(),'Example register')
 await card.scrollIntoViewIfNeeded();await page.screenshot({path:output+'/desktop.png',fullPage:true});assert.deepEqual(state.errors,[])
 await context.close()
 for(const locale of ['en','zh-CN','zh-TW','ja','ru','ko']) {
  const m=await setup(locale,390);await m.card.scrollIntoViewIfNeeded()
  assert.ok(await m.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
  await m.page.screenshot({path:output+'/'+locale+'-mobile.png',fullPage:true});assert.deepEqual(m.state.errors,[]);await m.context.close()
 }
 console.log('Mail template browser checks passed: independent saving, failed drafts, sample draft, sandbox, reset, six mobile locales')
} finally{await browser.close()}
