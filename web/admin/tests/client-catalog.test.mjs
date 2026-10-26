import assert from 'node:assert/strict'
import test from 'node:test'
import { applyClientSave, availableClientAssets, clientDraft, clientDraftErrors, clientJobProgress, safeClientExternalURL, selectableClientAssets } from '../src/lib/client-catalog.ts'

const platform = { platform: 'windows', show_home: false, show_portal: false, recommended: true, sort: 0, source: 'official', asset_ids: [1] }
const asset = { id: 2, name: 'new.exe', platform: 'windows', arch: 'x64', package_type: 'exe', size: 20, url: 'https://example.com/new.exe', version: '2' }
const client = { id: 'example', revision: 2, platforms: [platform], release: { version: '2', assets: [asset] }, cached_assets: [], published: [{ platform: 'windows', version: '1', downloads: [{ id: 1, label: 'old.exe', url: 'https://example.com/old.exe', arch: 'x64' }] }] }

test('checking a new release retains older published installer metadata and selections', () => {
  assert.deepEqual(availableClientAssets(client).map(file => file.id), [1, 2])
  assert.equal(availableClientAssets(client)[0].version, '1')
  const draft = clientDraft(client)
  assert.deepEqual(draft.platforms[0].asset_ids, [1])
  draft.platforms[0].asset_ids.push(3)
  assert.deepEqual(client.platforms[0].asset_ids, [1])
})

test('cached sources require matching ready artifacts rather than silently using official files', () => {
  const draft = clientDraft(client)
  draft.platforms[0].source = 'cache'
  assert.equal(clientDraftErrors(client, draft)['platforms.0.asset_ids'], 'cacheNotReady')
  draft.platforms[0].asset_ids = []
  assert.equal(clientDraftErrors(client, draft)['platforms.0.asset_ids'], 'cacheSelectionRequired')
  assert.deepEqual(selectableClientAssets(client, draft.platforms[0]), [])
  const cached = { ...client, cached_assets: [asset] }
  draft.platforms[0].asset_ids = [2]
  assert.deepEqual(clientDraftErrors(cached, draft), {})
  assert.equal(selectableClientAssets(cached, draft.platforms[0])[0].id, 2)
})

test('a slow save keeps newer edits dirty and advances only the known revision', () => {
  const submitted = clientDraft(client)
  const current = clientDraft(client)
  current.platforms[0].show_home = true
  const result = { ...client, revision: 3 }
  const merged = applyClientSave(current, submitted, result)
  assert.equal(merged.current.revision, 3)
  assert.equal(merged.current.platforms[0].show_home, true)
  assert.equal(merged.baseline.platforms[0].show_home, false)
  assert.notDeepEqual(merged.current, merged.baseline)
  assert.deepEqual(applyClientSave(submitted, submitted, result).current, clientDraft(result))
})

test('download progress uses only actual bytes and external links reject active schemes', () => {
  assert.equal(clientJobProgress({ received_bytes: 20, total_bytes: 40 }), 50)
  assert.equal(clientJobProgress({ received_bytes: 40, total_bytes: 0 }), null)
  assert.equal(clientJobProgress({ received_bytes: -2, total_bytes: 40 }), null)
  for (const url of ['javascript:alert(1)', 'data:text/html,test', 'http://example.com', 'https://u:p@example.com']) assert.equal(safeClientExternalURL(url), undefined)
  assert.equal(safeClientExternalURL('https://example.com/a'), 'https://example.com/a')
})
