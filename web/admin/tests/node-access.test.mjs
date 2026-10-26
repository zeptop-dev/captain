import assert from 'node:assert/strict'
import test from 'node:test'
import { accessDraft, accessChanged, changedAccessPayload, validAccess, sharedEntries, groupInboundIDs, groupSelection, accessMatches, preserveExistingGroup } from '../src/lib/node-access.ts'
const row = (id, policy = 'groups', group_ids = [1], extras = {}) => ({ inbound_id: id, node_id: 1, node_name: 'Server', tag: `in-${id}`, protocol: 'vless', port: 443, policy, group_ids, legacy: false, editable: true, revision: 'r1', entries: [{ id: 10, name: 'First' }, { id: 11, name: 'Second' }], plan_ids: [1], ...extras })
const directory = { revision: 'r1', inbounds: [row(1, 'all', []), row(2), row(3, 'groups', [2]), row(4, 'none', []), row(5, 'all', [], { editable: false })], groups: [{ id: 1, name: 'A' }, { id: 2, name: 'B' }], plans: [] }
test('unchanged access is omitted, including reordered group choices', () => {
 const draft = accessDraft(row(1, 'groups', [1, 2])); draft.group_ids.reverse()
 assert.equal(accessChanged(draft), false)
 assert.equal(changedAccessPayload(draft, 1), undefined)
})
test('a draft cannot cross a target-inbound switch, including line targets', () => {
 const draft = { ...accessDraft(row(1)), policy: 'none' }
 assert.equal(changedAccessPayload(draft, 2), undefined)
 assert.deepEqual(changedAccessPayload(draft, 1), { inbound_id: 1, revision: 'r1', policy: 'none', group_ids: [], confirm_shared: false })
})
test('all and none are distinct despite both having empty groups', () => {
 const draft = { ...accessDraft(row(1, 'all', [])), policy: 'none' }
 assert.equal(accessChanged(draft), true)
 assert.equal(changedAccessPayload(draft, 1).policy, 'none')
})
test('revision, receiver and deleted group guards fail closed', () => {
 assert.equal(validAccess(accessDraft(row(1)), directory), true)
 for (const candidate of [row(1, 'groups', []), row(1, 'groups', [99]), row(1, 'groups', [1], { revision: 'old' }), row(1, 'groups', [1], { editable: false })]) assert.equal(validAccess(accessDraft(candidate), directory), false)
})
test('shared confirmation scope includes disabled nodes but excludes only the current Entry', () => {
 assert.deepEqual(sharedEntries(row(1), 10).map(e => e.id), [11])
 assert.deepEqual(sharedEntries(row(1)).map(e => e.id), [10, 11])
 assert.deepEqual(sharedEntries(row(1, 'groups', [1], { entries: [{ id: 10, name: 'Only' }] }), 10), [])
})
test('plan preview includes all and selected group, excludes none and internal receivers', () => {
 assert.deepEqual(groupInboundIDs(directory, 1), [1, 2])
 assert.deepEqual(groupInboundIDs(directory, 0), [1])
})
test('group save retains all inbounds and only editable known selected ids', () => {
 assert.deepEqual(groupSelection(directory, [4, 5, 999]), [1, 4])
 assert.deepEqual(groupSelection(directory, []), [1])
})
test('access filters distinguish exact scope from availability of a group', () => {
 assert.equal(accessMatches(row(1, 'all', []), 'groups'), false)
 assert.equal(accessMatches(row(1, 'all', []), 'group:2'), true)
 assert.equal(accessMatches(row(1, 'none', []), 'group:2'), false)
 assert.equal(accessMatches(undefined, 'none'), false)
 assert.equal(accessMatches(undefined, null), true)
})
test('editing an existing reverse/WG connection never sends legacy group ownership', () => {
 const existing = { id: 'connection', group_id: 1, port: 443 }
 assert.deepEqual(preserveExistingGroup(existing), { id: 'connection', port: 443 })
 assert.deepEqual(existing, { id: 'connection', group_id: 1, port: 443 })
 assert.deepEqual(preserveExistingGroup({ ...existing, id: '' }), { id: '', group_id: 1, port: 443 })
})
