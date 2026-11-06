import assert from 'node:assert/strict'
import test from 'node:test'
import { fallbackFollowState, addressFallbackFresh } from '../src/lib/node-addresses.ts'

const fallback = { address: '198.51.100.20', source: 'captain_cloudflare_trace', origin: 'https://example.com:2096', sampled_at: 1120, received_at: 1130, run_id: 'a'.repeat(32), check_status: 'no_conflict' }
const candidate = { ...fallback, candidate_since: 1010, candidate_sampled_at: 1000, last_sampled_at: 1120, last_success: 1130, confirmed_at: 1130 }
const input = { enabled: true, fresh: true, primary: false, fallback, candidate, entry: fallback.address }

test('fallback following needs permission and current evidence even after confirmation', () => {
  assert.equal(fallbackFollowState(input), 'following')
  assert.equal(fallbackFollowState({ ...input, enabled: false }), 'disabled')
  assert.equal(fallbackFollowState({ ...input, fresh: false }), 'unavailable')
  assert.equal(fallbackFollowState({ ...input, fallback: { ...fallback, error: 'timeout' } }), 'unavailable')
  assert.equal(fallbackFollowState({ ...input, fallback: undefined }), 'unavailable')
  assert.equal(fallbackFollowState({ ...input, primary: true }), 'primary')
})

test('conflicts and legacy or missing proof cannot inherit a confirmed candidate', () => {
  assert.equal(fallbackFollowState({ ...input, fallback: { ...fallback, check_status: 'conflict' } }), 'conflict')
  for (const patch of [{ check_status: undefined }, { run_id: undefined }, { run_id: 'invalid' }, { check_status: 'future-status' }]) {
    assert.equal(fallbackFollowState({ ...input, fallback: { ...fallback, ...patch } }), 'legacy')
  }
})

test('confirmation is bound to exact address, origin, source, run and sample identities', () => {
  for (const patch of [
    { address: '203.0.113.30' }, { source: 'different' }, { origin: 'https://other.example.com:2096' },
    { confirmed_at: 1131 }, { confirmed_at: 1009 }, { run_id: 'b'.repeat(32) }, { last_sampled_at: 1119 }, { last_success: 1129 }, { candidate_since: 0 },
  ]) assert.equal(fallbackFollowState({ ...input, candidate: { ...candidate, ...patch } }), 'waiting')
  assert.equal(fallbackFollowState({ ...input, candidate: undefined }), 'waiting')
  assert.equal(fallbackFollowState({ ...input, candidate: {} }), 'waiting')
})

test('matching observations distinguish confirming, applying and current entry state', () => {
  assert.equal(fallbackFollowState({ ...input, candidate: { ...candidate, confirmed_at: 0 } }), 'confirming')
  assert.equal(fallbackFollowState({ ...input, entry: '192.0.2.10' }), 'applying')
  assert.equal(fallbackFollowState(input), 'following')
})


test('a delayed fallback report cannot renew the lifetime of its sampled address', () => {
  assert.equal(addressFallbackFresh(fallback, 1130), true)
  assert.equal(addressFallbackFresh({ ...fallback, sampled_at: 230 }, 1130), false)
  assert.equal(addressFallbackFresh({ ...fallback, sampled_at: 231 }, 1130), true)
  assert.equal(addressFallbackFresh({ ...fallback, sampled_at: 1190 }, 1130), true)
  assert.equal(addressFallbackFresh({ ...fallback, sampled_at: 1191 }, 1130), false)
  assert.equal(addressFallbackFresh({ ...fallback, sampled_at: NaN }, 1130), false)
  assert.equal(addressFallbackFresh({ ...fallback, received_at: 1131 }, 1130), false)
})
