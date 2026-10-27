import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveSettingsRoute, settingsCategories, settingsCatalog, settingsEntryPath, searchSettings } from '../src/lib/settings-catalog.ts'
import { canVisit, navigation, navigationGroup } from '../src/lib/navigation.ts'

test('all legacy setting links lead to a canonical category and section', () => {
  assert.equal(settingsCategories.length, 8)
  for (const entry of settingsCatalog) {
    const route = resolveSettingsRoute(entry.id)
    assert.equal(route.redirect, settingsEntryPath(entry))
    assert.equal(resolveSettingsRoute(`category/${route.category}`).category, entry.category)
    assert.equal(navigationGroup(route.redirect.split('#')[0]), 'system')
  }
  assert.equal(resolveSettingsRoute('').redirect, '/settings/category/site')
  assert.equal(resolveSettingsRoute('area/users').redirect, '/settings/category/account')
  assert.equal(resolveSettingsRoute('area/nodes').redirect, '/settings/category/proxy')
  assert.equal(resolveSettingsRoute('area/security').redirect, '/settings/category/account')
  assert.equal(resolveSettingsRoute('category/unknown'), null)
  assert.equal(resolveSettingsRoute('area/site'), null)
  assert.equal(resolveSettingsRoute('unknown'), null)
})

test('search finds direct editors across categories using only metadata', () => {
  assert.deepEqual(searchSettings('SMTP', key => key).map(e => e.id), ['mail'])
  assert.deepEqual(searchSettings('Single plan purchase', key => key).map(e => e.id), ['purchase'])
  assert.deepEqual(searchSettings('   ', key => key), [])
  assert.deepEqual(searchSettings('unknown-token-value', key => key), [])
  assert.equal(new Set(settingsCatalog.map(e => e.id)).size, settingsCatalog.length)
})

test('settings remain admin-only and resource navigation does not duplicate global settings', () => {
  const paths = ['/settings', '/site', ...settingsCatalog.flatMap(e => [`/settings/${e.id}`, settingsEntryPath(e)])]
  for (const path of paths) {
    assert.equal(canVisit(path, 'admin'), true)
    assert.equal(canVisit(path, 'operator'), false, path)
    assert.equal(canVisit(path, 'support'), false, path)
  }
  assert.equal(canVisit('/nodes/1', 'operator'), true)
  assert.equal(canVisit('/users?tab=groups', 'support'), false)
  assert.equal(canVisit('/account', 'support'), true)
  assert.ok(navigation.every(group => group.items.every(item => !item.to.startsWith('/settings'))))
  assert.equal(navigationGroup('/site'), 'system')
})
