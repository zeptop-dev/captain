import test from 'node:test'
import assert from 'node:assert/strict'
import { planAccessDraft, staticInboundIDs } from '../src/lib/plan-access.ts'
import { accessMatches } from '../src/lib/node-access.ts'
const directory={revision:'r1',groups:[{id:1,name:'Shared',category_ids:[7]}],plans:[{id:1,name:'Monthly',group_id:1},{id:2,name:'Annual',group_id:1}],categories:[{id:7,name:'Premium'}],inbounds:[
 {inbound_id:1,policy:'groups',group_ids:[1],static_group_ids:[],editable:true,entries:[{id:11,name:'First',category_id:7}],plan_ids:[1,2]},
 {inbound_id:2,policy:'groups',group_ids:[1],static_group_ids:[1],editable:true,entries:[],plan_ids:[1,2]},
 {inbound_id:3,policy:'none',group_ids:[],static_group_ids:[],editable:false,entries:[],plan_ids:[]}
]}
test('opening a plan does not pin dynamic category grants as individual selections',()=>{
 const draft=planAccessDraft(directory,1,1)
 assert.deepEqual(draft.inbound_ids,[2]);assert.deepEqual(draft.category_ids,[7]);assert.equal(draft.mode,'shared')
 draft.category_ids.push(8);assert.deepEqual(directory.groups[0].category_ids,[7])
})
test('a new plan starts without inherited nodes or followed categories',()=>{
 const draft=planAccessDraft(directory,0,null)
 assert.equal(draft.mode,'independent');assert.deepEqual(draft.inbound_ids,[]);assert.deepEqual(draft.category_ids,[])
})
test('private ownership and shared inbound identification remain explicit',()=>{
 const owned={...directory,plans:[{...directory.plans[0],independent:true}]}
 assert.equal(planAccessDraft(owned,1,1).mode,'independent')
 assert.deepEqual(staticInboundIDs(directory,1),[2])
})
test('plan filters use unique plan IDs even when multiple plans share a scheme',()=>{
 assert.equal(accessMatches(directory.inbounds[0],'plan:1'),true)
 assert.equal(accessMatches(directory.inbounds[0],'plan:2'),true)
 assert.equal(accessMatches(directory.inbounds[0],'plan:3'),false)
})
