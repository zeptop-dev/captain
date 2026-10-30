import test from 'node:test'
import assert from 'node:assert/strict'
import { evidenceLease, evidenceIsFresh, observeLineChange, lineTargetStatus, lineChangeStatus, lineChangesURL } from '../src/lib/line-change-evidence.ts'
import { readFileSync } from 'node:fs'

const start = { monotonic: 1000, wall: 10000 }
test('a delayed response and the exact expiry boundary never extend confirmation', () => {
  const lease = evidenceLease(start, 2000)
  assert.equal(evidenceIsFresh(lease, { monotonic: 2999, wall: 11999 }, true), true)
  assert.equal(evidenceIsFresh(lease, { monotonic: 3000, wall: 12000 }, true), false)
  assert.equal(evidenceIsFresh(lease, { monotonic: 5000, wall: 14000 }, true), false)
})
test('sleep and wall-clock rollback cannot preserve stale confirmation', () => {
  const lease = evidenceLease(start, 2000)
  assert.equal(evidenceIsFresh(lease, { monotonic: 1001, wall: 20000 }, true), false)
  assert.equal(evidenceIsFresh(lease, { monotonic: 5000, wall: 0 }, true), false)
})
test('offline or refresh errors revoke confirmation even within its original lease', () => {
  assert.equal(evidenceIsFresh(evidenceLease(start, 2000), start, false), false)
})
test('missing, invalid or nonpositive freshness cannot produce a confirmation', () => {
  for (const remaining of [undefined, NaN, Infinity, -1, 0]) {
    assert.equal(evidenceIsFresh(evidenceLease(start, remaining), start, true), false)
  }
})

const target = (node_id, status = 'applied', fresh_for_ms = 2000) => ({ node_id, status, fresh_for_ms, confirmed_at: '2026-10-29T00:00:00Z', action: 'apply', before: [], after: [] })
const change = (targets, extra = {}) => observeLineChange({ id: 'change-one', status: 'applied', targets, ...extra }, start)
test('one expired server downgrades the change, and historical confirmation survives', () => {
  const value = change([target(1), target(2, 'applied', 1000)])
  const now = { monotonic: 2000, wall: 11000 }
  assert.equal(lineChangeStatus(value, start, true), 'applied')
  assert.equal(lineChangeStatus(value, now, true), 'partial')
  assert.equal(lineTargetStatus(value.targets[1], now, true), 'stale')
  assert.equal(value.targets[1].confirmed_at, '2026-10-29T00:00:00Z')
  assert.equal(lineChangeStatus(value, { monotonic: 3000, wall: 12000 }, true), 'pending')
})
test('superseded, changed identity, removed servers and unconfirmed application never count as applied', () => {
  for (const status of ['pending', 'delivered', 'not_applied', 'stale', 'superseded', 'identity_changed', 'removed']) {
    const value = change([target(1, status)])
    assert.equal(lineChangeStatus(value, start, true), status === 'superseded' ? 'superseded' : 'pending')
    assert.equal(lineTargetStatus(value.targets[0], start, true), status)
  }
  assert.equal(lineChangeStatus(change([target(1)], { superseded_at: '2026-10-29T00:01:00Z' }), start, true), 'superseded')
  assert.equal(lineChangeStatus(change([target(1)], { status: 'superseded' }), start, true), 'superseded')
  assert.equal(lineChangeStatus(change([target(1), target(2, 'superseded')]), start, true), 'partial')
})
test('a metadata-only save or a successful delete request is not application evidence', () => {
  assert.equal(lineChangeStatus(change([]), start, true), 'no_runtime_change')
  const deletion = change([{ ...target(1, 'delivered'), action: 'remove' }], { kind: 'delete' })
  assert.equal(lineChangeStatus(deletion, start, true), 'pending')
})
test('list and detail refresh failures independently revoke every current green result', () => {
  const value = change([target(1)])
  assert.equal(lineChangeStatus(value, start, false), 'unknown')
  assert.equal(lineTargetStatus(value.targets[0], start, false), 'unknown')
  assert.equal(lineChangeStatus(value, start, true), 'applied')
})
test('history query keeps deleted line identity and cursor without depending on current line inventory', () => {
  assert.equal(lineChangesURL(), '/api/admin/line-changes?limit=20')
  assert.equal(lineChangesURL(7, 123), '/api/admin/line-changes?limit=20&line_id=7&before=123')
})
test('all six languages cover every change and target status without losing time placeholders', () => {
  const statuses = ['no_runtime_change', 'superseded', 'applied', 'partial', 'pending', 'unknown']
  const targetStatuses = ['pending', 'delivered', 'applied', 'not_applied', 'stale', 'superseded', 'identity_changed', 'removed', 'unknown']
  for (const language of ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko']) {
    const value = JSON.parse(readFileSync(new URL(`../src/i18n/${language}.json`, import.meta.url))).lineChanges
    for (const status of statuses) assert.ok(value.status[status], `${language}: ${status}`)
    for (const status of targetStatuses) assert.ok(value.targetStatus[status], `${language}: ${status}`)
    for (const field of ['created', 'supersededAt', 'observed', 'confirmed', 'issued']) assert.ok(value[field].includes('{{at}}'), `${language}: ${field}`)
    assert.ok(value.version.includes('{{version}}'))
    assert.ok(value.actor.includes('{{actor}}'))
  }
})
