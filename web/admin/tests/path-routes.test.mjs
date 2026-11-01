import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { pathInput, routeInput, routePortsComplete, pathStepsComplete, linearSourceLine, pathSourceLineOptions, selectPathSourceLine, movePathItem, pathBackend, pathInboundSupported, pathSelectionIssue, pathHealthTransport } from '../src/lib/path-routes.ts'

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

test('source selection accepts linear transport/backend combinations but excludes nonlinear and composite lines', () => {
  const line = { protocol: 'tcp', backend: 'relay', groups: [{ members: [{ node_id: 1 }] }] }
  assert.equal(linearSourceLine(line), true)
  for (const protocol of ['tcp', 'udp', 'both']) for (const backend of ['relay', 'realm', 'nft', '']) {
    assert.equal(linearSourceLine({ ...line, protocol, backend }), true)
  }
  assert.equal(linearSourceLine({ ...line, protocol: 'unknown' }), false)
  assert.equal(linearSourceLine({ ...line, backend: 'unknown' }), false)
  assert.equal(linearSourceLine({ ...line, groups: [{ members: [{}, {}] }] }), false)
  assert.equal(linearSourceLine({ ...line, path_route: true }), false)
})

test('direct backend edits survive reorder while references cannot override their pinned backend', () => {
  const steps = [{ id: 'a', node_id: 1, backend: 'nft' }, { id: 'b', node_id: 2, backend: 'realm' }]
  const input = pathInput({ name: 'path', version: 2, steps: movePathItem(steps, 0, 1) })
  assert.deepEqual(input.steps.map(step => [step.id, step.backend]), [['b', 'realm'], ['a', 'nft']])
  const references = pathInput({ name: 'refs', version: 2, steps: [{ id: 'p', path_id: 3, path_revision: 4, backend: 'nft' }, { id: 'l', line_id: 5, line_version: 6, backend: 'realm' }] })
  assert.equal(references.steps.some(step => 'backend' in step), false)
  assert.equal(pathInput({ name: 'reset', version: 2, steps: [{ id: 'a', node_id: 1, backend: 'relay' }] }).steps[0].backend, 'relay')
  assert.equal(pathBackend(undefined), 'relay')
  assert.equal(pathBackend(''), 'relay')
})

test('route projection retains explicit entry backend resets and never writes derived protocol', () => {
  for (const entry_backend of ['relay', 'realm', 'nft', '']) {
    const projected = routeInput({ ...route(), protocol: 'udp', entry_backend })
    assert.equal(projected.entry_backend, entry_backend)
    assert.equal('protocol' in projected, false)
  }
  assert.equal('entry_backend' in routeInput(route()), false)
})

test('landing and candidate constraints reject unsafe combinations without changing the draft', () => {
  for (const field of ['reverse', 'wireguard_slot', 'accept_proxy_protocol']) assert.equal(pathInboundSupported({ Protocol: 'vless', Settings: { [field]: {} } }, 'tcp'), false)
  assert.equal(pathInboundSupported({ Protocol: 'wireguard', Settings: {} }, 'udp'), false)
  assert.equal(pathInboundSupported({ Protocol: 'mieru', Settings: {} }, 'both'), false)
  assert.equal(pathInboundSupported({ Protocol: 'mieru', Settings: {} }, 'udp'), true)
  for (const protocol of ['udp', 'both']) {
    assert.equal(pathSelectionIssue(protocol, 'relay', 2, 'failover'), 'udpRoundRobin')
    assert.equal(pathSelectionIssue(protocol, 'relay', 2, 'roundrobin'), null)
    assert.equal(pathSelectionIssue(protocol, 'realm', 2, 'roundrobin'), 'entryRelayRequired')
    assert.equal(pathSelectionIssue(protocol, 'nft', 2, 'roundrobin'), 'entryRelayRequired')
    assert.equal(pathSelectionIssue(protocol, 'realm', 1, 'failover'), null)
  }
  assert.equal(pathSelectionIssue('tcp', '', 2, 'failover'), null)
})

test('health compatibility requires a known complete TCP native graph at exact fixed revisions', () => {
  const draft = { ...route(), health: { enabled: true } }
  assert.equal(pathHealthTransport('tcp', draft, [path()]), 'supported')
  for (const protocol of ['udp', 'both']) assert.equal(pathHealthTransport(protocol, draft, [path()]), 'unsupported')
  for (const backend of ['nft', 'realm']) {
    assert.equal(pathHealthTransport('tcp', { ...draft, entry_backend: backend }, [path()]), 'unsupported')
    assert.equal(pathHealthTransport('tcp', draft, [{ ...path(), hops: [{ ...path().hops[0], backend }] }]), 'unsupported')
  }
  for (const paths of [[undefined], [{ ...path(), version: 99 }], [{ ...path(), hops: [] }]]) assert.equal(pathHealthTransport('tcp', draft, paths), 'unknown')
  assert.equal(pathHealthTransport(undefined, draft, [path()]), 'unknown')
  assert.equal(draft.health.enabled, true)
})

test('all six runtime languages include transport constraints and backend release boundaries', async () => {
  const keys = ['pathRoutes.scope', 'pathRoutes.lineHint', 'pathRoutes.entryBackend', 'pathRoutes.entryRelayRequired', 'pathRoutes.udpRoundRobin', 'pathRoutes.transportRequirement', 'pathRoutes.transportUnsupported', 'pathRoutes.releaseWarnings.realm', 'pathRoutes.releaseWarnings.nft', 'pathRoutes.releaseWarnings.relay', 'pathRollouts.scope', 'pathHealth.transportUnsupported', 'pathHealth.transportConflict', 'pathHealth.transportUnknown']
  const values = new Map(keys.map(key => [key, new Set()]))
  for (const language of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const dict = JSON.parse(await readFile(new URL(`../src/i18n/${language}.json`, import.meta.url), 'utf8'))
    for (const key of keys) {
      const value = key.split('.').reduce((parent, part) => parent?.[part], dict)
      assert.equal(typeof value, 'string', `${language}: ${key}`)
      assert.ok(value.trim(), `${language}: ${key}`)
      values.get(key).add(value)
    }
    assert.match(dict.pathRoutes.transportRequirement, /v0\.88\.0/)
    assert.match(dict.pathRoutes.releaseWarnings.realm, /realm/)
    assert.match(dict.pathRoutes.releaseWarnings.nft, /conntrack/)
  }
  for (const [key, translated] of values) assert.equal(translated.size, 6, `${key} must not reuse an untranslated fallback`)
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
