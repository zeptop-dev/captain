import assert from 'node:assert/strict'
import test from 'node:test'
import { inboundDirectory, inboundUsage, managedInboundDestination } from '../src/lib/inbound-directory.ts'

const details=[{node:{id:1,name:'first'},inbounds:[{ID:10,NodeID:1,Protocol:'socks',Settings:{standalone:{username:'operator',password:'secret'}}}]},{node:{id:2,name:'second'},inbounds:[{ID:20,NodeID:2,Protocol:'vless',Settings:{}}]}]
const entries=[{ID:1,Name:'first node',InboundID:20},{ID:2,Name:'second node',InboundID:20}]

test('the global directory preserves each inbound owner and all linked subscriptions',()=>{
 const rows=inboundDirectory(details,entries)
 assert.equal(rows.length,2)
 assert.equal(rows[0].owner.node.id,rows[0].ib.NodeID)
 assert.equal(rows[1].owner.node.id,rows[1].ib.NodeID)
 assert.equal(rows[0].entries.length,0)
 assert.deepEqual(rows[1].entries.map(entry=>entry.ID),[1,2])
})
test('server, protocol and use filters compose without changing ownership or source data',()=>{
 assert.deepEqual(inboundDirectory(details,entries,2,'vless','plan').map(row=>row.ib.ID),[20])
 assert.deepEqual(inboundDirectory(details,entries,undefined,'socks','standalone').map(row=>row.ib.ID),[10])
 assert.deepEqual(inboundDirectory(details,entries,1,null,'plan'),[])
 assert.equal(details[0].inbounds.length,1)
 assert.deepEqual(inboundDirectory([],entries),[])
})

test('managed reverse inbounds open the owning exit rather than the listener server',()=>{
 const linked=[{InboundID:10,ManagedConnection:{type:'reverse',exit_node_id:9}}]
 assert.equal(managedInboundDestination(10,1,linked),'/entries?tab=lines&section=reverse&server=9')
 assert.equal(managedInboundDestination(11,1,linked),'/entries?node=1')
})

test('managed tunnel listeners are distinct from customer and independent credentials',()=>{
 const managed={ID:30,NodeID:2,Protocol:'vless',Settings:{reverse:{receiver:true}}}
 assert.equal(inboundUsage(managed),'managed')
 assert.equal(inboundDirectory([{node:{id:2},inbounds:[managed]}],[],undefined,null,'plan').length,0)
 assert.equal(inboundDirectory([{node:{id:2},inbounds:[managed]}],[],undefined,null,'managed').length,1)
})
