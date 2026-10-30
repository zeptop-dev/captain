import assert from 'node:assert/strict'
import { test } from 'node:test'
import { DiagnosticSession, diagnosticFresh } from '../src/lib/diagnostic-session.ts'
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
const settle = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }
function rig(overrides = {}) {
  let time = 0, next = 0
  const timers = new Map(), released = [], states = [], calls = []
  const io = {
    newID: () => 'a', create: async () => ({ id: 'a', fresh_for_ms: 30000 }), renew: async id => ({ id, fresh_for_ms: 30000 }),
    read: async () => ({ sequence: 1 }), release: async id => { released.push(id) },
    clock: () => ({ monotonic: time, wall: time + 10000 }), changed: s => states.push(s),
    schedule: (fn, ms) => { const id = ++next; timers.set(id, { fn, at: time + ms }); return id }, cancel: id => timers.delete(id), ...overrides,
  }
  for (const key of ['create', 'renew', 'read']) { const f = io[key]; io[key] = (...args) => { calls.push(key); return f(...args) } }
  const session = new DiagnosticSession(io)
  const advance = async ms => { time += ms; const due = [...timers].filter(([, v]) => v.at <= time); for (const [id, v] of due) { if (timers.delete(id)) v.fn() } await settle() }
  return { session, released, states, calls, timers, advance }
}
test('closing before create completes releases the late lease and never reads it', async () => {
  const create = deferred(), r = rig({ create: () => create.promise })
  const started = r.session.start(); r.session.stop(); create.resolve({ id: 'a', fresh_for_ms: 30000 }); await started
  assert.deepEqual(r.released, ['a', 'a']); assert.equal(r.calls.includes('read'), false); assert.equal(r.timers.size, 0)
})
test('closing during renewal releases again after the late server extension', async () => {
  const renewal = deferred(), r = rig({ renew: () => renewal.promise })
  await r.session.start(); await settle(); await r.advance(10000)
  assert.ok(r.calls.includes('renew')); r.session.stop(); assert.deepEqual(r.released, ['a'])
  renewal.resolve({ id: 'a', fresh_for_ms: 30000 }); await settle()
  assert.deepEqual(r.released, ['a', 'a']); assert.equal(r.timers.size, 0); assert.equal(r.states.at(-1).phase, 'idle')
})
test('failed sample request revokes cached data and stops all renewals', async () => {
  let count = 0
  const r = rig({ read: async () => { if (++count > 1) throw new Error('offline'); return { sequence: 1 } } })
  await r.session.start(); await settle(); assert.equal(r.states.at(-1).data.sequence, 1)
  await r.advance(1000)
  assert.equal(r.states.at(-1).phase, 'error'); assert.equal(r.states.at(-1).data, undefined)
  assert.deepEqual(r.released, ['a']); assert.equal(r.timers.size, 0)
  await r.advance(60000); assert.equal(r.calls.includes('renew'), false)
})
test('a hanging create expires and its late response is still cleaned up', async () => {
  const create = deferred(), r = rig({ create: () => create.promise })
  void r.session.start(); await r.advance(10000); assert.equal(r.states.at(-1).phase, 'error')
  create.resolve({ id: 'a', fresh_for_ms: 30000 }); await settle()
  assert.deepEqual(r.released, ['a', 'a']); assert.equal(r.states.at(-1).phase, 'error')
})
test('a closed generation cannot replace a newly opened session', async () => {
  const old = deferred(); let count = 0, ids = 0
  const r = rig({ newID: () => ++ids === 1 ? 'old' : 'new', create: () => ++count === 1 ? old.promise : Promise.resolve({ id: 'new', fresh_for_ms: 30000 }) })
  void r.session.start(); r.session.stop(); await r.session.start(); await settle()
  old.resolve({ id: 'old', fresh_for_ms: 30000 }); await settle()
  assert.deepEqual(r.released, ['old', 'old']); assert.equal(r.states.at(-1).phase, 'active')
  r.session.stop(); assert.deepEqual(r.released, ['old', 'old', 'new'])
})
test('sample freshness includes slow responses, sleep and backwards wall clocks', () => {
  const start = { monotonic: 100, wall: 1000 }
  assert.equal(diagnosticFresh(1000, start, { monotonic: 1099, wall: 1999 }, true), true)
  for (const now of [{ monotonic: 1100, wall: 900 }, { monotonic: 101, wall: 2000 }]) assert.equal(diagnosticFresh(1000, start, now, true), false)
  assert.equal(diagnosticFresh(1000, start, start, false), false)
  for (const ms of [NaN, 0, -1]) assert.equal(diagnosticFresh(ms, start, start, true), false)
})
test('a hanging GET expires locally and never renews the viewing demand indefinitely', async () => {
  const read = deferred(), r = rig({ read: () => read.promise })
  await r.session.start(); await r.advance(10000)
  assert.equal(r.states.at(-1).phase, 'error'); assert.equal(r.states.at(-1).data, undefined)
  assert.ok(r.released.includes('a')); assert.equal(r.timers.size, 0)
  read.resolve({ sequence: 9 }); await settle()
  assert.equal(r.states.at(-1).phase, 'error'); assert.equal(r.states.at(-1).data, undefined)
})
