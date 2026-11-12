import assert from 'node:assert/strict'
import test from 'node:test'
import {planNameError} from '../src/lib/plan-names.ts'
const plans=[{ID:1,Name:'Ultra',Enabled:false},{ID:2,Name:'Legacy'},{ID:3,Name:' legacy '}]
test('new names include disabled plans and ignore outer spaces and case',()=>{
 for(const name of ['Ultra','ultra',' ULTRA '])assert.equal(planNameError(name,plans),'duplicate')
 assert.equal(planNameError('Distinct',plans),null)
})
test('existing historical duplicates can edit metadata, renaming cannot create duplicates',()=>{
 assert.equal(planNameError(' legacy ',plans,3),null)
 assert.equal(planNameError('LEGACY',plans,3),'duplicate')
 assert.equal(planNameError('Unique',plans,3),null)
})
test('blank, controls and long names fail validation',()=>{
 for(const name of [' ','bad\nname','x'.repeat(201)])assert.equal(planNameError(name,plans),'required')
 assert.equal(planNameError('Élite',[{ID:4,Name:'élite'}]),'duplicate')
})
