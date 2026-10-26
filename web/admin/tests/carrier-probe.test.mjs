import assert from 'node:assert/strict'
import test from 'node:test'
import { carrierProbeDraft, carrierProbePayload, carrierTargetPlaceholder } from '../src/lib/carrier-probe.ts'

test('protocol and family changes preserve four independent custom target lists through save and reload', () => {
 const draft = carrierProbeDraft(undefined, [{ name: 'CT', addr: 'example.com:80' }])
 draft.texts.tcp_ipv6 = 'CU [2001:db8::1]:443'
 draft.texts.icmp_ipv4 = 'CT 192.0.2.10'
 draft.texts.icmp_ipv6 = 'CT 2001:db8::2'
 draft.probe.protocol = 'icmp'
 draft.probe.family = 'dual'
 const saved = carrierProbePayload(draft.probe, draft.texts)
 const restored = carrierProbeDraft(saved)
 assert.deepEqual(restored.texts, draft.texts)
 assert.equal(restored.probe.protocol, draft.probe.protocol)
 assert.equal(restored.probe.family, draft.probe.family)
 assert.equal(saved.tcp_ipv4[0].addr, 'example.com:80')
 assert.equal(saved.icmp_ipv6[0].addr, '2001:db8::2')
 restored.probe.protocol = 'tcp'
 assert.deepEqual(carrierProbePayload(restored.probe, restored.texts).icmp_ipv4, saved.icmp_ipv4)
})

test('empty explicit target lists use new defaults instead of reviving legacy custom targets', () => {
 const draft = carrierProbeDraft({ tcp_ipv4: [] }, [{ name: 'CT', addr: 'example.com:80' }])
 assert.equal(draft.texts.tcp_ipv4, '')
 assert.deepEqual(carrierProbePayload(draft.probe, draft.texts).tcp_ipv4, [])
 for (const protocol of ['tcp', 'icmp']) for (const family of ['ipv4', 'ipv6']) {
  const placeholder = carrierTargetPlaceholder(protocol, family)
  assert.equal(placeholder.split('\n').length, 3)
  assert(placeholder.includes(`${family}.pek-4134.endpoint.nxtrace.org`))
  assert(placeholder.includes(`${family}.pek-4837.endpoint.nxtrace.org`))
  assert(placeholder.includes(`${family}.pek-9808.endpoint.nxtrace.org`))
  assert.equal(placeholder.includes(':80'), protocol === 'tcp')
 }
})
