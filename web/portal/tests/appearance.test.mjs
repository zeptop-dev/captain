import assert from 'node:assert/strict'
import test from 'node:test'
import { effectivePortalScheme, parsePortalPreference, readPortalPreference, portalSchemeStorageKey } from '../src/lib/appearance.ts'

test('portal starts light and keeps visitor choices separate from other interfaces', () => {
  assert.equal(effectivePortalScheme(null, undefined, true), 'light')
  assert.equal(effectivePortalScheme('dark', 'light', false), 'dark')
  assert.equal(effectivePortalScheme('light', 'dark', true), 'light')
  assert.equal(portalSchemeStorageKey, 'captain-portal-color-scheme')
  assert.equal(readPortalPreference({ getItem: key => key === 'mantine-color-scheme-value' ? 'dark' : null }), null)
})
test('operator defaults remain effective until the visitor makes a choice', () => {
  assert.equal(effectivePortalScheme(null, 'dark', false), 'dark')
  assert.equal(effectivePortalScheme(null, 'auto', true), 'dark')
  assert.equal(effectivePortalScheme(null, 'auto', false), 'light')
  assert.equal(effectivePortalScheme('light', 'auto', true), 'light')
})
test('invalid or inaccessible browser storage safely falls back', () => {
  for (const value of [null, '', 'auto', 'invalid']) assert.equal(parsePortalPreference(value), null)
  assert.equal(readPortalPreference({ getItem: () => { throw new Error('blocked') } }), null)
  assert.equal(readPortalPreference({ getItem: () => 'dark' }), 'dark')
})
