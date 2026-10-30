import assert from 'node:assert/strict'
import test from 'node:test'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'
const source = process.env.SETTINGS_SOURCE_ROOT
const moduleURL = source ? pathToFileURL(resolve(source, 'src/lib/node-service-control.ts')) : new URL('../src/lib/node-service-control.ts', import.meta.url)
const { policyDraftError, policyWrite, manualDraftError, manualWrite, controlStatusColor, controlCapabilityKey } = await import(moduleURL.href)
const state = { capability: { supported: true }, traffic: { monitoring_enabled: true, limit_bytes: 1000, fresh: true }, automatic: { active: false } }
const policy = { action: 'limit', threshold_percent: 95, limit_mbps: 10, auto_resume: true }

test('automatic restriction requires a usable server, monitoring and a saved allowance', () => {
  assert.equal(policyDraftError(policy, state), null)
  assert.equal(policyDraftError(policy, { ...state, capability: { supported: false } }), 'unavailable')
  assert.equal(policyDraftError(policy, { ...state, traffic: { ...state.traffic, monitoring_enabled: false } }), 'monitoringRequired')
  assert.equal(policyDraftError(policy, { ...state, traffic: { ...state.traffic, limit_bytes: 0 } }), 'quotaRequired')
  assert.equal(policyDraftError(policy, { ...state, traffic: { ...state.traffic, fresh: false } }), 'staleTraffic')
  assert.equal(policyDraftError({ ...policy, action: 'alert' }, { ...state, capability: { supported: false } }), null, 'An old or unhealthy agent must not prevent disabling automatic enforcement')
})
test('empty and fractional control limits never become an unintended restriction', () => {
  for (const threshold_percent of ['', 0, 101, 1.2, 'invalid']) assert.equal(policyDraftError({ ...policy, threshold_percent }, state), 'thresholdInvalid')
  for (const limit_mbps of ['', 0, -1, 1.2, 1_000_001, Infinity]) assert.equal(policyDraftError({ ...policy, limit_mbps }, state), 'rateInvalid')
  assert.equal(policyDraftError({ ...policy, action: 'pause', limit_mbps: 0 }, state), null)
})
test('policy writes carry CAS and only their own section', () => {
  assert.deepEqual(policyWrite('config-1', policy), { revision: 'config-1', policy })
  const payload = policyWrite('config-2', { ...policy, action: 'pause' })
  assert.equal(payload.policy.limit_mbps, 0)
  assert.equal('manual' in payload, false)
  assert.equal('exempt_current_period' in payload, false)
  assert.equal('LimitBytes' in payload, false, 'Control saves cannot overwrite probe or accounting drafts')
})
test('restoring while automatic enforcement is active needs an explicit exemption', () => {
  const draft = { mode: 'normal', limit_mbps: 10, exempt_current_period: false }
  assert.equal(manualDraftError(draft, state), null)
  const active = { ...state, automatic: { active: true } }
  assert.equal(manualDraftError(draft, active), 'exemptionRequired')
  assert.equal(manualDraftError({ ...draft, exempt_current_period: true }, active), null)
  assert.equal(manualDraftError(draft, { ...state, capability: { supported: false } }), null, 'Recovery remains available after losing capability')
})
test('manual pause and limit never modify the policy or silently exempt it', () => {
  assert.deepEqual(manualWrite('config-1', { mode: 'pause', limit_mbps: 10, exempt_current_period: true }), { revision: 'config-1', manual: { mode: 'pause', limit_mbps: 0 } })
  assert.deepEqual(manualWrite('config-2', { mode: 'normal', limit_mbps: 10, exempt_current_period: true }), { revision: 'config-2', manual: { mode: 'normal', limit_mbps: 0 }, exempt_current_period: true })
  assert.equal(manualDraftError({ mode: 'pause' }, { ...state, capability: { supported: false } }), 'unavailable')
  assert.equal(manualDraftError({ mode: 'limit', limit_mbps: 0 }, state), 'rateInvalid')
})
test('only a confirmed execution report is green; new capability reasons do not imply support', () => {
  assert.equal(controlStatusColor('applied'), 'green')
  for (const status of ['unconfigured', 'pending', 'error', 'unknown', 'unsupported']) assert.notEqual(controlStatusColor(status), 'green')
  assert.equal(controlCapabilityKey('upgrade_required'), 'upgradeRequired')
  assert.equal(controlCapabilityKey('monitor_only'), 'monitorOnly')
  assert.equal(controlCapabilityKey('unpaired'), 'unpaired')
  assert.equal(controlCapabilityKey('new_reason_from_newer_agent'), 'unavailable')
})

test('offline protection is explicit, capability gated and cleared when alert-only is selected', () => {
  assert.equal('offline_enabled' in policyWrite('legacy', policy), false)
  assert.equal(policyDraftError({ ...policy, offline_enabled: true }, state), 'offlineUpgradeRequired')
  assert.equal(policyDraftError({ ...policy, offline_enabled: true }, { ...state, capability: { supported: true, offline_supported: true } }), null)
  assert.equal(policyWrite('new', { ...policy, offline_enabled: true }).offline_enabled, true)
  assert.equal(policyWrite('disabled', { ...policy, action: 'alert', offline_enabled: true }).offline_enabled, false)
  assert.equal(manualDraftError({ mode: 'normal', exempt_current_period: false }, { ...state, runtime: { offline: { automatic: true } } }), 'exemptionRequired')
})
