import test from 'node:test'
import assert from 'node:assert/strict'
import { forwardingAddress } from '../src/lib/ingress.ts'
import { entryEndpoint } from '../src/lib/entry-flow.ts'
const node={domain:'server.example.com',public_addr:'192.0.2.10',internal_addr:'10.10.0.2'}
const ingress={line_ip:'198.51.100.20',entry_host:'203.0.113.30',entry_domain:'entry.example.com',port_offset:10000,port_from:443,port_to:443}
test('explicit paths distinguish local ports from mapped ports while subscription stays public',()=>{
 assert.equal(forwardingAddress(node,443,ingress),'198.51.100.20:443')
 assert.equal(forwardingAddress(node,443,ingress,'line'),'198.51.100.20:443')
 assert.equal(forwardingAddress(node,443,ingress,'public'),'entry.example.com:10443')
 assert.equal(forwardingAddress(node,443,ingress,'internal'),'10.10.0.2:443')
 assert.deepEqual(entryEndpoint({node,ib:{Port:443},ingress}),{DisplayHost:'entry.example.com',DisplayPort:10443})
})
test('explicit paths never fall back when the selected address or mapping is missing',()=>{
 assert.equal(forwardingAddress(node,443,undefined,'line'),'')
 assert.equal(forwardingAddress(node,443,{...ingress,line_ip:''},'line'),'')
 assert.equal(forwardingAddress(node,444,ingress,'public'),'')
 assert.equal(forwardingAddress(node,443,{...ingress,entry_host:'',entry_domain:''},'public'),'')
 assert.equal(forwardingAddress(node,443,{...ingress,line_ip:'2001:db8::2'},'line'),'[2001:db8::2]:443')
})
