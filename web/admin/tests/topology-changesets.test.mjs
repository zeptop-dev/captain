import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { changesetFingerprint, changesetInput, changesetValid, inboundLineProtocol, keepSavedInboundSettings, lineTargetProtocol, modelFromDraft, newChangesetOperation, operationIsReferenced, reconcileChangesetSave, selectChangesetResource } from '../src/lib/topology-changesets.ts'

const inbound = () => ({ ...newChangesetOperation('inbound'), key: 'landing', inbound: { node_id: 1, tag: 'landing', listen: '', port: 443, protocol: 'vless', core: '', enabled: true, sort: 0, ingress_id: 0, settings: { transport: { type: 'tcp' } } } })
const saved = model => ({ id: 'draft-id', version: 2, status: 'draft', ...structuredClone(model), operations: model.operations.map(op => op.kind !== 'inbound' || op.id ? structuredClone(op) : { ...structuredClone(op), inbound: { ...structuredClone(op.inbound), settings: undefined, settings_configured: true } }) })
const catalog = () => ({ nodes: [], ingresses: [], inbounds: [{ ...inbound().inbound, id: 7, revision: 'inbound-v1', editable: true, settings: { secret: 'never-send' } }], lines: [], entries: [] })

test('editing an existing inbound sends only supported fields and preserves explicit ingress clear', () => {
  const op = selectChangesetResource({ ...inbound(), editMode: true }, 7, catalog())
  op.inbound.settings = { secret: 'never-send' }
  const result = changesetInput({ name: ' Rename ', operations: [op] }, 4)
  assert.deepEqual(result, { name: 'Rename', version: 4, operations: [{ key: 'landing', kind: 'inbound', id: 7, revision: 'inbound-v1', inbound: { tag: 'landing', listen: '', port: 443, enabled: true, sort: 0, ingress_id: 0 } }] })
  assert.equal(JSON.stringify(result).includes('never-send'), false)
  const updatedCatalog = catalog(); updatedCatalog.inbounds[0].revision = 'inbound-v2'; updatedCatalog.inbounds[0].tag = 'concurrent change'
  assert.equal(selectChangesetResource(op, 7, updatedCatalog), op)
})

test('reopening a configured new inbound never sends fresh settings or generated credentials', () => {
  const stored = saved({ name: 'Saved', operations: [inbound()] })
  const reopened = modelFromDraft(stored)
  reopened.name = 'Renamed'
  reopened.operations[0].inbound.settings = { server_key: 'server-secret' }
  const output = changesetInput(reopened, stored.version)
  assert.equal(output.operations[0].inbound.protocol, 'vless')
  assert.equal('settings' in output.operations[0].inbound, false)
  assert.equal('settings_configured' in output.operations[0].inbound, false)
  assert.equal(JSON.stringify(output).includes('server-secret'), false)
})

test('API-authored partial edits hydrate visible values but retain omitted fields and captured resource revision', () => {
  const c = catalog()
  c.inbounds[0].revision = 'live-newer-revision'
  c.entries = [{ id: 8, name: 'Existing entry', inbound_id: 7, line_id: null, line_member_id: '', display_host: 'edge.example', display_port: 443, rate: 2, sort: 5, enabled: true, tags: ['kept'], region: 'JP', revision: 'entry-live', editable: true }]
  const partial = { id: 'api-draft', version: 4, name: 'API authored', status: 'draft', operations: [
    { key: 'ib', kind: 'inbound', id: 7, revision: 'original-inbound', inbound: { tag: 'Planned tag', ingress_id: null } },
    { key: 'entry', kind: 'entry', id: 8, revision: 'original-entry', entry: { name: 'Planned name', rate: null } },
  ] }
  const model = modelFromDraft(partial, c)
  assert.equal(model.operations[0].inbound.port, 443)
  assert.equal(model.operations[1].entry.rate, 2)
  assert.equal(model.operations[1].entry.display_host, 'edge.example')
  assert.deepEqual(changesetInput(model, 4).operations, [
    { key: 'ib', kind: 'inbound', id: 7, revision: 'original-inbound', inbound: { tag: 'Planned tag' } },
    { key: 'entry', kind: 'entry', id: 8, revision: 'original-entry', entry: { name: 'Planned name' } },
  ])
  model.operations[0].inbound.port = 8443
  model.operations[1].entry.rate = 3
  const edited = changesetInput(model, 4)
  assert.deepEqual(edited.operations[0].inbound, { tag: 'Planned tag', port: 8443 })
  assert.deepEqual(edited.operations[1].entry, { name: 'Planned name', rate: 3 })
  assert.equal(edited.operations[0].revision, 'original-inbound')
  assert.equal(edited.operations[1].revision, 'original-entry')
})

test('late save preserves continued edits, retains server version and strips already saved initial settings', () => {
  const submitted = { name: 'First name', operations: [inbound()] }, current = structuredClone(submitted)
  current.name = 'Later name'; current.operations[0].inbound.port = 8443
  const response = saved(submitted), result = reconcileChangesetSave(current, submitted, response)
  assert.equal(result.model.name, 'Later name')
  assert.equal(result.model.operations[0].inbound.port, 8443)
  assert.deepEqual(result.conflicts, [])
  assert.equal(result.model.operations[0].inbound.settings_configured, true)
  assert.equal(result.model.operations[0].inbound.settings, undefined)
  assert.notEqual(changesetFingerprint(result.model), changesetFingerprint(response))
  assert.equal(changesetInput(result.model, response.version).version, 2)
  assert.deepEqual(reconcileChangesetSave({ name: 'Removed while saving', operations: [] }, submitted, response).model.operations, [])
})

test('late protocol edits are preserved as a conflict until saved settings are explicitly retained', () => {
  const submitted = { name: 'Draft', operations: [inbound()] }, current = structuredClone(submitted)
  current.operations[0].inbound.protocol = 'shadowsocks'
  const response = saved(submitted), merged = reconcileChangesetSave(current, submitted, response)
  assert.deepEqual(merged.conflicts, ['landing'])
  assert.equal(merged.model.operations[0].inbound.protocol, 'shadowsocks')
  const restored = keepSavedInboundSettings(merged.model, response, 'landing')
  assert.equal(restored.operations[0].inbound.protocol, 'vless')
  assert.equal('settings' in changesetInput(restored).operations[0].inbound, false)
})

test('late server edits conflict until the saved server and its ingress are explicitly retained', () => {
  const submitted = { name: 'Draft', operations: [inbound()] }, current = structuredClone(submitted)
  submitted.operations[0].inbound.ingress_id = 10
  current.operations[0].inbound.node_id = 2
  current.operations[0].inbound.ingress_id = 20
  current.operations[0].inbound.port = 8443
  const response = saved(submitted), merged = reconcileChangesetSave(current, submitted, response)
  assert.deepEqual(merged.conflicts, ['landing'])
  assert.equal(merged.model.operations[0].inbound.node_id, 2)
  const restored = keepSavedInboundSettings(merged.model, response, 'landing')
  const output = changesetInput(restored).operations[0].inbound
  assert.equal(output.node_id, 1)
  assert.equal(output.ingress_id, 10)
  assert.equal(output.port, 8443)
  assert.equal('settings' in output, false)
  assert.deepEqual(reconcileChangesetSave(restored, restored, saved(restored)).conflicts, [])
})

test('API-authored null line arrays reopen as an invalid editable draft without crashing', () => {
  for (const groups of [null, [{ strategy: 'failover', members: null }]]) {
    const line = newChangesetOperation('line')
    line.line.name = 'Incomplete line'; line.line.target_inbound_id = 7
    line.line.groups = groups; line.line.endpoints = null
    const draft = saved({ name: 'Incomplete', operations: [line] })
    const model = modelFromDraft(draft)
    assert.doesNotThrow(() => changesetFingerprint(model))
    assert.equal(changesetValid(model), false)
    const output = changesetInput(model).operations[0].line
    assert.deepEqual(output.groups, groups === null ? [] : [{ strategy: 'failover', members: [] }])
    assert.deepEqual(model.operations[0].line.endpoints, [])
    assert.deepEqual(draft.operations[0].line.groups, groups)
  }
})

test('cross-object references and member IDs survive serialization and block dangling operations', () => {
  const ib = inbound(), line = newChangesetOperation('line'), entry = newChangesetOperation('entry')
  line.key = 'relay'; line.inbound_ref = ib.key; line.line.name = 'Relay'; line.line.groups[0].members[0].node_id = 2; line.line.groups[0].members[0].port = 8443
  entry.entry.name = 'Subscription'; entry.entry.line_ref = line.key; entry.entry.line_member_id = line.line.groups[0].members[0].id
  const model = { name: 'Together', operations: [ib, line, entry] }
  assert.equal(changesetValid(model), true)
  assert.equal(operationIsReferenced(model.operations, ib.key), true)
  assert.equal(operationIsReferenced(model.operations, line.key), true)
  const output = changesetInput(model)
  assert.equal(output.operations[1].inbound_ref, 'landing')
  assert.equal(output.operations[2].entry.line_ref, 'relay')
  assert.equal(output.operations[2].entry.line_member_id, output.operations[1].line.groups[0].members[0].id)
  assert.equal(changesetValid({ ...model, operations: model.operations.slice(1) }), false)
  assert.equal(changesetValid({ ...model, operations: [...model.operations, ib] }), false)
  assert.equal(changesetValid({ ...model, operations: Array.from({ length: 33 }, (_, i) => ({ ...ib, key: `item-${i}` })) }), false)
})

test('Shadowsocks transport follows the target and non-TCP candidate groups require roundrobin', () => {
  assert.equal(inboundLineProtocol('shadowsocks'), 'both'); assert.equal(inboundLineProtocol('snell'), 'both')
  const ib = inbound(); ib.inbound.protocol = 'shadowsocks'
  const op = newChangesetOperation('line'); op.inbound_ref = ib.key
  assert.equal(lineTargetProtocol(op, [ib, op], catalog()), 'both')
  op.line.name = 'Relay'; op.line.protocol = 'both'; const member = op.line.groups[0].members[0]; member.node_id = 2; member.port = 8443
  const second = { ...member, id: 'second', node_id: 3 }
  op.line.groups[0].members.push(second)
  assert.equal(changesetValid({ name: 'Public entry group', operations: [ib, op] }), true)
  op.line.groups = [{ strategy: 'failover', members: [member] }, { strategy: 'failover', members: [second, { ...second, id: 'third', node_id: 4 }] }]
  assert.equal(changesetValid({ name: 'Invalid failover', operations: [ib, op] }), false)
  op.line.groups[1].strategy = 'roundrobin'
  assert.equal(changesetValid({ name: 'Weighted', operations: [ib, op] }), true)
  const c = catalog(); c.inbounds[0].line_protocol = 'udp'; op.inbound_ref = undefined; op.line.target_inbound_id = 7
  assert.equal(lineTargetProtocol(op, [], c), 'udp')
  c.inbounds[0].line_protocol = ''
  assert.equal(lineTargetProtocol(op, [], c), undefined)
})

test('preview identity changes for every supported mutation but not response-only fields', () => {
  const model = { name: 'Draft', operations: [inbound()] }, source = changesetFingerprint(model)
  const changed = structuredClone(model); changed.operations[0].inbound.enabled = false
  assert.notEqual(changesetFingerprint(changed), source)
  assert.equal(changesetFingerprint({ ...model, id: 'server', version: 100 }), source)
})

test('the six-language fragment has identical keys and covers the new component labels', () => {
  const locales = JSON.parse(readFileSync(new URL('../src/lib/topology-changesets.locales.json', import.meta.url)))
  const keys = Object.keys(locales.en.topologyChangesets).sort()
  assert.equal(Object.keys(locales).length, 6)
  for (const locale of Object.values(locales)) assert.deepEqual(Object.keys(locale.topologyChangesets).sort(), keys)
  const source = readFileSync(new URL('../src/components/TopologyChangesetsPanel.tsx', import.meta.url), 'utf8')
  for (const [, key] of source.matchAll(/t\('topologyChangesets\.([^']+)'/g)) assert.ok(keys.includes(key), key)
})
