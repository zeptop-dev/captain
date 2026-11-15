import assert from 'node:assert/strict'
import test from 'node:test'
import { mergeStandaloneProtocolJSON, standaloneProtocolJSON, standaloneConnection, standaloneKey } from '../src/lib/standalone.ts'
import { isSubscriberInbound } from '../src/lib/entry-flow.ts'
import { groupSelection } from '../src/lib/node-access.ts'

test('standalone listeners are excluded from subscription and plan choices',()=>{
 const ib={Protocol:'socks',Settings:{standalone:{username:'operator',password:'secret'}}}
 assert.equal(isSubscriberInbound(ib),false)
 assert.deepEqual(groupSelection({inbounds:[{inbound_id:1,editable:false,policy:'none'}]},[1]),[])
 assert.equal(isSubscriberInbound({...ib,Settings:{}}),true)
})
test('copy uses the mapped ingress endpoint and only the independent credentials',()=>{
 const ib={Protocol:'socks',Port:1080,Settings:{standalone:{username:'operator',password:'secret'}}}
 const choice={ib,node:{domain:'server.example.com'},ingress:{entry_host:'198.51.100.20',entry_domain:'entry.example.com',port_mappings:[{local_from:1080,local_to:1080,public_from:20000}]}}
 assert.deepEqual(JSON.parse(standaloneConnection(choice)),{type:'socks5',server:'entry.example.com',port:20000,username:'operator',password:'secret'})
 choice.ingress.port_mappings=[]
 choice.ingress.port_from=2000;choice.ingress.port_to=2001
 assert.equal(standaloneConnection({...choice,node:{}}),undefined)
})
test('SS2022 copy uses the single key, with required cipher-specific key lengths',()=>{
 for(const [method,size] of [['2022-blake3-aes-128-gcm',16],['2022-blake3-aes-256-gcm',32],['2022-blake3-chacha20-poly1305',32]]) {
  const key=standaloneKey(method)
  assert.equal(Buffer.from(key,'base64').length,size)
  const ib={Protocol:'shadowsocks',Port:8388,Settings:{standalone:{},cipher:method,server_key:key}}
  assert.deepEqual(JSON.parse(standaloneConnection({ib,node:{domain:'server.example.com'}})),{type:'shadowsocks',server:'server.example.com',port:8388,method,password:key})
 }
})

test('advanced JSON never displays credentials and protocol edits cannot replace them',()=>{
 const current=JSON.stringify({standalone:{username:'operator',password:'secret'},server_key:'private-key',cipher:'2022-blake3-aes-128-gcm'})
 assert.equal(standaloneProtocolJSON(current).includes('secret'),false)
 assert.equal(standaloneProtocolJSON(current).includes('private-key'),false)
 const merged=JSON.parse(mergeStandaloneProtocolJSON(current,JSON.stringify({cipher:'2022-blake3-aes-256-gcm',standalone:{username:'injected'},server_key:'replacement'})))
 assert.deepEqual(merged.standalone,{username:'operator',password:'secret'})
 assert.equal(merged.server_key,'private-key')
 assert.throws(()=>mergeStandaloneProtocolJSON(current,'[]'))
})
