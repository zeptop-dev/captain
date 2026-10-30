import assert from 'node:assert/strict'
import { test } from 'node:test'
import { acceptedBatchBytes, historyQuality, sourceInterval, trafficHistoryURL, validHistoryTime } from '../src/lib/traffic-history.ts'
test('unknown and invalid batch metadata cannot become a complete observed interval', () => {
  assert.equal(historyQuality({ metadata_status: 'legacy' }), 'legacy')
  assert.equal(historyQuality({ metadata_status: 'invalid' }), 'invalid')
  assert.equal(historyQuality({ metadata_status: 'valid', sample: { version: 2, quality: 'observed' } }), 'unknown')
  assert.equal(historyQuality({ metadata_status: 'valid', sample: { version: 1, quality: 'partial' } }), 'partial')
})
test('source intervals never borrow collection or reception time for missing boundaries', () => {
  const source = { started_at: '2026-10-29T01:00:00Z', ended_at: '2026-10-29T01:02:00Z' }
  assert.deepEqual(sourceInterval(source), [source.started_at, source.ended_at])
  for (const value of [undefined, '', 'invalid', '0001-01-01T00:00:00Z', '2026-10-29T02:00:00Z']) assert.equal(sourceInterval({ ...source, started_at: value }), null)
  assert.equal(validHistoryTime('invalid'), false)
})
test('paging uses the server cursor and preserves the fixed per-page bound', () => {
  assert.equal(trafficHistoryURL(7), '/api/admin/nodes/7/traffic-history?limit=50')
  assert.equal(trafficHistoryURL(7, 90), '/api/admin/nodes/7/traffic-history?limit=50&before=90')
  assert.equal(trafficHistoryURL(7, -1), trafficHistoryURL(7))
})
test('unknown aggregate totals cannot be displayed as measured zero', () => {
  assert.equal(acceptedBatchBytes({ totals_known: false, up_bytes: 0, down_bytes: 0 }), null)
  assert.equal(acceptedBatchBytes({ totals_known: true, up_bytes: Infinity, down_bytes: 0 }), null)
  assert.deepEqual(acceptedBatchBytes({ totals_known: true, up_bytes: 0, down_bytes: 123 }), { up: 0, down: 123 })
})
