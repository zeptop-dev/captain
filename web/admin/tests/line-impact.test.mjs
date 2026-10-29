import test from 'node:test'
import assert from 'node:assert/strict'
import { lineImpactInput, currentLineReview } from '../src/lib/line-impact.ts'

const draft = () => ({name:' Line ',backend:'relay',protocol:'tcp',target_inbound_id:9,target_address_mode:'default',groups:[{key:'form',strategy:'failover',members:[{key:'form-member',id:'one',node_id:1,port:443,weight:1,address_mode:'internal'}]}]})
test('review payload strips UI fields, preserves effective protocol/version and first-group address rules', () => {
 const result=lineImpactInput(draft(),'both',7)
 assert.equal(result.protocol,'both');assert.equal(result.version,7);assert.equal(result.name,'Line')
 assert.equal(result.groups[0].members[0].address_mode,'default');assert.equal(result.groups[0].members[0].ingress_id,null)
 assert.equal('key' in result.groups[0],false);assert.equal('key' in result.groups[0].members[0],false)
})
test('an exact review is invalidated by edits, effective protocol or loaded line version',()=>{
 const input=lineImpactInput(draft(),'tcp',7),source=JSON.stringify(input)
 assert.equal(currentLineReview(source,input),true)
 assert.equal(currentLineReview(undefined,input),false)
 assert.equal(currentLineReview(source,lineImpactInput(draft(),'udp',7)),false)
 assert.equal(currentLineReview(source,lineImpactInput(draft(),'tcp',8)),false)
 const edited=draft();edited.groups[0].members[0].port=444
 assert.equal(currentLineReview(source,lineImpactInput(edited,'tcp',7)),false)
})
