import test from 'node:test'
import assert from 'node:assert/strict'
import { quickLineGroup } from '../src/lib/quick-line.ts'
test('guided lines keep ingress mappings explicit and leave conflict validation to preview', () => {
 const details = [{ node: { id: 1 }, inbounds: [{ Port: 20000 }], ingresses: [{ id: 3, require_ingress: true, port_from: 20000, port_to: 20003, port_offset: 100, reserved_ports: [20001] }] }]
 const group = quickLineGroup(['1', '2'], 'roundrobin', 30000, details)
 assert.equal(group.strategy, 'roundrobin')
 assert.equal(group.members[0].node_id, 1)
 assert.equal(group.members[0].ingress_id, 3)
 assert.equal(group.members[0].port, 20002)
 assert.equal(group.members[1].port, 30000)
 assert.equal(group.members[1].address_mode, 'default')
 assert.equal(details[0].inbounds.length, 1)
 assert.equal(quickLineGroup(['1'], 'failover', 30000, [{ ...details[0], inbounds: [20000, 20002, 20003].map(Port => ({ Port })) }]).members[0].port, 0)
})
