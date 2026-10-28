import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import {
  addressType, registeredAddresses, dnsFamily, dnsFamilyStatus, dnsSnapshotStale,
  dnsStatusColor, dnsCheckWait, nodeDNSTarget, nodeDNSLink, syncHasChanges, syncResultStatus,
} from '../src/lib/dns-health.ts'

const node = { id: 7, domain: 'node.example.com', public_addr: '198.51.100.20', v6_addr: '2001:db8::20' }
const target = { key: 'node', host: node.domain, mode: 'direct', expected: [node.public_addr, node.v6_addr], status: 'ok', answers: [] }
const health = { node_id: 7, node: 'Example', checked_at: 1000, stale: false, targets: [target], failure_count: 0 }

test('server address rows preserve both families and handle legacy IPv6 PublicAddr', () => {
  assert.deepEqual(registeredAddresses(node), { A: [node.public_addr], AAAA: [node.v6_addr] })
  assert.deepEqual(registeredAddresses({ public_addr: node.v6_addr, v6_addr: node.v6_addr }), { A: [], AAAA: [node.v6_addr] })
  assert.deepEqual(registeredAddresses({ public_addr: '2001:db8::30', v6_addr: node.v6_addr }), { A: [], AAAA: ['2001:db8::30', node.v6_addr] })
  assert.deepEqual(registeredAddresses({ public_addr: '2001:db8::20', v6_addr: '2001:0db8::20' }), { A: [], AAAA: ['2001:db8::20'] })
  assert.equal(addressType('::ffff:192.0.2.10'), 'A')
  for (const value of ['', 'node.example.com', '999.0.0.1', 'not:an:ip']) assert.equal(addressType(value), undefined)
})

test('legacy aggregate snapshots cannot fabricate independent A or AAAA health', () => {
  for (const type of ['A', 'AAAA']) {
    const family = dnsFamily(target, type)
    assert.equal(family.status, 'unknown')
    assert.deepEqual(family.answers, [])
    assert.deepEqual(family.expected, [type === 'A' ? node.public_addr : node.v6_addr])
  }
})

test('a failed family does not change the other family, and shared DNS means resolved only', () => {
  const value = { ...target, status: 'missing', families: [
    { type: 'A', status: 'ok', expected: [node.public_addr], answers: [] },
    { type: 'AAAA', status: 'missing', expected: [node.v6_addr], answers: [] },
  ] }
  assert.equal(dnsFamilyStatus(dnsFamily(value, 'A'), false, 'direct'), 'ok')
  assert.equal(dnsFamilyStatus(dnsFamily(value, 'AAAA'), false, 'direct'), 'missing')
  assert.equal(dnsFamilyStatus(dnsFamily(value, 'A'), false, 'shared'), 'resolved')
  assert.equal(dnsFamilyStatus({ status: 'unconfigured' }, false, 'direct'), 'unconfigured')
  assert.equal(dnsFamilyStatus({ status: 'unconfigured' }, false, 'shared'), 'no_record')
  assert.equal(dnsFamilyStatus({ status: 'unknown', answers: [{ source: 'cloudflare', error: 'timeout' }] }, false, 'direct'), 'query_failed')
  assert.equal(dnsStatusColor('query_failed'), 'orange')
  assert.equal(dnsStatusColor('unconfigured'), 'gray')
  assert.equal(dnsStatusColor('unexpected'), 'orange')
  assert.equal(dnsStatusColor('missing'), 'red')
})

test('expired, missing and future evidence never stays green while DNS polling is unavailable', () => {
  assert.equal(dnsSnapshotStale(health, 1000), false)
  assert.equal(dnsSnapshotStale(health, 1901), true)
  assert.equal(dnsSnapshotStale(health, 999), true)
  assert.equal(dnsSnapshotStale({ ...health, checked_at: 0 }, 1000), true)
  assert.equal(dnsSnapshotStale({ ...health, stale: true }, 1000), true)
  assert.equal(dnsFamilyStatus({ status: 'ok' }, true, 'direct'), 'stale')
  assert.equal(dnsFamilyStatus({ status: 'mismatch', propagation_pending: true }, false, 'direct'), 'propagating')
  assert.equal(dnsFamilyStatus({ status: 'ok', propagation_pending: true }, false, 'direct'), 'ok')
})

test('info badges reject cached results for changed domain, addresses, ownership or server', () => {
  assert.equal(nodeDNSTarget(health, node), target)
  assert.equal(nodeDNSTarget(health, { ...node, domain: 'NODE.EXAMPLE.COM.' }), target)
  assert.equal(nodeDNSTarget(health, { ...node, v6_addr: '2001:0db8:0000:0000:0000:0000:0000:0020' }), target)
  assert.equal(nodeDNSTarget(health, { ...node, public_addr: '::ffff:198.51.100.20' }), target)
  for (const changed of [{ id: 8 }, { domain: '' }, { domain: 'other.example.com' }, { public_addr: '203.0.113.30' }, { v6_addr: '' }, { domain_shared: true }, { domain_conflict: true }]) {
    assert.equal(nodeDNSTarget(health, { ...node, ...changed }), undefined)
  }
  assert.equal(nodeDNSLink(7, 'AAAA'), '/nodes/7/diagnostics?view=health#dns-7-node-aaaa')
})

test('recheck cooldown matches accepted observation time and handles a clock jump', () => {
  assert.equal(dnsCheckWait(1000, 1000), 30)
  assert.equal(dnsCheckWait(1000, 1015.1), 15)
  assert.equal(dnsCheckWait(1000, 1030), 0)
  assert.equal(dnsCheckWait(1000, 999), 0)
})

test('info badges reject obsolete ownership modes and honor shared-domain precedence', () => {
  const snapshot = mode => ({ ...health, targets: [{ ...target, mode, expected: mode === 'shared' || mode === 'resolve_only' ? [] : target.expected }] })
  for (const mode of ['shared', 'conflict', 'resolve_only']) assert.equal(nodeDNSTarget(snapshot(mode), node), undefined)
  const shared = snapshot('shared')
  assert.equal(nodeDNSTarget(shared, { ...node, domain_shared: true, domain_conflict: true }), shared.targets[0])
  // A different server sharing this domain may have enabled shared mode.
  assert.equal(nodeDNSTarget(shared, { ...node, domain_conflict: true }), shared.targets[0])
  const conflict = snapshot('conflict')
  assert.equal(nodeDNSTarget(conflict, { ...node, domain_conflict: true }), conflict.targets[0])
  assert.equal(nodeDNSTarget(conflict, { ...node, domain_shared: true, domain_conflict: true }), undefined)
  const resolveOnly = snapshot('resolve_only')
  assert.equal(nodeDNSTarget(resolveOnly, { ...node, public_addr: '', v6_addr: '' }), resolveOnly.targets[0])
  assert.equal(nodeDNSTarget(health, { ...node, public_addr: '', v6_addr: '' }), undefined)
})

test('sync apply requires an enabled versioned plan containing an actual write', () => {
  const plan = { enabled: true, revision: 'config-and-record-revision', records: [{ action: 'created' }] }
  assert.equal(syncHasChanges(plan), true)
  for (const changed of [{ enabled: false }, { revision: '' }, { records: [] }, { records: [{ action: 'unchanged' }, { action: 'skipped' }] }]) assert.equal(syncHasChanges({ ...plan, ...changed }), false)
})

test('an attempted create/update is not a successful write when its outcome is unknown or rejected', () => {
  assert.equal(syncResultStatus({ action: 'updated', outcome: 'unknown', error: 'Request interrupted' }), 'unknown')
  assert.equal(syncResultStatus({ action: 'created', outcome: 'failed' }), 'failed')
  assert.equal(syncResultStatus({ action: 'updated', error: 'Rejected' }), 'failed')
  assert.equal(syncResultStatus({ action: 'updated', outcome: 'success' }), 'success')
  assert.equal(syncResultStatus({ action: 'unchanged' }), 'success')
  assert.equal(syncResultStatus({ action: 'skipped', reason: 'shared_domain' }), 'skipped')
  assert.equal(syncResultStatus({ action: 'unrecognized' }), 'unknown')
})

test('DNS sync and family status copy is complete in all six languages', async () => {
  for (const locale of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const dict = JSON.parse(await readFile(new URL(`../src/i18n/${locale}.json`, import.meta.url), 'utf8'))
    const keys = ['nodeAddresses.unregistered', 'dnsHealth.recheck', 'dnsHealth.badgeHint', 'dnsHealth.scopeHint', 'dnsHealth.unexpectedHint',
      ...['resolved', 'unconfigured', 'unexpected', 'propagating', 'stale', 'query_failed', 'no_record'].map(key => `dnsHealth.statuses.${key}`),
      ...['title', 'hint', 'loading', 'loadError', 'record', 'before', 'expected', 'action', 'nothing', 'changed', 'requestUnknown', 'result', 'resultHint', 'reload', 'apply', 'close'].map(key => `dnsHealth.sync.${key}`),
      ...['no_domain', 'invalid_domain', 'no_address', 'shared_domain', 'domain_address_conflict', 'unmanaged_domain', 'auto_dns_disabled', 'missing_token', 'external_records', 'provider_unavailable', 'invalid_address', 'unknown'].map(key => `dnsHealth.sync.reasons.${key}`)]
    for (const key of keys) assert.equal(typeof key.split('.').reduce((value, part) => value?.[part], dict), 'string', `${locale}: ${key}`)
  }
})
