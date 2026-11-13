import assert from 'node:assert/strict'
import test from 'node:test'
import { designMainDNSPolicy, designDNSAutoDirect, designPolicyCanDirect, renameDesignGroup } from '../src/lib/sub-design.ts'
const d={groups:[{name:'Main',type:'select',members:['@all','DIRECT']},{name:'Auto',type:'url-test',members:['@all']}],rules:[],final:'Main',enhancements:{dns_mode:'follow'}}
test('follow mode identifies structural main group and distinguishes manual from automatic DIRECT',()=>{
 assert.equal(designMainDNSPolicy(d),'Main');assert.equal(designPolicyCanDirect(d,'Main'),true);assert.equal(designDNSAutoDirect(d,'Main'),false)
 const bad=structuredClone(d);bad.groups[0].members.push('Auto');bad.groups[1].members.push('DIRECT');assert.equal(designDNSAutoDirect(bad,'Main'),true)
 const renamed=renameDesignGroup({...d,enhancements:{dns_mode:'follow',dns_policy:'Main'}},0,'New');assert.equal(renamed.enhancements.dns_policy,'New')
})
