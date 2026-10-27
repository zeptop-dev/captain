import assert from 'node:assert/strict'
import test from 'node:test'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'

const source = process.env.SETTINGS_SOURCE_ROOT
const moduleURL = source ? pathToFileURL(resolve(source, 'src/lib/server-detail.ts')) : new URL('../src/lib/server-detail.ts', import.meta.url)
const { serverDetailLocation, serverSettings } = await import(moduleURL.href)
const locate = (path = '/nodes/42', search = '', hash = '') => serverDetailLocation('42', path, search, hash)

test('server bookmarks open overview without mounting an editor', () => {
  assert.deepEqual(locate(), { tab: 'overview', section: 'proxy', redirect: null })
  assert.deepEqual(locate('/nodes/42', '?section=unknown'), locate())
})

test('information and diagnostics remain distinct destinations', () => {
  assert.deepEqual(locate('/nodes/42/information', '', '#billing-link'), { tab: 'information', section: 'proxy', redirect: null })
  assert.deepEqual(locate('/nodes/42/diagnostics', '?view=network'), { tab: 'diagnostics', section: 'proxy', redirect: null })
  assert.deepEqual(locate('/nodes/42/diagnostics', '?view=routes'), { tab: 'diagnostics', section: 'proxy', redirect: null })
})

test('each settings destination keeps its own editor selection', () => {
  for (const section of ['proxy', 'ingresses', 'routing', 'overrides', 'runtime']) {
    assert.ok(serverSettings.includes(section))
    assert.deepEqual(locate(`/nodes/42/settings/${section}`), { tab: 'settings', section, redirect: null })
  }
  for (const path of ['/nodes/42/settings', '/nodes/42/settings/unknown']) {
    assert.deepEqual(locate(path), { tab: 'settings', section: 'proxy', redirect: '/nodes/42/settings/proxy' })
  }
})

test('legacy proxy links redirect to the matching settings section', () => {
  for (const section of ['ingresses', 'routing', 'overrides']) {
    assert.deepEqual(locate('/nodes/42', `?section=${section}`), { tab: 'settings', section, redirect: `/nodes/42/settings/${section}` })
  }
})

test('legacy diagnostics and billing links retain their intended action', () => {
  assert.deepEqual(locate('/nodes/42', '', '#diagnostics'), { tab: 'diagnostics', section: 'proxy', redirect: '/nodes/42/diagnostics?view=network' })
  assert.deepEqual(locate('/nodes/42', '', '#billing-link'), { tab: 'information', section: 'proxy', redirect: '/nodes/42/information#billing-link' })
})

test('old query and hash values cannot override an explicit new page', () => {
  assert.deepEqual(locate('/nodes/42/information', '?section=overrides', '#diagnostics'), { tab: 'information', section: 'proxy', redirect: null })
  assert.deepEqual(locate('/nodes/42/settings/runtime', '', '#billing-link'), { tab: 'settings', section: 'runtime', redirect: null })
})

test('an unknown tab returns safely to overview', () => {
  assert.deepEqual(locate('/nodes/42/unknown'), { tab: 'overview', section: 'proxy', redirect: '/nodes/42' })
})
