import assert from 'node:assert/strict'
import test from 'node:test'
import { moveStorage, recommendedStorage, storageDraft } from '../src/lib/probe-storage.ts'

test('older settings use automatic selection and never submit stale manual items', () => {
  assert.deepEqual(storageDraft(), { mode: 'auto', items: [] })
  assert.deepEqual(storageDraft({ mode: 'auto', items: [{ mount: '/old', name: 'Archive' }] }), { mode: 'auto', items: [] })
})

test('manual selections preserve missing mounts, empty labels and explicit hidden storage', () => {
  const saved = { mode: 'manual', items: [{ mount: '/missing', name: 'Archive' }, { mount: '/', name: '' }] }
  const draft = storageDraft(saved)
  assert.deepEqual(draft, saved)
  draft.items[0].name = 'Changed'
  assert.equal(saved.items[0].name, 'Archive')
  assert.deepEqual(storageDraft({ mode: 'manual', items: [] }), { mode: 'manual', items: [] })
})

test('switching to manual uses only server recommendations in their order', () => {
  const candidates = [
    { mount: '/', recommended: true },
    { mount: '/srv/disks/a', recommended: false },
    { mount: '/data', recommended: true },
    { mount: '/data', recommended: true },
    { mount: '/boot', recommended: false },
  ]
  assert.deepEqual(recommendedStorage(candidates), [{ mount: '/', name: '' }, { mount: '/data', name: '' }])
})

test('changing homepage priority retains all names and does not mutate the saved selection', () => {
  const items = [{ mount: '/', name: '' }, { mount: '/data', name: 'Data' }, { mount: '/archive', name: 'Archive' }]
  assert.deepEqual(moveStorage(items, 2, 0), [items[2], items[0], items[1]])
  assert.deepEqual(items.map(item => item.mount), ['/', '/data', '/archive'])
  assert.deepEqual(moveStorage(items, 0, -1), items)
  assert.deepEqual(moveStorage(items, 2, 3), items)
})
