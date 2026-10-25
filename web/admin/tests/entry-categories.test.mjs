import assert from 'node:assert/strict'
import test from 'node:test'
import { matchesEntryCategory, visibleEntrySelection } from '../src/lib/entry-categories.ts'

test('category filtering keeps unclassified entries distinct from all categories', () => {
  const entries = [{ ID: 3, CategoryID: 8, Tags: ['other'] }, { ID: 1 }, { ID: 4, CategoryID: null }, { ID: 2, CategoryID: 6 }]
  assert.deepEqual(entries.filter(e => matchesEntryCategory(e, null)).map(e => e.ID), [3, 1, 4, 2])
  assert.deepEqual(entries.filter(e => matchesEntryCategory(e, '0')).map(e => e.ID), [1, 4])
  assert.deepEqual(entries.filter(e => matchesEntryCategory(e, '8')).map(e => e.ID), [3])
  assert.deepEqual(entries.filter(e => matchesEntryCategory(e, '99')), [])
})

test('bulk assignment excludes hidden and deleted selections and follows visible order', () => {
  assert.deepEqual(visibleEntrySelection([2, 3, 3, 99, 1], [{ ID: 3 }, { ID: 1 }, { ID: 4 }]), [3, 1])
  assert.deepEqual(visibleEntrySelection([2], []), [])
})
