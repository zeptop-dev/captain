// Local preview only; separate real HTTP/SQLite tests cover the server.
import assert from 'node:assert/strict'
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright')
const base=process.env.ADMIN_TEST_URL||'http://127.0.0.1:5638/admin/'
const browser=await chromium.launch({headless:true,channel:'chrome'}),ctx=await browser.newContext({locale:'en-US',viewport:{width:1440,height:1000}})
const errors=[],filters=[],writes=[]
const users=[1,2].map(id=>({id,email:`user${id}@example.com`,status:'active',uuid:`uuid${id}`,sub_token:`test${id}`,group_id:id===1?null:7,balance_cents:0,created_at:'2026-01-01T00:00:00Z',plan_name:'Example',sub_usable:true,sub_count:1,quota_bytes:1024,used_bytes:0,expires_at:null}))
let covered=false
await ctx.addInitScript(()=>{localStorage.setItem('i18nextLng','en');localStorage.setItem('captain.user-filters.v1',JSON.stringify({group_id:'7'}))})
await ctx.route('**/*',async route=>{
 const req=route.request(),url=new URL(req.url()),path=url.pathname
 if(url.origin!==new URL(base).origin)return route.abort()
 if(!path.startsWith('/api/'))return route.continue()
 const send=(v,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(v)})
 if(req.method()!=='GET'){
  const body=req.postDataJSON();writes.push({path,body})
  if(req.method()==='PATCH'&&path==='/api/admin/users/2'){assert.equal('GroupID'in body,false);return send({ok:true})}
  if(path==='/api/admin/users/2/legacy-grant/migrate'){assert.equal(body.confirm,true);assert.equal(body.revision,'current');users[1].group_id=null;return send({ok:true})}
  return send({error:'Unexpected write'},500)
 }
 if(path==='/api/admin/me')return send({id:1,email:'admin@example.com',role:'admin',version:'dev'})
 if(path==='/api/site')return send({name:'Example',theme:{}})
 if(path==='/api/admin/system/update')return send({captain:{current:'dev',latest:'dev',has_update:false}})
 if(path==='/api/admin/system/selfcheck')return send({checks:[]})
 if(path==='/api/admin/plans')return send([{ID:1,Name:'Example',PriceCents:0}])
 if(path==='/api/admin/users'){filters.push(url.searchParams.get('group_id'));return send({items:users,total:2,page:1,per_page:50})}
 if(path==='/api/admin/users/subscription-profiles')return send([])
 if(path==='/api/admin/users/2/legacy-grant')return send({group_id:users[1].group_id,name:'Old permission',revision:'current',can_migrate:covered,covering_plans:covered?[{id:1,name:'Example'}]:[]})
 const m=path.match(/^\/api\/admin\/users\/(1|2)(.*)$/)
 if(m){const id=Number(m[1]),suffix=m[2];if(!suffix)return send({...users[id-1],subscriptions:[],devices:[],hwid_devices:[],sub_requests:[],hwid_limit:null,dyn_limit:null,first_connected_at:null});if(suffix==='/subscription-profile')return send({profile_id:null,effective:null});if(suffix==='/metadata')return send({});if(suffix==='/wireguard-devices')return send({available:false,reason:'no_wireguard',devices:[],entries:[],limit:0,legacy_enabled:false});return send([])}
 return send([])
})
try{
 const page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(base+'users')
 await page.getByText('user1@example.com',{exact:true}).click();const drawer=page.getByRole('dialog')
 await drawer.getByRole('heading',{name:'Edit',exact:true}).waitFor()
 assert.equal(await drawer.getByText('Legacy account authorization',{exact:true}).count(),0)
 assert.equal(await page.getByRole('textbox',{name:'User group',exact:true}).count(),0)
 await page.keyboard.press('Escape');await page.getByText('user2@example.com',{exact:true}).click()
 await drawer.getByText('Old permission',{exact:true}).waitFor();await drawer.getByRole('button',{name:'Save',exact:true}).first().click()
 await drawer.getByRole('button',{name:'Move authorization to plans',exact:true}).click();const modal=page.getByRole('dialog',{name:'Move authorization to plans',exact:true})
 assert.equal(await modal.getByRole('button',{name:'Confirm plan-based authorization',exact:true}).isDisabled(),true)
 assert.equal(await modal.getByRole('link',{name:'Manage plan permissions',exact:true}).getAttribute('href'),'/admin/plans')
 await modal.getByRole('button',{name:'Cancel',exact:true}).click();covered=true
 await drawer.getByRole('button',{name:'Move authorization to plans',exact:true}).click();await modal.getByText('Already covered by: Example',{exact:true}).waitFor()
 await modal.getByRole('button',{name:'Confirm plan-based authorization',exact:true}).click();await page.getByText('Old permission',{exact:true}).waitFor({state:'hidden'})
 assert.equal(users[1].group_id,null);assert.ok(filters.every(v=>v===null||v===''));assert.deepEqual(errors,[])
 console.log('Legacy authorization UI: empty account, retired controls/filter, omitted group save, coverage guard, migration and cache refresh passed.')
}finally{await ctx.close();await browser.close()}
