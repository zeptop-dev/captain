import { test } from 'node:test'
import assert from 'node:assert/strict'
import { currentEntries, fetchServiceStatus, headlineKey, snapshotExpired, statusCategories, summarize } from '../src/lib/service-status.ts'

const now = 1_800_000_000
const entry = (extra = {}) => ({ id: 1, name: 'Example node', rate: 1, region: '', category: null, state: 'healthy', reason: 'healthy', evidence: ['inbound'], observed_at: now, end_to_end: false, maintenance: null, ...extra })
const snapshot = (entries, extra = {}) => ({ generated_at: now, refresh_seconds: 60, entries, summary: {}, ...extra })

test('expired snapshots and stale observations never preserve operational or failed status', () => {
  const data = snapshot([entry(), entry({ id: 2, state: 'down', reason: 'component_failure' })])
  assert.equal(snapshotExpired(data, now + 179), false)
  assert.equal(snapshotExpired(data, now + 180), true)
  assert.deepEqual(currentEntries(data, now + 180).map(e => [e.state, e.reason]), [['unknown', 'stale'], ['unknown', 'stale']])
  const freshReport = snapshot([entry({ observed_at: now - 180 })])
  assert.equal(currentEntries(freshReport, now)[0].state, 'unknown')
})

test('maintenance uses its explicit window without requiring a recent component observation', () => {
  const data = snapshot([entry({ state: 'maintenance', reason: 'maintenance', observed_at: now - 1000, maintenance: { starts_at: now - 100, ends_at: now + 20, note: 'Scheduled work' } })])
  assert.equal(currentEntries(data, now)[0].state, 'maintenance')
  assert.equal(currentEntries(data, now + 20)[0].state, 'unknown')
})

test('future, unsupported, and missing evidence safely degrade to unknown', () => {
  assert.equal(currentEntries(snapshot([entry()], { generated_at: now + 61 }), now)[0].reason, 'stale')
  assert.equal(currentEntries(snapshot([entry({ observed_at: now + 61 })]), now)[0].reason, 'stale')
  assert.equal(currentEntries(snapshot([entry({ observed_at: null })]), now)[0].reason, 'no_data')
  assert.equal(currentEntries(snapshot([entry({ state: 'new-state' })]), now)[0].reason, 'unsupported')
  assert.equal(currentEntries(snapshot([entry({ reason: 'raw-server-error' })]), now)[0].reason, 'unsupported')
})

test('counts are derived from current visible entries, not the possibly stale server summary', () => {
  const entries = currentEntries(snapshot([entry(), entry({ id: 2, state: 'degraded', reason: 'partial_failure' }), entry({ id: 3, observed_at: now - 200 })], { summary: { healthy: 999 } }), now)
  assert.deepEqual(summarize(entries), { healthy: 1, degraded: 1, down: 0, maintenance: 0, unknown: 1, total: 3 })
  assert.equal(headlineKey(summarize(entries)), 'issues')
  assert.equal(headlineKey(summarize([])), 'emptyTitle')
})

test('categories come only from returned entries and preserve their category order', () => {
  const category = (id, sort) => ({ id, name: `Category ${id}`, description: '', sort })
  const result = statusCategories([entry({ category: category(8, 2) }), entry({ id: 2, category: category(4, 1) }), entry({ id: 3, category: category(8, 2) }), entry({ id: 4 })])
  assert.deepEqual(result.map(c => c.id), [4, 8])
})

test('failed or malformed API payloads reject rather than produce an empty healthy view', async () => {
  const original = globalThis.fetch
  try {
    globalThis.fetch = async () => new Response('{}', { status: 404 })
    await assert.rejects(fetchServiceStatus('/example', new AbortController().signal), error => error.status === 404)
    globalThis.fetch = async () => new Response(JSON.stringify({ generated_at: now, entries: null }))
    await assert.rejects(fetchServiceStatus('/example', new AbortController().signal))
    globalThis.fetch = async () => new Response(JSON.stringify(snapshot([entry()])))
    assert.equal((await fetchServiceStatus('/example', new AbortController().signal)).entries.length, 1)
  } finally { globalThis.fetch = original }
})
