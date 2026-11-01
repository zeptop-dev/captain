import test from 'node:test'
import assert from 'node:assert/strict'
import { rolloutActionRequest, rolloutDraftInput, rolloutReviewKey, rolloutHistoryURL } from '../src/lib/line-rollouts.ts'

test('confirmed release actions retain the reviewed identity and CAS version across polling', () => {
  const visible = { id: 'reviewed-plan', version: 4 }
  for (const action of ['pause', 'retry', 'compensate']) {
    const reviewed = rolloutActionRequest(visible, action)
    const confirm = () => reviewed
    visible.version = 5
    visible.id = 'newly-selected-plan'
    assert.deepEqual(confirm(), { path: `/api/admin/line-rollouts/reviewed-plan/${action}`, body: { version: 4 } })
    visible.id = 'reviewed-plan'
    visible.version = 4
  }
})

test('release action identifiers remain one encoded path segment', () => {
  assert.deepEqual(rolloutActionRequest({ id: 'a/b?c', version: 9 }, 'compensate'), {
    path: '/api/admin/line-rollouts/a%2Fb%3Fc/compensate', body: { version: 9 },
  })
})

test('a release preview belongs to the exact operation, draft and timeout', () => {
  const draft = { name: 'Route', protocol: 'tcp', backend: 'relay', target_inbound_id: 1, groups: [{ members: [{ node_id: 2, port: 443 }] }] }
  const initial = rolloutReviewKey(draft, false, 300)
  assert.notEqual(rolloutReviewKey(draft, true, 300), initial)
  assert.notEqual(rolloutReviewKey(draft, false, 60), initial)
  draft.groups[0].members[0].port = 8443
  assert.notEqual(rolloutReviewKey(draft, false, 300), initial)
  assert.equal(rolloutHistoryURL(7, 12), '/api/admin/line-rollouts?limit=20&line_id=7&before=12')
})

test('path releases keep independent scope and reviewed path intent', () => {
  assert.equal(rolloutHistoryURL(7, 12, 'path-rollouts'), '/api/admin/path-rollouts?limit=20&route_id=7&before=12')
  assert.deepEqual(rolloutActionRequest({ id: 'path/a', version: 5 }, 'retry', 'path-rollouts'), {
    path: '/api/admin/path-rollouts/path%2Fa/retry', body: { version: 5 },
  })
  const route = { name: 'Complete path', entry: { node_id: 1, port: 443 }, branches: [{ path_id: 2, path_revision: 3, ports: [8443] }] }
  const key = rolloutReviewKey(route, false, 300)
  route.branches[0].path_revision++
  assert.notEqual(rolloutReviewKey(route, false, 300), key)
})

test('path release preview and save retain backend intent but omit server-derived protocol', () => {
  const path = { id: 7, version: 3, name: 'Route', protocol: 'udp', entry_backend: 'nft', entry: { id: 'entry', node_id: 1, port: 443 }, target_inbound_id: 2, target_address_mode: 'default', strategy: 'failover', branches: [{ id: 'branch', path_id: 2, path_revision: 3, weight: 1, ports: [8443] }] }
  const input = rolloutDraftInput(path, 'path-rollouts')
  assert.equal(input.entry_backend, 'nft')
  assert.equal('protocol' in input, false)
  assert.equal('preview_revision' in rolloutDraftInput({ ...path, preview_revision: 'stale' }, 'path-rollouts'), false)
  path.branches[0].ports[0]++
  assert.equal(input.branches[0].ports[0], 8443)
  assert.notEqual(rolloutReviewKey(input, false, 300), rolloutReviewKey({ ...input, entry_backend: 'relay' }, false, 300))
  const line = { protocol: 'tcp', backend: 'relay' }
  assert.equal(rolloutDraftInput(line, 'line-rollouts'), line)
})
