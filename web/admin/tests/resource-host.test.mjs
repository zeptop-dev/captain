import assert from 'node:assert/strict'
import test from 'node:test'
import { latestResourceHost } from '../src/lib/resource-host.ts'

test('the server overview uses the newest resource sample across live beats and full reports', () => {
  const older = { resources: { at: 1000 }, cpu_percent: 10 }
  const newer = { resources: { at: 2000 }, cpu_percent: 20 }
  assert.equal(latestResourceHost(older, newer), newer)
  assert.equal(latestResourceHost(newer, older), newer)
  assert.equal(latestResourceHost(newer, { ...newer }), newer, 'Equal timestamps preserve the live sample')
})

test('missing timestamps and missing sources retain the existing fallback behavior', () => {
  const live = { cpu_percent: 10 }
  const reported = { cpu_percent: 20 }
  const sampled = { resources: { at: 2000 }, cpu_percent: 30 }
  assert.equal(latestResourceHost(live, reported), live)
  assert.equal(latestResourceHost(undefined, reported), reported)
  assert.equal(latestResourceHost(null, reported), reported)
  assert.equal(latestResourceHost(live, undefined), live)
  assert.equal(latestResourceHost(live, null), live)
  assert.equal(latestResourceHost(live, sampled), sampled)
  assert.equal(latestResourceHost(sampled, reported), sampled)
  assert.equal(latestResourceHost(undefined, undefined), undefined)
  assert.equal(latestResourceHost(null, null), null)
})
