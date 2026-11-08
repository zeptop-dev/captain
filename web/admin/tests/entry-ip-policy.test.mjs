import assert from 'node:assert/strict'
import test from 'node:test'
import { defaultEntryIPVersion, entryHostFamily, entryIPPolicyError, canCreateIPVariants, IP_POLICY_EXTRA_KEYS } from '../src/lib/entry-ip-policy.ts'

test('new node defaults follow actual literal family while domains allow dual stack', () => {
  for (const [host, expected] of [['node.example.com', 'dual'], ['192.0.2.10', 'ipv4'], ['2001:db8::20', 'ipv6']]) assert.equal(defaultEntryIPVersion(host), expected)
  assert.equal(entryHostFamily(' 192.0.2.10 '), 'ipv4')
  assert.equal(entryHostFamily('::ffff:192.0.2.10'), 'ipv4')
  assert.equal(entryHostFamily('::ffff:0:0:1'), 'ipv6')
  assert.equal(entryHostFamily('[2001:db8::20]'), 'ipv6')
  assert.equal(entryHostFamily('[::ffff:192.0.2.10]'), 'ipv4')
  assert.equal(defaultEntryIPVersion('[2001:db8::20]'), 'ipv6')
})
test('legacy inherited behavior remains valid even with existing custom client fields', () => {
  assert.equal(entryIPPolicyError('', 'node.example.com', true, '{"ip-version":"ipv4","detour":"front"}'), null)
})
test('explicit policies reject opposite literals, dual literals and client-chain landing selection', () => {
  assert.equal(entryIPPolicyError('ipv6', '192.0.2.10', false, ''), 'familyMismatch')
  assert.equal(entryIPPolicyError('ipv4', '2001:db8::20', false, ''), 'familyMismatch')
  assert.equal(entryIPPolicyError('dual', '192.0.2.10', false, ''), 'dualDomain')
  assert.equal(entryIPPolicyError('ipv6', '2001:db8::20', false, ''), null)
  assert.equal(entryIPPolicyError('dual', 'node.example.com', true, ''), 'chain')
})
test('typed policies cannot be silently replaced by extra endpoint or resolver overrides', () => {
  for (const key of IP_POLICY_EXTRA_KEYS) assert.equal(entryIPPolicyError('dual', 'node.example.com', false, JSON.stringify({ [key]: null })), 'extraConflict', key)
  assert.equal(entryIPPolicyError('dual', 'node.example.com', false, '{"IP-Version":"ipv4"}'), 'extraConflict')
  assert.equal(entryIPPolicyError('dual', 'node.example.com', false, '{" Underlying_Proxy ":"front"}'), 'extraConflict')
  assert.equal(entryIPPolicyError('dual', 'node.example.com', false, '{" Plugin_Opts ":{}}'), 'extraConflict')
  assert.equal(entryIPPolicyError('dual', 'node.example.com', false, '{"tfo":true}'), null)
})
test('variant batches require a domain, direct dialing and no endpoint overrides', () => {
  assert.equal(canCreateIPVariants('node.example.com', false, ''), true)
  assert.equal(canCreateIPVariants('192.0.2.10', false, ''), false)
  assert.equal(canCreateIPVariants('2001:db8::20', false, ''), false)
  assert.equal(canCreateIPVariants('[2001:db8::20]', false, ''), false)
  assert.equal(canCreateIPVariants('', false, ''), false)
  assert.equal(canCreateIPVariants('node.example.com', true, ''), false)
  assert.equal(canCreateIPVariants('node.example.com', false, '{"server":"other.example.com"}'), false)
})

test('WireGuard selects only endpoint literals and leaves domain resolution to clients', () => {
  assert.equal(entryIPPolicyError('ipv4', 'node.example.com', false, '', 'wireguard'), 'wireguardDomain')
  assert.equal(entryIPPolicyError('ipv6', 'node.example.com', false, '', 'wireguard'), 'wireguardDomain')
  assert.equal(entryIPPolicyError('dual', 'node.example.com', false, '', 'wireguard'), null)
  assert.equal(entryIPPolicyError('', 'node.example.com', false, '', 'wireguard'), null)
  assert.equal(entryIPPolicyError('ipv4', '192.0.2.10', false, '', 'wireguard'), null)
  assert.equal(entryIPPolicyError('ipv6', '[2001:db8::20]', false, '', 'wireguard'), null)
  assert.equal(entryIPPolicyError('ipv4', '[2001:db8::20]', false, '', 'wireguard'), 'familyMismatch')
  assert.equal(canCreateIPVariants('node.example.com', false, '', 'wireguard'), false)
})
