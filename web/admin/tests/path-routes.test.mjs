import test from 'node:test'
import assert from 'node:assert/strict'
import { pathInput, routeInput, routePortsComplete, pathStepsComplete, linearSourceLine, pathSourceLineOptions, selectPathSourceLine, movePathItem } from '../src/lib/path-routes.ts'

test('path input keeps fixed references and removes stale fields from another step type', () => {
  const value = pathInput({ name: ' Reused path ', version: 2, steps: [
    { id: 'server', node_id: 3, ingress_id: 8, address_mode: 'internal', uiKey: 'editor' },
    { id: 'nested', path_id: 4, path_revision: 2, node_id: 99, uiKey: 'editor' },
    { id: 'line', line_id: 7, line_version: 6, include_landing: true, node_id: 99 },
  ] })
  assert.deepEqual(value, { name: 'Reused path', version: 2, steps: [
    { id: 'server', node_id: 3, ingress_id: 8, address_mode: 'internal' },
    { id: 'nested', path_id: 4, path_revision: 2 },
    { id: 'line', line_id: 7, line_version: 6, include_landing: true },
  ] })
  assert.equal(pathStepsComplete(value.steps), true)
  assert.equal(pathStepsComplete([{ id: '', path_id: 4, path_revision: 0 }]), false)
  assert.equal(pathStepsComplete(Array(9).fill({ node_id: 1 })), false)
})

const route = () => ({ id: 2, version: 4, name: ' Route ', entry: { id: 'entry', node_id: 1, port: 8443, weight: 9, address_mode: 'internal', status: 'old' }, target_inbound_id: 8, target_address_mode: 'default', strategy: 'failover', branches: [{ id: 'b', path_id: 3, path_revision: 2, weight: 1, ports: [14443, 24443], uiKey: 'editor' }], preview_revision: 'old-token', change_id: 'old-change' })
const path = () => ({ id: 3, version: 2, hops: [{ key: 'one', node_id: 2 }, { key: 'two', node_id: 3 }] })
test('route review payload retains explicit ports and identity but excludes old review metadata', () => {
  const input = route(), projected = routeInput(input)
  assert.equal(projected.name, 'Route')
  assert.equal(projected.entry.address_mode, 'default')
  assert.equal(projected.entry.weight, 1)
  assert.equal('preview_revision' in projected, false)
  assert.equal('change_id' in projected, false)
  assert.equal('uiKey' in projected.branches[0], false)
  const source = JSON.stringify(projected)
  input.branches[0].ports[0] = 14444
  assert.equal(projected.branches[0].ports[0], 14443)
  assert.notEqual(JSON.stringify(routeInput(input)), source)
})

test('ports require the exact fixed path revision and one explicit valid value per expanded hop', () => {
  const value = route()
  assert.equal(routePortsComplete(value.branches, [path()]), true)
  assert.equal(routePortsComplete(value.branches, [undefined]), false)
  assert.equal(routePortsComplete(value.branches, [{ ...path(), version: 3 }]), false)
  assert.equal(routePortsComplete(value.branches, [{ ...path(), id: 9 }]), false)
  for (const ports of [[], [443], [443, 0], [443, 65536], [443, 80.5], [443, 80, 90]]) {
    assert.equal(routePortsComplete([{ ...value.branches[0], ports }], [path()]), false)
  }
})

test('source selection excludes nonlinear, non-TCP, non-relay and managed composite lines', () => {
  const line = { protocol: 'tcp', backend: 'relay', groups: [{ members: [{ node_id: 1 }] }] }
  assert.equal(linearSourceLine(line), true)
  assert.equal(linearSourceLine({ ...line, protocol: 'both' }), false)
  assert.equal(linearSourceLine({ ...line, backend: 'nft' }), false)
  assert.equal(linearSourceLine({ ...line, groups: [{ members: [{}, {}] }] }), false)
  assert.equal(linearSourceLine({ ...line, path_route: true }), false)
})

test('reordering keeps candidate identity and its explicit ports together', () => {
  const items = [{ id: 'one', ports: [123] }, { id: 'two', ports: [456] }]
  assert.deepEqual(movePathItem(items, 0, 1), [items[1], items[0]])
  assert.equal(items[0].id, 'one')
  assert.equal(movePathItem(items, 0, -1), items)
})

test('a pinned source remains visible and unchanged when the live line is no longer eligible', () => {
  const pin = { id: 'saved-step', line_id: 7, line_version: 1, include_landing: true }
  const current = { id: 7, name: 'Source now has candidates', version: 2, protocol: 'tcp', backend: 'relay', groups: [{ members: [{}, {}] }] }
  const other = { id: 8, name: 'Eligible source', version: 3, protocol: 'tcp', backend: 'relay', groups: [{ members: [{}] }] }
  const lines = [current, other], sources = lines.filter(linearSourceLine)
  assert.deepEqual(pathSourceLineOptions(lines, sources, pin, 'Fixed version: 1'), [
    { value: '8', label: 'Eligible source' },
    { value: '7', label: 'Source now has candidates · Fixed version: 1', disabled: true },
  ])
  const draft = { name: 'Renamed template', version: 4, steps: [{ ...pin, ...selectPathSourceLine(pin, 7, sources) }] }
  assert.deepEqual(pathInput(draft).steps, [pin])
  assert.deepEqual(pathSourceLineOptions(lines, sources, { id: '', line_id: 0, line_version: 0 }, ''), [{ value: '8', label: 'Eligible source' }])
  assert.deepEqual(selectPathSourceLine({ id: '', line_id: 0 }, 7, sources), {})
})

test('missing live source uses its fixed identity without becoming selectable in another row', () => {
  const pin = { id: 'saved-step', line_id: 7, line_version: 1 }
  assert.deepEqual(pathSourceLineOptions([], [], pin, 'Fixed version: 1'), [{ value: '7', label: '#7 · Fixed version: 1', disabled: true }])
  assert.deepEqual(selectPathSourceLine(pin, 7, []), {})
  assert.deepEqual(pathSourceLineOptions([], [], { id: '', line_id: 0 }, ''), [])
})

test('reselecting the same eligible source preserves its pin while selecting another source uses its current version', () => {
  const pin = { id: 'saved-step', line_id: 7, line_version: 1 }
  const sources = [{ id: 7, name: 'Updated source', version: 5 }, { id: 8, name: 'Other source', version: 3 }]
  assert.deepEqual({ ...pin, ...selectPathSourceLine(pin, 7, sources) }, pin)
  assert.equal(pathSourceLineOptions(sources, sources, pin, 'Fixed version: 1').filter(option => option.value === '7').length, 1)
  assert.deepEqual(selectPathSourceLine(pin, 8, sources), { line_id: 8, line_version: 3 })
})
