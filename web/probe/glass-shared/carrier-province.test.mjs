import assert from 'node:assert/strict'
import test from 'node:test'
import { carrierProvinceCodes, carrierProvinceName, carrierProvinceOptions } from '../../shared/carrier-provinces.ts'
import { networkLabel, networkTaskLabel } from '../src/lib/network.ts'

test('province catalogue has distinct verified codes and names in six languages', () => {
 assert.equal(new Set(carrierProvinceCodes).size, 31)
 for (const language of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
  const options = carrierProvinceOptions(language)
  assert.equal(options.length, 31)
  assert(options.every(p => p.value && p.label))
 }
 assert.equal(carrierProvinceName('he', 'zh-CN'), '河北')
 assert.equal(carrierProvinceName('ha', 'zh-CN'), '河南')
 assert.equal(carrierProvinceName('sx', 'zh-CN'), '山西')
 assert.equal(carrierProvinceName('sn', 'zh-CN'), '陕西')
 assert.equal(carrierProvinceName('bj', 'en'), 'Beijing')
 assert.equal(carrierProvinceName('gd', 'zh-Hant'), '廣東')
 assert.equal(carrierProvinceName('<unknown>', 'en'), '')
})

test('public labels use the exact current TCP target, never invent province from a carrier name', () => {
 const task = {id: 0, name: 'CT', type: 'tcp', family: 'ipv4', series: 'guangdong', province: 'gd'}
 const sample = {task_id: 0, name: 'CT', family: 'ipv4', series: 'guangdong', quality: {type:'tcp_reachability'}}
 assert.equal(networkLabel(task, 'zh-CN'), 'CT · 广东 · IPv4 · TCP')
 assert.equal(networkTaskLabel(sample, [task], 'zh-CN'), 'CT · 广东 · IPv4 · TCP')
 assert.equal(networkTaskLabel({...sample, series:'old-beijing'}, [task], 'zh-CN'), 'CT · IPv4 · TCP')
 assert.equal(networkTaskLabel(sample, [{...task, province:undefined}], 'zh-CN'), 'CT · IPv4 · TCP')
 assert.equal(networkTaskLabel(sample, undefined, 'zh-CN'), 'CT · IPv4 · TCP')
 assert.equal(networkLabel({...task, type:'icmp'}, 'zh-CN'), 'CT · IPv4 · ICMP')
 assert.equal(networkTaskLabel({...sample, task_id: 5}, [{...task, id:5}], 'zh-CN'), 'CT · IPv4 · TCP')
})
