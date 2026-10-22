import assert from 'node:assert/strict'
import test from 'node:test'
import { forwardTargetAddress, forwardingAddress, forwardTargetProtocol } from '../src/lib/ingress.ts'
import { forwardHealth } from '../src/lib/forward-health.ts'

test('IPv6 managed targets use brackets for direct, line and NAT mappings', () => {
 const base = {ib:{Port:1081},node:{domain:'',public_addr:'2001:db8::10'}}
 assert.equal(forwardTargetAddress(base),'[2001:db8::10]:1081')
 const ingress = {line_ip:'2001:db8::20',entry_host:'2001:db8::30',entry_domain:'',port_offset:2000}
 assert.equal(forwardTargetAddress({...base,ingress}),'[2001:db8::20]:1081')
 assert.equal(forwardTargetAddress({...base,ingress:{...ingress,line_ip:''}}),'[2001:db8::30]:3081')
 assert.equal(forwardTargetAddress({...base,ingress:{...ingress,line_ip:'',port_mappings:[{local_from:1080,local_to:1089,public_from:5000}]}}),'[2001:db8::30]:5001')
 assert.equal(forwardTargetAddress({...base,node:{domain:'example.com',public_addr:'2001:db8::10'}}),'example.com:1081')
})
test('UDP protocols select UDP forwarding, manual selection can override afterwards', () => {
 for(const p of ['hysteria2','tuic']) assert.equal(forwardTargetProtocol(p),'udp')
 assert.equal(forwardTargetProtocol('vless'),'both')
})
test('legacy UDP reports never appear as healthy or offline based on TCP', () => {
 for(const up of [true,false]) for(const protocol of ['udp','both']) {
  const h=forwardHealth({up,rtt_ms:0},protocol)
  assert.equal(h.state,'unknown'); assert.equal(h.color,'gray')
 }
 const h=forwardHealth({up:false,rtt_ms:0,health:'down',probe_protocol:'tcp'},'both')
 assert.equal(h.udpUnknown,true);assert.equal(h.transport,'tcp')
 const failed=forwardHealth({up:false,rtt_ms:0,health:'down',probe_protocol:'backend'},'udp')
 assert.equal(failed.color,'red');assert.equal(failed.transport,'backend')
})


test('explicit internal forwarding uses the registered IP and real listener port instead of public mappings', () => {
 const pick = {ib:{Port:1081},node:{domain:'relay.example.com',public_addr:'198.51.100.20',internal_addr:'10.10.0.2'},ingress:{line_ip:'',entry_host:'203.0.113.30',entry_domain:'mapped.example.com',port_offset:2000}}
 assert.equal(forwardTargetAddress(pick), 'mapped.example.com:3081')
 assert.equal(forwardTargetAddress(pick, 'internal'), '10.10.0.2:1081')
 assert.equal(forwardTargetAddress({...pick,ingress:{...pick.ingress,line_ip:'192.0.2.10'}}, 'internal'), '10.10.0.2:1081')
 // The same rule is used for intermediate members, independently of their listener mapping.
 assert.equal(forwardingAddress(pick.node, 8443, pick.ingress, 'internal'), '10.10.0.2:8443')
})

test('internal selection never falls back when the registered address is missing', () => {
 const pick = {ib:{Port:443},node:{domain:'public.example.com',public_addr:'198.51.100.20',internal_addr:''}}
 assert.equal(forwardTargetAddress(pick, 'internal'), '')
 assert.equal(forwardTargetAddress({...pick,node:{...pick.node,internal_addr:'   '}}, 'internal'), '')
 assert.equal(forwardTargetAddress({...pick,node:{...pick.node,internal_addr:'2001:db8::20'}}, 'internal'), '[2001:db8::20]:443')
 assert.equal(forwardTargetAddress(pick, 'default'), 'public.example.com:443')
 assert.equal(forwardTargetAddress({...pick,node:{domain:'',public_addr:'',v6_addr:'2001:db8::20'}}, 'default'), '[2001:db8::20]:443')
})
