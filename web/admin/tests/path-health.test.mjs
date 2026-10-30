import assert from 'node:assert/strict'
import { test } from 'node:test'
import { pathCandidateEvidence, pathHealthElapsed, pathHealthInput, pathHealthStatus, validPathHealthPolicy } from '../src/lib/path-health.ts'
import { routeInput } from '../src/lib/path-routes.ts'

const candidate = (extra = {}) => ({ id: 'b', tag: 'line', target: '192.0.2.10:443', status: 'up', result: 'success', eligible: true, fresh_for_ms: 1000, observed_at: '2026-10-29T12:00:00Z', epoch: 'process', sequence: 8, rtt_ms: 12, ...extra })
const view = (extra = {}) => ({ route_id: 1, enabled: true, capable: true, status: 'healthy', candidates: [candidate()], ...extra })

test('sleep, clock rollback and delayed responses cannot preserve health', () => {
  const start = { monotonic: 100, wall: 10000 }
  for (const clock of [{ monotonic: 101, wall: 12000 }, { monotonic: 2100, wall: 9000 }]) {
    assert.equal(pathHealthStatus(view(), pathHealthElapsed(start, clock), true), 'unknown')
  }
  assert.equal(pathHealthStatus(view(), pathHealthElapsed(start, start, { monotonic: 1100, wall: 11000 }), true), 'unknown')
})
test('health evidence expires during a slow request and without another response', () => {
  assert.deepEqual(pathCandidateEvidence(candidate(), 999, true), { fresh: true, status: 'up', eligible: true })
  for (const elapsed of [1000, 6000]) assert.deepEqual(pathCandidateEvidence(candidate(), elapsed, true), { fresh: false, status: 'unknown', eligible: false })
  assert.equal(pathHealthStatus(view(), 1000, true), 'unknown')
  assert.equal(pathHealthStatus(view({ status: 'down', candidates: [candidate({ status: 'down' })] }), 1000, true), 'unknown')
})
test('failed requests and authoritative unknown state cannot keep cached green results', () => {
  assert.equal(pathHealthStatus(view(), 0, false), 'unknown')
  assert.equal(pathCandidateEvidence(candidate(), 0, false).eligible, false)
  for (const status of ['unknown', 'paused', 'unsupported']) assert.equal(pathHealthStatus(view({ status }), 0, true), status)
  assert.equal(pathHealthStatus(view({ capable: false }), 0, true), 'unknown')
  for (const patch of [{ observed_at: undefined }, { observed_at: 'invalid' }, { epoch: '' }, { sequence: 0 }, { fresh_for_ms: NaN }]) {
    assert.equal(pathCandidateEvidence(candidate(patch), 0, true).status, 'unknown')
  }
})
test('degraded candidates retain the runtime hysteresis decision until their evidence expires', () => {
  const c = candidate({ status: 'degraded', result: 'failure' })
  assert.equal(pathCandidateEvidence(c, 0, true).eligible, true)
  assert.equal(pathHealthStatus(view({ status: 'degraded', candidates: [c] }), 0, true), 'degraded')
  assert.equal(pathCandidateEvidence({ ...c, eligible: false }, 0, true).eligible, false)
  assert.equal(pathCandidateEvidence(c, 1000, true).eligible, false)
  assert.equal(pathHealthStatus(view({ candidates: [candidate(), candidate({ status: 'down', eligible: false })] }), 0, true), 'degraded')
})
test('route review and staged input retain the explicit policy without sharing mutable form state', () => {
  const health = { ...pathHealthInput(), enabled: true, auto_failover: true, server_name: ' Example.COM ' }
  const route = { id: 1, version: 2, name: 'route', entry: { id: 'a', node_id: 1, port: 443 }, target_inbound_id: 2, target_address_mode: 'default', strategy: 'failover', branches: [], health }
  const output = routeInput(route)
  assert.equal(output.health.server_name, 'example.com')
  health.failure_threshold = 9
  assert.equal(output.health.failure_threshold, 3)
  assert.equal(output.health.auto_failover, true)
  assert.equal('health' in routeInput({ ...route, health: undefined }), false)
  assert.equal(routeInput({ ...route, health: { ...health, enabled: false } }).health.enabled, false)
})
test('automatic selection requires failover and validated integer timing and SNI fields', () => {
  const policy = { ...pathHealthInput(), enabled: true, auto_failover: true }
  assert.equal(validPathHealthPolicy(policy, 'failover'), true)
  assert.equal(validPathHealthPolicy(policy, 'roundrobin'), false)
  assert.equal(validPathHealthPolicy({ ...policy, auto_failover: false }, 'roundrobin'), true)
  for (const patch of [{ interval_seconds: 0 }, { timeout_seconds: 11 }, { recovery_threshold: 1.5 }, { failure_threshold: 11 }, { server_name: 'https://example.com' }, { server_name: '192.0.2.10' }, { server_name: '*.example.com' }]) {
    assert.equal(validPathHealthPolicy({ ...policy, ...patch }, 'failover'), false)
  }
})
