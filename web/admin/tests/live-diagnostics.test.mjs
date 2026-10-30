import assert from 'node:assert/strict'
import { test } from 'node:test'
import { LiveSampleWindow, finiteMetric, liveGraphPoints, mergeLiveSamples } from '../src/lib/live-diagnostics.ts'

const sample = (sequence, extra = {}) => ({ epoch: 'boot-a', sequence, sampled_at: new Date(1800000000000 + sequence * 1000).toISOString(), elapsed_ms: 1000, net_up_bytes_per_second: 100, net_down_bytes_per_second: 200, ...extra })
const view = (samples, extra = {}) => ({ epoch: 'boot-a', latest_sequence: samples.at(-1)?.sequence ?? 0, samples, ...extra })

test('live ring deduplicates, rejects unsafe sequences and retains at most 120 observations', () => {
  const ring = mergeLiveSamples([sample(1), sample(2)], view([...Array.from({ length: 125 }, (_, i) => sample(i + 2)), sample(1, { sequence: Number.MAX_SAFE_INTEGER + 1 }), sample(1000, { epoch: 'old-boot' })]))
  assert.equal(ring.length, 120); assert.equal(ring[0].sequence, 7); assert.equal(ring.at(-1).sequence, 126)
  assert.deepEqual(mergeLiveSamples(ring, view([sample(1, { epoch: 'boot-b' })], { epoch: 'boot-b' })).map(s => s.sequence), [1])
})

test('restart with a lower sequence resets the cursor before fetching the new ring', () => {
  const ring = new LiveSampleWindow(); ring.open('lease')
  ring.accept('lease', view([sample(100)])); assert.equal(ring.after, 100)
  assert.equal(ring.accept('lease', view([], { epoch: 'boot-b', latest_sequence: 2 })).samples.length, 0)
  assert.equal(ring.after, 0)
  assert.equal(ring.accept('lease', view([sample(2, { epoch: 'boot-b' })], { epoch: 'boot-b' })).samples[0].sequence, 2)
})

test('late requests and cleanup from the previous viewing lease cannot contaminate a reopened ring', () => {
  const ring = new LiveSampleWindow(); ring.open('old'); ring.accept('old', view([sample(1)]))
  ring.close('old'); ring.open('new'); ring.accept('new', view([sample(3)])); ring.close('old')
  assert.throws(() => ring.accept('old', view([sample(4)])), /closed diagnostics/)
  assert.deepEqual(ring.accept('new', view([sample(5)])).samples.map(s => s.sequence), [3, 5])
})

test('missing sequences, source clock rollback and elapsed sampling gaps break the graph', () => {
  const first = sample(1)
  for (const second of [sample(3), sample(2, { sampled_at: first.sampled_at }), sample(2, { elapsed_ms: 9000 }), sample(2, { sampled_at: new Date(Date.parse(first.sampled_at) + 5000).toISOString() })]) {
    const points = liveGraphPoints([first, second])
    assert.equal(points.length, 3); assert.equal(points[1].up, null); assert.equal(points[1].down, null)
  }
  assert.equal(liveGraphPoints([first, sample(2)]).length, 2)
  for (const invalid of [null, undefined, NaN, Infinity, -1]) assert.equal(finiteMetric(invalid), null)
  assert.equal(finiteMetric(0), 0)
})
test('client history cannot stretch its two-minute window across disconnects or node clock rollback', () => {
  const old = sample(1), recent = sample(500)
  assert.deepEqual(mergeLiveSamples([old], view([recent])).map(s => s.sequence), [500])
  assert.deepEqual(mergeLiveSamples([recent], view([sample(501, { sampled_at: old.sampled_at })])).map(s => s.sequence), [501])
})
