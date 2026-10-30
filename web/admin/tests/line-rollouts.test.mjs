import test from 'node:test'
import assert from 'node:assert/strict'
import { rolloutActionRequest, rolloutReviewKey, rolloutHistoryURL } from '../src/lib/line-rollouts.ts'

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
