import { test } from 'node:test'
import assert from 'node:assert/strict'
import { formatMetricCount, latestStoragePoint, storageHistoryURL } from '../src/lib/storage-history.ts'

test('fractional count summaries preserve one decimal without exposing floating point tails', () => {
  assert.deepEqual([13.6666666666, 179.8333333333, 26.5, 180, 0, null, NaN, Infinity].map(formatMetricCount), ['13.7', '179.8', '26.5', '180', '0', '—', '—', '—'])
})

test('storage history URLs keep exact selected periods and reject path injection', () => {
  assert.equal(storageHistoryURL('12', { range: '4h' }), '/api/probe/nodes/12/storage-history?range=4h')
  assert.equal(storageHistoryURL('12', { from: 1800000000.9, to: 1800003600.9 }), '/api/probe/nodes/12/storage-history?from=1800000000&to=1800003600')
  assert.throws(() => storageHistoryURL('../1', {}))
})

test('chart summary only borrows the previous bucket and keeps used/total paired', () => {
  const valid = { ts: 180, used: 5, total: 100 }
  assert.deepEqual(latestStoragePoint([valid, { ts: 240, used: null, total: null }], 250, 60), valid)
  assert.equal(latestStoragePoint([valid, { ts: 240, used: null, total: null }, { ts: 300, used: null, total: null }], 310, 60), undefined)
  assert.equal(latestStoragePoint([{ ts: 180, used: 5, total: null }, { ts: 240, used: null, total: 100 }], 250, 60), undefined)
  assert.equal(latestStoragePoint([{ ts: 300, used: 5, total: 100 }], 250, 60), undefined)
})
