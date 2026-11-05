import assert from 'node:assert/strict'
import test from 'node:test'
import { nodeProbePlanDraft, nodeProbePlanPayload } from '../src/lib/node-probe-plan.ts'
import { carrierProbeDraft } from '../src/lib/carrier-probe.ts'
import { addressObservationFresh, addressFallbackError } from '../src/lib/node-addresses.ts'

const globalConfig = { ...carrierProbeDraft().probe, protocol: 'icmp', family: 'dual', icmp_ipv4: [{ name: 'CT', addr: '192.0.2.10' }] }
const plan = { mode: 'inherit', revision: 'node-revision', custom: null, monitoring_enabled: true, global: { enabled: true, config: globalConfig }, effective: { enabled: true, source: 'inherit', config: globalConfig, changed_at: 0 } }

test('first independent plan copies all global target families without modifying global configuration', () => {
  const draft = nodeProbePlanDraft(plan)
  assert.equal(draft.probe.protocol, 'icmp')
  assert.equal(draft.probe.family, 'dual')
  draft.mode = 'custom'
  draft.texts.icmp_ipv4 = 'CU 198.51.100.20'
  const write = nodeProbePlanPayload(draft)
  assert.equal(write.revision, 'node-revision')
  assert.deepEqual(write.custom.icmp_ipv4, [{ name: 'CU', addr: '198.51.100.20' }])
  assert.deepEqual(globalConfig.icmp_ipv4, [{ name: 'CT', addr: '192.0.2.10' }])
})

test('switching back to inherit preserves saved independent targets and switching off does not create a draft', () => {
  const custom = { ...globalConfig, tcp_province: 'sh', tcp_ipv6: [{ name: 'CU', addr: '[2001:db8::2]:2096' }] }
  const draft = nodeProbePlanDraft({ ...plan, mode: 'custom', custom })
  draft.mode = 'inherit'
  const write = nodeProbePlanPayload(draft)
  assert.equal(write.mode, 'inherit')
  assert.deepEqual(write.custom.tcp_ipv6, custom.tcp_ipv6)
  assert.deepEqual(nodeProbePlanDraft({ ...plan, custom: write.custom }).texts, draft.texts)
  const untouched = nodeProbePlanDraft(plan)
  untouched.mode = 'disabled'
  assert.deepEqual(nodeProbePlanPayload(untouched), { mode: 'disabled', revision: 'node-revision' })
})

test('a newly edited independent draft survives a return to inherit before saving', () => {
  const draft = nodeProbePlanDraft(plan)
  draft.initialized = true
  draft.probe.protocol = 'tcp'
  draft.texts.tcp_ipv4 = 'CT example.com:2096'
  const write = nodeProbePlanPayload(draft)
  assert.equal(write.mode, 'inherit')
  assert.equal(write.custom.protocol, 'tcp')
  assert.deepEqual(write.custom.tcp_ipv4, [{ name: 'CT', addr: 'example.com:2096' }])
})

test('IP observations expire based on reception age, reject future receipts and missing samples', () => {
  assert.equal(addressObservationFresh(1000, 1899), true)
  assert.equal(addressObservationFresh(1000, 1900), false)
  assert.equal(addressObservationFresh(1000, 999), false)
  assert.equal(addressObservationFresh(undefined, 1000), false)
  assert.equal(addressObservationFresh(NaN, 1000), false)
  assert.equal(addressFallbackError('tls'), 'tls')
  assert.equal(addressFallbackError('unexpected response body'), 'invalid_response')
})
