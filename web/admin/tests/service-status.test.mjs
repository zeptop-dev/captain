import assert from 'node:assert/strict'
import test from 'node:test'
import { maintenanceError, maintenanceLocalTime, missingStatusEntryIDs, parseMaintenanceLocalTime } from '../src/lib/service-status.ts'

const row = { entry_id: 1, starts_at: 1792900000, ends_at: 1792903600, note: 'Planned maintenance' }

test('maintenance times are bounded and preserve second precision', () => {
  assert.equal(maintenanceError(row), null)
  assert.equal(maintenanceError({ ...row, ends_at: row.starts_at }), 'time')
  assert.equal(maintenanceError({ ...row, starts_at: 0 }), 'time')
  assert.equal(maintenanceError({ ...row, ends_at: row.starts_at + 30 * 86400 }), null)
  assert.equal(maintenanceError({ ...row, ends_at: row.starts_at + 30 * 86400 + 1 }), 'duration')
  assert.equal(maintenanceError({ ...row, entry_id: 0 }), 'entry')
  assert.equal(parseMaintenanceLocalTime(maintenanceLocalTime(row.starts_at)), row.starts_at)
})

test('maintenance notes count Unicode characters and reject invisible controls', () => {
  assert.equal(maintenanceError({ ...row, note: '🔧'.repeat(512) }), null)
  assert.equal(maintenanceError({ ...row, note: '🔧'.repeat(513) }), 'note')
  assert.equal(maintenanceError({ ...row, note: 'line one\nline two' }), null)
  assert.equal(maintenanceError({ ...row, note: 'line\tcolumn' }), null)
  assert.equal(maintenanceError({ ...row, note: 'hidden\u0000message' }), 'note')
  assert.equal(maintenanceError({ ...row, note: 'hidden\u0085message' }), 'note')
  assert.equal(maintenanceError({ ...row, note: 'line\rreturn' }), 'note')
})

test('missing selections are retained for explicit removal, while disabled nodes remain valid', () => {
  const settings = { revision: 4, public_enabled: false, public_entry_ids: [1, 2, 4], maintenance: [{ ...row, entry_id: 4 }, { ...row, entry_id: 8 }] }
  const entries = [{ ID: 1, Enabled: true }, { ID: 2, Enabled: false }]
  assert.deepEqual(missingStatusEntryIDs(settings, entries), [4, 8])
  assert.deepEqual(settings.public_entry_ids, [1, 2, 4])
  assert.deepEqual(missingStatusEntryIDs(settings, []), [1, 2, 4, 8])
})

test('datetime-local conversion rejects normalized invalid dates and DST gaps', () => {
  const before = process.env.TZ
  process.env.TZ = 'America/Los_Angeles'
  try {
    assert.equal(parseMaintenanceLocalTime('2026-02-30T12:00'), 0)
    assert.equal(parseMaintenanceLocalTime('2026-03-08T02:30'), 0)
    assert.equal(parseMaintenanceLocalTime('2026-10-25T12:34:56Z'), 0)
    assert.equal(parseMaintenanceLocalTime(''), 0)
    const local = '2026-10-25T12:34:56'
    assert.equal(maintenanceLocalTime(parseMaintenanceLocalTime(local)), local)
    // The input formatting itself is read-only even when a repeated local
    // hour represents two different timestamps.
    const first = Date.parse('2026-11-01T08:30:05Z') / 1000
    const second = Date.parse('2026-11-01T09:30:05Z') / 1000
    assert.equal(maintenanceLocalTime(first), '2026-11-01T01:30:05')
    assert.equal(maintenanceLocalTime(second), '2026-11-01T01:30:05')
  } finally {
    if (before === undefined) delete process.env.TZ
    else process.env.TZ = before
  }
})
