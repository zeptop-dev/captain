import assert from 'node:assert/strict'
import test from 'node:test'
import { nodeEditPayload, nodeEditValues } from '../src/lib/node-edit.ts'

const node = {
  id: 7, name: 'Example server', public_addr: '198.51.100.20', internal_addr: '10.10.0.2',
  v6_addr: '2001:db8::20', domain: 'node.example.com', domain_shared: true,
  monitor_url: 'https://example.com/status', decoy_enabled: true,
  decoy_upstream: 'https://example.com', user_speed_limit_mbps: 25,
  mita_quotas: true, egress_by_ingress: true, dstatus_sid: 'legacy-server',
}

test('node editor keeps the complete replacement payload, including unexposed legacy fields', () => {
  assert.deepEqual(nodeEditValues(node), {
    Name: node.name, PublicAddr: node.public_addr, InternalAddr: node.internal_addr,
    V6Addr: node.v6_addr, Domain: node.domain, DomainShared: true, MonitorURL: node.monitor_url,
    DecoyEnabled: true, DecoyUpstream: node.decoy_upstream, UserSpeedLimitMbps: 25,
    MitaQuotas: true, EgressByIngress: true, DStatusSID: 'legacy-server',
  })
  const old = { ...node }
  for (const key of ['domain_shared', 'decoy_enabled', 'decoy_upstream', 'user_speed_limit_mbps', 'mita_quotas', 'egress_by_ingress', 'dstatus_sid']) delete old[key]
  assert.deepEqual(nodeEditValues(old), {
    ...nodeEditValues(node), DomainShared: false, DecoyEnabled: false, DecoyUpstream: '',
    UserSpeedLimitMbps: 0, MitaQuotas: false, EgressByIngress: false, DStatusSID: '',
  })
})

test('profile changes preserve fresh policy, legacy state and untouched profile fields', () => {
  const baseline = nodeEditValues(node)
  const latest = { ...node, public_addr: '203.0.113.30', decoy_enabled: false, user_speed_limit_mbps: 50, dstatus_sid: 'updated-legacy' }
  const submitted = { ...baseline, Name: 'Renamed server', UserSpeedLimitMbps: 999, DStatusSID: '' }
  assert.deepEqual(nodeEditPayload(latest, submitted, baseline, 'profile'), {
    ...nodeEditValues(latest), Name: submitted.Name,
  })
})

test('policy changes preserve fresh profile and unexposed legacy state', () => {
  const baseline = nodeEditValues(node)
  const latest = { ...node, name: 'Renamed elsewhere', domain: 'new.example.com', mita_quotas: false, dstatus_sid: 'updated-legacy' }
  const submitted = { ...baseline, UserSpeedLimitMbps: 10, Name: 'Stale edit', DStatusSID: '' }
  assert.deepEqual(nodeEditPayload(latest, submitted, baseline, 'policy'), {
    ...nodeEditValues(latest), UserSpeedLimitMbps: 10,
  })
})

test('explicit empty strings, false switches and a zero limit remain intentional changes', () => {
  const baseline = nodeEditValues(node)
  assert.deepEqual(nodeEditPayload(node, { ...baseline, InternalAddr: '', V6Addr: '', Domain: '', DomainShared: false, MonitorURL: '' }, baseline, 'profile'), {
    ...baseline, InternalAddr: '', V6Addr: '', Domain: '', DomainShared: false, MonitorURL: '',
  })
  assert.deepEqual(nodeEditPayload(node, { ...baseline, DecoyEnabled: false, DecoyUpstream: '', UserSpeedLimitMbps: 0, MitaQuotas: false, EgressByIngress: false }, baseline, 'policy'), {
    ...baseline, DecoyEnabled: false, DecoyUpstream: '', UserSpeedLimitMbps: 0, MitaQuotas: false, EgressByIngress: false,
  })
})

test('unchanged editors preserve the latest node and intentional same-field edits win', () => {
  const baseline = Object.freeze(nodeEditValues(node))
  const latest = Object.freeze({ ...node, name: 'Changed remotely', user_speed_limit_mbps: 99 })
  for (const section of ['profile', 'policy']) assert.deepEqual(nodeEditPayload(latest, baseline, baseline, section), nodeEditValues(latest))
  const submitted = Object.freeze({ ...baseline, Name: 'Changed here' })
  assert.deepEqual(nodeEditPayload(latest, submitted, baseline, 'profile'), { ...nodeEditValues(latest), Name: submitted.Name })
  assert.equal(latest.name, 'Changed remotely')
  assert.equal(baseline.Name, node.name)
})
