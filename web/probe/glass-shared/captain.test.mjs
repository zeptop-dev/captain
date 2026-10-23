import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { snapshot, clientFor, statusFor, captainCall, clearHistoryCache, pingMetrics, periodTraffic, captainCPUSpec, captainStorage } from './captain.ts'
import { cpuSpec } from '../src/lib/cpu-spec.ts'
import { storageItems, storageCapacity, storageBytes, storagePercent } from '../src/lib/storage.ts'
import { summarizePings } from './ping-summary.ts'
import { selectPings, selectedHistory } from './ping-selection.ts'
const now = Math.floor(Date.now() / 60000) * 60
const node = {id:1,name:'Example',online:true,last_seen:new Date(now*1000).toISOString(),info:{},host:{valid:{cpu:true,memory:true,disk:true,network:true,load:true,swap:true,connections:true,processes:true},mem_total:1024},traffic:{used:100,limit:1000,mode:'sum'},recent:[]}
beforeEach(() => { snapshot.value = { nodes:[structuredClone(node)], now }; clearHistoryCache() })
test('public storage follows selection and disk visibility, without reading private resources', () => {
 const n = snapshot.value.nodes[0]
 n.host.disk_total = 1024
 n.host.disk_used = 0
 Object.defineProperty(n.host, 'resources', { get() { throw Error('private resources must not be read') } })
 assert.deepEqual(captainStorage('1', key => key).map(v => [v.id, v.used, v.total, v.percent]), [['legacy-root', 0, 1024, 0]])
 n.storage = []
 assert.deepEqual(captainStorage('1', key => key), [])
 n.storage = [
  {id:'pool-opaque', name:'Archive', kind:'pool', used:1024, total:4096},
  {id:'system-opaque', name:'', kind:'system', used:null, total:1024},
  {id:'volume-opaque', name:'', kind:'volume', used:0, total:null},
 ]
 const items = captainStorage('1', key => key)
 assert.deepEqual(items.map(v => [v.label, v.percent]), [['Archive',25],['storage.system',null],['storage.data 2',null]])
 assert.equal(storageCapacity(items[1]), '— / 1K')
 assert.equal(storageCapacity(items[2]), '0B / —')
 n.host.disk_used = 999 // Explicit projection is independent of the legacy root summary.
 assert.deepEqual(captainStorage('1', key => key), items)
 snapshot.value.public_sections = ['cpu','system']
 assert.deepEqual(captainStorage('1', key => key), [])
 assert.deepEqual(captainStorage('999', key => key), [])
})
test('storage values preserve unknowns and format each volume without summing overlapping capacity', () => {
 const volumes = storageItems({storage:[
  {id:'a',name:'',kind:'pool',used:0,total:1024},
  {id:'b',name:'',kind:'pool',used:0,total:1024},
  {id:'c',name:'',kind:'volume',used:NaN,total:Infinity},
  {id:'d',name:'',kind:'volume',used:-1,total:0},
 ]}, key => key)
 assert.deepEqual(volumes.map(v => v.label), ['storage.data 1','storage.data 2','storage.data 3','storage.data 4'])
 assert.deepEqual(volumes.map(v => [v.used,v.total,v.percent]), [[0,1024,0],[0,1024,0],[null,null,null],[null,0,null]])
 assert.deepEqual(volumes.map(v => storageCapacity(v)), ['0B / 1K','0B / 1K','— / —','— / 0B'])
 assert.equal(storageBytes(1.1 * 1024**4),'1.1T')
 assert.equal(storageBytes(463 * 1024**3),'463G')
 assert.equal(storageCapacity({used:8.17 * 1024**2,total:14.32 * 1024**4},true),'8M/14.3T')
 assert.deepEqual(volumes.map(v => v.detailLabel), ['storage.dataDisk 1','storage.dataDisk 2','storage.dataDisk 3','storage.dataDisk 4'])
 assert.equal(storageItems({storage:[{id:'only',name:'',kind:'pool',used:1,total:2}]}, key => key)[0].label, 'storage.disk')
 assert.equal(storageCapacity({used:463 * 1024**3,total:463 * 1024**3},true),'463/463G')
 assert.equal(storageCapacity({used:1023 * 1024**3,total:1023 * 1024**3},true),'1/1T')
 assert.equal(storagePercent(0),'0.0%')
 assert.equal(storagePercent(null),'—')
 assert.equal(storageItems({host:{valid:{disk:false},disk_used:5,disk_total:10}}, key => key)[0].percent,null)
 assert.equal(storageItems({host:null}, key => key)[0].total,null)
})
test('CPU specifications distinguish whole-machine topology from virtual CPUs', () => {
 const bare={cpu_cores:16,cpu_physical_cores:8,cpu_sockets:1,virtualization_role:'none'}
 assert.equal(cpuSpec(bare),'8C / 16T')
 assert.equal(cpuSpec({...bare,cpu_cores:64,cpu_physical_cores:32,cpu_sockets:2}),'2S · 32C / 64T')
 assert.equal(cpuSpec({...bare,virtualization_role:'host',virt:'kvm'}),'8C / 16T')
 assert.equal(cpuSpec({...bare,cpu_cores:4,virtualization_role:'guest'}),'4 vCPU')
 assert.equal(cpuSpec({...bare,cpu_cores:4,cpu_physical_cores:-1,cpu_sockets:Infinity,virtualization_role:'guest'}),'4 vCPU')
 // No SMT, or asymmetric/hybrid thread counts: never assume two threads/core.
 assert.equal(cpuSpec({...bare,cpu_cores:8}),'8C / 8T')
 assert.equal(cpuSpec({...bare,cpu_cores:12}),'8C / 12T')
 // Legacy virtualization names cannot distinguish a KVM host from a guest.
 assert.equal(cpuSpec({cpu_cores:4,virt:'kvm'},'zh-CN'),'4 逻辑 CPU')
 for (const change of [{cpu_sockets:undefined},{cpu_sockets:9},{cpu_physical_cores:17},{cpu_physical_cores:1.5},{virtualization_role:''},{virtualization_role:'unknown'}]) {
  assert.equal(cpuSpec({...bare,...change}), '16 logical CPU')
 }
 for (const cpu_cores of [undefined,0,-1,NaN,Infinity,1.5,'4',Number.MAX_SAFE_INTEGER+1]) assert.equal(cpuSpec({...bare,cpu_cores}), '—')
 assert.equal(cpuSpec(), '—')
 assert.equal(cpuSpec(null), '—')
 for (const locale of ['en','zh-CN','zh-TW','ja','ko','ru']) assert(!cpuSpec({cpu_cores:4},locale).includes('undefined'))
})
test('CPU card specs refresh with snapshots and follow system visibility without changing detail metrics', () => {
 const n=snapshot.value.nodes[0]
 n.host={...n.host,info:{cpu_cores:4,virtualization_role:'guest'},cpu_percent:12.4,load1:.11,load5:.22,load15:.33}
 assert.equal(captainCPUSpec('1'),'4 vCPU')
 snapshot.value={...snapshot.value,nodes:[{...n,host:{...n.host,info:{cpu_cores:2,virtualization_role:'guest'}}}]}
 assert.equal(captainCPUSpec('1'),'2 vCPU')
 snapshot.value.nodes=[n]
 n.host.info={cpu_cores:64,cpu_physical_cores:32,cpu_sockets:2,virtualization_role:'none'}
 assert.equal(captainCPUSpec('1'),'2S · 32C / 64T')
 assert.equal(clientFor(n,0).cpu_physical_cores,undefined)
 const status=statusFor(n)
 assert.deepEqual([status.cpu,status.load,status.load5,status.load15],[12.4,.11,.22,.33])
 snapshot.value.public_sections=['cpu']
 assert.equal(captainCPUSpec('1'),'')
 assert.equal(statusFor(n).cpu,12.4)
 snapshot.value.public_sections=['system']
 assert.equal(captainCPUSpec('1'),'2S · 32C / 64T')
 assert(Number.isNaN(statusFor(n).cpu))
 n.host.info=undefined
 assert.equal(captainCPUSpec('1'),'—')
})
test('omitted valid zeros differ from invalid and absent samples', () => {
 assert.equal(statusFor(node).cpu,0)
 assert(Number.isNaN(statusFor({...node,host:null}).cpu))
 assert(Number.isNaN(statusFor({...node,host:{valid:{cpu:false}}}).cpu))
 assert(Number.isNaN(clientFor(node,0).price))
 snapshot.value.public_sections=[]
 assert(Number.isNaN(statusFor(node).cpu));assert(Number.isNaN(clientFor(node,0).traffic_limit))
})
test('twenty time buckets retain gaps and weight both attempts and successful latency', () => {
 const points=[{ts:now-3600+1,n:100,lost:20,avg_ms:10},{ts:now-3600+2,n:10,lost:0,avg_ms:100},{ts:now-1,n:10,lost:10,avg_ms:0}]
 const s=summarizePings({to:now,points},1)
 assert.equal(s.history.length,20);assert.equal(s.avgLoss,25)
 assert.equal(s.avgLatency,20);assert.equal(s.history[0].latency,20)
 assert.equal(s.history[1].latency,null);assert.equal(s.history[1].loss,null)
 assert.equal(s.history[19].latency,null);assert.equal(s.history[19].loss,100)
 assert(Number.isNaN(summarizePings({to:now,points:[points[2]]},1).avgLatency))
})
test('carrier names keep separate series, loss is a ratio, missing samples stay null', async () => {
 globalThis.fetch=async()=>Response.json({from:now-3600,to:now,step:60,points:[{task_id:0,name:'A',ts:now-60,n:10,lost:2,avg_ms:20},{task_id:0,name:'B',ts:now-60,n:20,lost:0,avg_ms:80}]})
 const {metrics,stats}=await pingMetrics('1',1)
 assert.equal(new Set(metrics.series.map(s=>s.tags.task_id)).size,2)
 assert.equal(metrics.series.find(s=>s.metric_key==='ping.loss').points.find(p=>p.count>0).value,.2)
 assert.equal(metrics.series[0].points[0].value,null)
 assert.equal(stats.stats[0].loss,20);assert.equal(stats.stats[0].valid,8)
 assert.equal(stats.stats[0].p99,undefined)
})
test('mixed resource and ping queries preserve both metric families', async () => {
 const paths=[]
 globalThis.fetch=async(path)=>{paths.push(path);return Response.json({from:now-3600,to:now,step:60,points:path.includes('/pings')?[{task_id:1,name:'Example',ts:now-60,n:10,lost:0,avg_ms:20}]:[{ts:now-60,n:6,cpu:0,valid:{cpu:true}}]})}
 const r=await captainCall('public:queryMetrics',{entity_id:'1',hours:1,metric_keys:['cpu.usage','ping.latency_ms']})
 assert(r.series.some(s=>s.metric_key==='cpu.usage' && s.points.some(p=>p.value===0)))
 assert(r.series.some(s=>s.metric_key==='ping.latency_ms'))
 assert.equal(paths.length,2)
})
test('hidden history, unknown nodes and administrative RPC never reach the network', async () => {
 let calls=0;globalThis.fetch=async()=>{calls++;throw Error('unexpected fetch')}
 snapshot.value.public_sections=[]
 await assert.rejects(()=>captainCall('public:queryMetrics',{entity_id:'1',hours:1,metric_keys:['cpu.usage']}))
 await assert.rejects(()=>pingMetrics('999',1))
 await assert.rejects(()=>captainCall('admin:runCommand',{command:'example'}))
 assert.equal(calls,0)
})
test('custom time ranges use the same bounds for ping charts and summary statistics', async () => {
 globalThis.fetch=async()=>Response.json({from:now-3600,to:now,step:60,points:[
  {task_id:1,name:'Example',ts:now-600,n:100,lost:50,avg_ms:200},
  {task_id:1,name:'Example',ts:now-300,n:10,lost:2,avg_ms:20},
  {task_id:1,name:'Example',ts:now-60,n:100,lost:0,avg_ms:100},
 ]})
 const params={entity_id:'1',start:new Date((now-360)*1000).toISOString(),end:new Date((now-240)*1000).toISOString(),metric_keys:['ping.latency_ms']}
 const stats=await captainCall('public:getPingMetricStats',params)
 const metrics=await captainCall('public:queryMetrics',params)
 assert.equal(stats.stats[0].total,10);assert.equal(stats.stats[0].avg,20);assert.equal(stats.stats[0].loss,20)
 assert.equal(stats.start,params.start);assert.equal(stats.end,params.end)
 assert.deepEqual(metrics.series[0].points.filter(p=>p.value!==null).map(p=>p.value),[20])
 const aliases=await captainCall('public:getPingMetricStats',{entity_id:'1',start_time:params.start,end_time:params.end})
 assert.deepEqual(aliases,stats)
})
test('three carriers are selected from configuration independently of samples', () => {
 const node={ping_tasks:[{id:0,name:'CT'},{id:0,name:'CU'},{id:0,name:'CM'},{id:1,name:'Other'}]}
 const selection=selectPings(node,true)
 assert.equal(selection.threeNetwork,true)
 assert.deepEqual(selection.carriers.map(c=>c.source.name),['CU','CT','CM'])
 const h={to:now,points:[{task_id:0,name:'CT',ts:now-60,n:10,lost:2,avg_ms:50},{task_id:0,name:'CU',ts:now-60,n:10,lost:0,avg_ms:150},{task_id:1,name:'CT',ts:now-60,n:10,lost:0,avg_ms:999}]}
 const summary=summarizePings(selectedHistory(h,selection.carriers[1].source),1)
 assert.equal(summary.avgLatency,50);assert.equal(summary.avgLoss,20)
 assert.equal(summarizePings(selectedHistory(h,selection.carriers[2].source),1).hasData,false)
 assert.equal(selectPings(node,false).threeNetwork,false)
 assert.equal(selectPings(node,false).first.id,1)
})
test('first task remains stable when its samples are missing, stale or reordered', () => {
 const node={ping_tasks:[{id:8,name:'Second'},{id:2,name:'First'}],host:{pings:[{task_id:1,name:'Deleted'},{task_id:8,name:'Second'}]}}
 const source=selectPings(node,false).first
 assert.equal(source.id,2)
 assert.deepEqual(selectedHistory({points:[{task_id:8,name:'Second'},{task_id:1,name:'Deleted'}]},source).points,[])
 assert.equal(selectPings({...node,ping_tasks:[]},false).first,null)
 assert.equal(selectPings({ping_tasks:[{id:0,name:'Custom'}]},true).threeNetwork,false)
 assert.equal(selectPings({ping_tasks:[{id:0,name:'Custom'}]},true).first.name,'Custom')
})

test('period totals survive OS counter resets, offline nodes and unavailable live metrics', () => {
 const before={...node,traffic:{...node.traffic,used_up:300,used_down:500,used:800},host:{...node.host,net_total_up:1000300,net_total_down:2000500,net_up:7,net_down:11}}
 const restarted={...before,host:{...before.host,net_total_up:10,net_total_down:20,net_up:1,net_down:2}}
 for(const n of [before,restarted,{...restarted,online:false,host:null},{...restarted,host:{valid:{network:false}}}]) {
  assert.equal(statusFor(n).net_total_up,300);assert.equal(statusFor(n).net_total_down,500)
  assert.equal(statusFor(n).traffic_up,300);assert.equal(statusFor(n).traffic_down,500)
  assert.equal(periodTraffic(n).used,800)
 }
 assert.equal(statusFor(before).net_out,7);assert.equal(statusFor(restarted).net_out,1)
 assert.equal(statusFor(before).net_in,11);assert.equal(statusFor(restarted).net_in,2)
 const reset={...before,traffic:{...before.traffic,used:0,used_up:0,used_down:0}}
 assert.equal(statusFor(reset).net_total_up,0);assert.equal(statusFor(reset).net_total_down,0)
 for(const [mode,used] of Object.entries({sum:800,up:300,down:500,max:500})) {
  const n={...before,traffic:{...before.traffic,mode,used}}
  assert.equal(periodTraffic(n).used,used)
  assert.equal(statusFor(n).net_total_up+statusFor(n).net_total_down,800)
 }
})
test('period counters and live rates obey independent visibility, missing counters never use OS totals', () => {
 const n={...node,traffic:{...node.traffic,used_up:0,used_down:500},host:{...node.host,net_total_up:999,net_total_down:999,net_up:7,net_down:11}}
 snapshot.value.public_sections=['traffic']
 assert.equal(statusFor(n).net_total_up,0);assert.equal(statusFor(n).net_total_down,500)
 assert(Number.isNaN(statusFor(n).net_out))
 snapshot.value.public_sections=['network']
 assert(Number.isNaN(statusFor(n).net_total_up));assert(Number.isNaN(statusFor(n).net_total_down))
 assert(Number.isNaN(periodTraffic(n).used));assert.equal(statusFor(n).net_out,7)
 snapshot.value.public_sections=['network','traffic']
 assert(Number.isNaN(statusFor({...n,traffic:{used:100}}).net_total_up))
 for(const value of [undefined,null,-1,Infinity,'123']) {
  assert(Number.isNaN(statusFor({...n,traffic:{...n.traffic,used_up:value}}).net_total_up))
 }
})

test('structured prices preserve decimal years, free and hidden meanings', async () => {
 const { nodePrice } = await import('../src/lib/node-price.ts')
 assert.equal(nodePrice({price:'legacy text'},'zh-CN'),'legacy text')
 const billing={amount:'36.3600',currency:'USD',cycle:'custom',years:'1.5'}
 assert.equal(nodePrice({billing},'zh-CN'),'USD 36.3600 / 1.5 年')
 assert.equal(nodePrice({billing:{...billing,amount:'0'}},'zh-CN'),'免费')
 assert.equal(nodePrice({price:'old text',billing:{...billing,amount:''}},'zh-CN'),'')
 assert.equal(nodePrice({billing:{...billing,cycle:'annual'}},'en-US'),'USD 36.3600 / year')
})

test('bandwidth metadata uses decimal bit rates and respects publication', async () => {
 const { nodeBandwidth } = await import('../src/lib/node-bandwidth.ts')
 const { captainBandwidth } = await import('./captain.ts')
 const b = { inbound_mbps: 1500, outbound_mbps: 100.5, public: true }
 assert.equal(nodeBandwidth(b), 'RX 1.5 Gbps · TX 100.5 Mbps')
 assert.equal(nodeBandwidth({...b,outbound_mbps:1500}), '1.5 Gbps')
 assert.equal(nodeBandwidth({...b,inbound_mbps:0}), 'RX — · TX 100.5 Mbps')
 assert.equal(nodeBandwidth({...b,public:false}), '')
 assert.equal(nodeBandwidth({...b,inbound_mbps:0,outbound_mbps:0}), '')
 snapshot.value.nodes[0].info.bandwidth = b
 assert.equal(captainBandwidth('1','en'), nodeBandwidth(b))
 snapshot.value.public_sections=['network']
 assert.equal(captainBandwidth('1','en'), '')
})
