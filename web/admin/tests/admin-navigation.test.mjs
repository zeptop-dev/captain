import test from 'node:test'
import assert from 'node:assert/strict'
import { navigation, visibleNavigation, activeNavigationItem, workspaceDestination, navigationGroup } from '../src/lib/navigation.ts'

const group = id => navigation.find(group => group.id === id)
const item = path => navigation.flatMap(group => group.items).find(item => item.to === path)
const current = path => { const url = new URL(path, 'https://example.com'); return { pathname: url.pathname, search: url.search, hash: url.hash } }

test('workspace visibility preserves existing staff access boundaries', () => {
  assert.deepEqual(visibleNavigation('admin').map(group => group.id), ['overview', 'users', 'nodes', 'entries', 'subscription', 'monitoring', 'business', 'support'])
  assert.deepEqual(visibleNavigation('operator').map(group => group.id), ['overview', 'users', 'nodes', 'entries', 'monitoring', 'business', 'support'])
  assert.deepEqual(visibleNavigation('operator').find(group => group.id === 'nodes').items.map(item => item.to), ['/nodes', '/domains'])
  assert.deepEqual(visibleNavigation('support').map(group => group.items.map(item => item.to)), [['/'], ['/users'], ['/orders'], ['/tickets']])
  assert.equal(navigationGroup('/nodes/12'), 'nodes')
  assert.equal(navigationGroup('/settings/category/site'), 'system')
  assert.equal(navigationGroup('/account'), 'account')
  assert.equal(navigationGroup('/business/content'), 'support')
})

test('query-selected workspace pages have one active destination, with editor modes sharing one tab', () => {
  for (const [id, path, search, expected] of [
    ['users', '/users', '?tab=groups&filter=active', '/users?tab=groups'],
    ['users', '/users', '?tab=renewals', '/users?tab=renewals'],
    ['users', '/users', '?tab=unknown', '/users'],
    ['entries', '/entries', '?tab=lines&server=2', '/entries?tab=lines'],
    ['subscription', '/sub-templates', '?mode=text&format=surge', '/sub-templates'],
    ['subscription', '/sub-templates', '?mode=design', '/sub-templates'],
    ['subscription', '/sub-templates', '?mode=library&format=surge', '/sub-templates?mode=library'],
    ['monitoring', '/monitoring', '?tab=dns', '/monitoring?tab=dns'],
    ['nodes', '/nodes/12', '', '/nodes'],
  ]) assert.equal(activeNavigationItem(group(id).items, path, search)?.to, expected)
  assert.equal(activeNavigationItem(group('nodes').items, '/unknown', ''), undefined)
})

test('switching a workspace selector preserves filters and removes only obsolete entry creation state', () => {
  assert.equal(workspaceDestination(item('/sub-templates?mode=profiles'), current('/sub-templates?mode=text&format=surge')), '/sub-templates?mode=profiles&format=surge')
  assert.equal(workspaceDestination(item('/sub-templates'), current('/sub-templates?mode=profiles&format=surge')), '/sub-templates?format=surge')
  assert.equal(workspaceDestination(item('/users?tab=renewals'), current('/users?tab=groups&filter=active')), '/users?tab=renewals&filter=active')
  assert.equal(workspaceDestination(item('/entries?tab=lines'), current('/entries?server=2&node=3&filter=active&create=1&section=wg&inbound=4#old')), '/entries?server=2&node=3&filter=active&tab=lines')
  assert.equal(workspaceDestination(item('/nodes'), current('/domains?tab=certificates')), '/nodes')
})

test('reselecting the active editor preserves its URL, format, and fragment exactly', () => {
  for (const path of ['/sub-templates?mode=text&format=surge#saved', '/sub-templates?mode=design&format=clash', '/users?tab=groups&filter=active#row']) {
    const loc = current(path)
    const items = group(path.startsWith('/users') ? 'users' : 'subscription').items
    assert.equal(workspaceDestination(activeNavigationItem(items, loc.pathname, loc.search), loc), path)
  }
})
