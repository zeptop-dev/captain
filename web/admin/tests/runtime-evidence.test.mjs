import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { createObservationClock, observationFresh, observedRequest } from '../src/lib/observed-request.ts'
import { renderedConfigStatus } from '../src/lib/rendered-config.ts'
import { operationDetailURL, operationPageURL, operationSourceURL, operationsURL, operationStepFresh, safeOperationSourceID, operationStatuses, operationSources } from '../src/lib/operations.ts'
import { canVisit, navigationGroup, visibleNavigation } from '../src/lib/navigation.ts'

const start = { monotonic: 100, wall: 1000 }
const received = { monotonic: 200, wall: 1100 }
const response = () => ({ started: start, received, value: { status: 'observed', config_revision: 'cfg1-a', state_revision: 'state-a', current: { id: 'delivery', config_revision: 'cfg1-a', state_revision: 'state-a', applied: true, current: true, fresh: true, fresh_for_ms: 1000, metadata_status: 'valid', evidence: { version: 1, status: 'observed', artifacts: [] } }, history: [] } })

test('runtime evidence confirmation requires exact applied current state and complete metadata', () => {
  assert.equal(renderedConfigStatus(response(), received, true), 'observed')
  for (const patch of [{ applied: false }, { current: false }, { fresh: false }, { metadata_status: 'invalid' }, { metadata_status: 'unavailable' }, { evidence: null }, { evidence: { version: 1, status: 'unavailable', artifacts: [{ core: 'xray' }] } }, { config_revision: 'cfg1-old' }, { state_revision: 'state-old' }]) {
    const value = response(); Object.assign(value.value.current, patch)
    assert.equal(renderedConfigStatus(value, received, true), 'unknown', JSON.stringify(patch))
  }
  const value = response(); value.value.current = null; value.value.history.push(response().value.current)
  assert.equal(renderedConfigStatus(value, received, true), 'unknown', 'Historical evidence cannot replace the current delivery')
})

test('runtime evidence expires from dispatch across slow requests, suspension and clock rollback', () => {
  for (const now of [{ monotonic: 1100, wall: 1100 }, { monotonic: 200, wall: 2000 }, { monotonic: 1200, wall: 500 }]) assert.equal(renderedConfigStatus(response(), now, true), 'stale')
  const delayed = response(); delayed.received = { monotonic: 1300, wall: 2200 }
  assert.equal(renderedConfigStatus(delayed, received, true), 'stale', 'A late response cannot renew confirmation')
  assert.equal(renderedConfigStatus(response(), received, false), 'unknown', 'Offline or hidden revokes current confirmation')
  for (const status of ['pending', 'stale', 'unavailable', 'unknown']) { const value = response(); value.value.status = status; assert.equal(renderedConfigStatus(value, received, true), status) }
  for (const ms of [0, -1, NaN, Infinity]) assert.equal(observationFresh(ms, start, received, received, true), false)
})

test('an unmounted request aborts the real hanging HTTP connection', async () => {
  let arrivedResolve, closedResolve
  const arrived = new Promise(resolve => { arrivedResolve = resolve }), closed = new Promise(resolve => { closedResolve = resolve })
  const server = createServer(request => { request.on('close', closedResolve); arrivedResolve() })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  const parent = new AbortController()
  try {
    const result = observedRequest(parent.signal, signal => fetch(`http://127.0.0.1:${server.address().port}/hang`, { signal }), 2000)
    const rejected = assert.rejects(result)
    await arrived; parent.abort(); await rejected
    await Promise.race([closed, new Promise((_, reject) => { const timer = setTimeout(() => reject(new Error('HTTP connection remained open')), 1000); timer.unref() })])
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)) }
})

test('a timeout aborts its request and a late successful callback cannot restore confirmation', async () => {
  let signal
  const parent = new AbortController()
  await assert.rejects(observedRequest(parent.signal, input => { signal = input; return new Promise(resolve => setTimeout(() => resolve('late'), 30)) }, 5), /observation_timeout/)
  assert.equal(signal.aborted, true)
  parent.abort()
  let called = false
  await assert.rejects(observedRequest(parent.signal, async () => { called = true }))
  assert.equal(called, false)
})

test('operation navigation and opaque pagination preserve data without creating tasks', () => {
  const cursor = 'a+b/c?next=&one', url = new URL(operationsURL({ source: 'path_rollout', status: 'paused', node_id: '4', line_id: '9' }, cursor), 'http://localhost')
  assert.equal(url.searchParams.get('before'), cursor); assert.equal(url.searchParams.get('limit'), '20')
  assert.equal(url.searchParams.get('source'), 'path_rollout'); assert.equal(url.searchParams.get('node_id'), '4')
  const invalid = new URL(operationsURL({ source: 'arbitrary', node_id: '1&admin=true', line_id: '9007199254740992' }), 'http://localhost')
  assert.deepEqual([...invalid.searchParams], [['limit', '20']])
  assert.equal(operationDetailURL('path_rollout:AbC'), '/api/admin/operations/path_rollout%3AAbC')
  assert.equal(operationPageURL('path_rollout:AbC'), '/operations/path_rollout%3AAbC')
  assert.equal(operationSourceURL({ source: 'path_rollout', source_id: 'AbC' }), '/entries?tab=lines&mode=paths&operation_rollout=AbC&operation_source=path_rollout')
  assert.equal(operationSourceURL({ source: 'topology_changeset', source_id: 'AbC' }), '/entries?tab=changesets&changeset=AbC')
  assert.equal(operationSourceURL({ source: 'node_job', source_id: 'AbC', node_id: 2 }), '/nodes/2/diagnostics', 'Avoid host-maintenance page which automatically submits inspect')
  assert.equal(operationSourceURL({ source: 'node_upgrade', source_id: 'AbC' }), null)
  for (const value of ['../nodes', 'new', 'x?admin=true', 'https://example.com', 'x/y', null]) assert.equal(safeOperationSourceID(value), undefined)
})

test('historical operation completion cannot renew step receipt freshness', () => {
  const operation = { started: start, received, value: { status: 'completed', evidence: 'historical' } }
  const step = { status: 'applied', receipt_applied: true, observed_at: '2026-11-01T00:00:00Z', fresh_for_ms: 1000 }
  assert.equal(operationStepFresh(step, operation, received, true), true)
  assert.equal(operationStepFresh(step, operation, { monotonic: 200, wall: 2000 }, true), false)
  assert.equal(operationStepFresh(step, operation, received, false), false)
  assert.equal(operationStepFresh({ ...step, receipt_applied: false }, operation, received, true), false)
  assert.equal(operationStepFresh({ ...step, observed_at: undefined }, operation, received, true), false)
})

test('operation navigation is only available to administrators, including deep links', () => {
  for (const path of ['/operations', '/operations/path_rollout%3Aexample']) {
    assert.equal(canVisit(path, 'admin'), true)
    for (const role of ['operator', 'support']) assert.equal(canVisit(path, role), false)
  }
  assert.equal(navigationGroup('/operations/path_rollout%3Aexample'), 'monitoring')
  for (const role of ['operator', 'support']) assert.equal(visibleNavigation(role).flatMap(group => group.items).some(item => item.to === '/operations'), false)
})

test('both features have matching six-language keys and interpolation parameters', async () => {
  const flatten = (obj, prefix = '') => Object.fromEntries(Object.entries(obj).flatMap(([key, value]) => typeof value === 'object' ? Object.entries(flatten(value, prefix + key + '.')) : [[prefix + key, value]]))
  let reference
  for (const language of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const dictionary = JSON.parse(await readFile(new URL(`../src/i18n/${language}.json`, import.meta.url)))
    const values = flatten({ renderedConfig: dictionary.renderedConfig, operations: dictionary.operations })
    if (!reference) reference = values
    assert.deepEqual(Object.keys(values).sort(), Object.keys(reference).sort(), language)
    for (const [key, text] of Object.entries(values)) {
      assert.equal(typeof text, 'string'); assert.ok(text.trim(), `${language}.${key}`)
      assert.deepEqual([...text.matchAll(/{{(.*?)}}/g)].map(match => match[1]).sort(), [...reference[key].matchAll(/{{(.*?)}}/g)].map(match => match[1]).sort(), `${language}.${key}`)
    }
    for (const source of operationSources) { assert.ok(values[`operations.source.${source}`]); for (const status of operationStatuses[source]) assert.ok(values[`operations.status.${status}`]) }
    assert.equal(dictionary.serverDetail.diagnostics['rendered-config'], dictionary.renderedConfig.title)
  }
})

test('clock rollback never revives an old response, while a new request can establish fresh evidence', () => {
  let raw = { monotonic: 0, wall: 1000 }
  const clock = createObservationClock(() => raw), dispatched = clock()
  raw = { monotonic: 10, wall: 1010 }; const accepted = clock()
  raw = { monotonic: 20, wall: 500 }
  assert.equal(observationFresh(100, dispatched, accepted, clock(), true), false)
  raw = { monotonic: 25, wall: 1020 }
  assert.equal(observationFresh(100, dispatched, accepted, clock(), true), false, 'Catching up after a rollback cannot restore the old response')
  const newDispatch = clock(), newReceipt = clock()
  assert.equal(observationFresh(100, newDispatch, newReceipt, clock(), true), true)
  raw = { monotonic: 26, wall: 5000 }; assert.equal(observationFresh(100, newDispatch, newReceipt, clock(), true), false)
  raw = { monotonic: 27, wall: 1021 }; assert.equal(observationFresh(100, newDispatch, newReceipt, clock(), true), false, 'A wall-clock expiry during sleep remains expired after clock correction')
  assert.equal(observationFresh(100, { monotonic: 0, wall: 1000 }, { monotonic: 10, wall: 1010 }, { monotonic: 20, wall: 500 }, true), false)
})
