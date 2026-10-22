import assert from 'node:assert/strict'
import test from 'node:test'
import { entryEndpoint, changeEntryInbound, canDialThrough, lineSupportsGroups, freshLineStatus } from '../src/lib/entry-flow.ts'

const choice = (port, host = 'one.example.com', ingress) => ({ ib: { Port: port, Settings: {} }, node: { domain: host, public_addr: '' }, ingress })

test('changing an inbound updates defaults but preserves independent manual overrides', () => {
  const old = choice(443), next = choice(8443, 'two.example.com')
  assert.deepEqual(changeEntryInbound(entryEndpoint(old), old, next), entryEndpoint(next))
  assert.deepEqual(changeEntryInbound({ DisplayHost: 'relay.example.com', DisplayPort: 443 }, old, next), { DisplayHost: 'relay.example.com', DisplayPort: 8443 })
  assert.deepEqual(changeEntryInbound({ DisplayHost: 'one.example.com', DisplayPort: 9443 }, old, next), { DisplayHost: 'two.example.com', DisplayPort: 9443 })
})

test('published ports use explicit mappings rather than a legacy offset', () => {
  const ingress = { entry_host: '198.51.100.20', entry_domain: '', port_offset: 100, port_mappings: [{ local_from: 8000, local_to: 8009, public_from: 20000 }] }
  assert.deepEqual(entryEndpoint(choice(8003, '', ingress)), { DisplayHost: '198.51.100.20', DisplayPort: 20003 })
  assert.equal(entryEndpoint(choice(443, '', ingress)).DisplayPort, 0)
})

test('client chains reject cycles, missing predecessors and more than eight entries', () => {
  const entries = Array.from({ length: 8 }, (_, i) => ({ ID: i + 1, ClientDialerEntryID: i ? i : null }))
  assert.equal(canDialThrough(entries, 7), true)
  assert.equal(canDialThrough(entries, 8), false)
  assert.equal(canDialThrough(entries, 3, 2), false)
  assert.equal(canDialThrough(entries, 99), false)
  assert.equal(canDialThrough([{ ID: 1, ClientDialerEntryID: 2 }, { ID: 2, ClientDialerEntryID: 1 }], 1), false)
})

test('multiple entry endpoints do not imply backend load balancing', () => {
  const groups = [{ members: [{}, {}] }, { members: [{}] }]
  assert.equal(lineSupportsGroups('nft', groups), true)
  groups[1].members.push({})
  assert.equal(lineSupportsGroups('realm', groups), false)
  assert.equal(lineSupportsGroups('relay', groups), true)
})

test('missing, future and stale observations never appear healthy', () => {
  const now = Date.parse('2026-10-21T12:00:00Z')
  for (const at of [undefined, 'invalid', '2026-10-21T12:01:00Z', '2026-10-21T11:56:59Z']) assert.equal(freshLineStatus(at, now), false)
  assert.equal(freshLineStatus('2026-10-21T11:59:00Z', now), true)
})


test('a registered internal address does not change a subscription endpoint', () => {
 const target = choice(443)
 target.node.internal_addr = '10.10.0.2'
 assert.deepEqual(entryEndpoint(target), { DisplayHost: 'one.example.com', DisplayPort: 443 })
})
