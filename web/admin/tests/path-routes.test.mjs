import test from 'node:test'
import assert from 'node:assert/strict'
import { pathInput, routeInput, routePortsComplete, pathStepsComplete, linearSourceLine, movePathItem } from '../src/lib/path-routes.ts'

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
