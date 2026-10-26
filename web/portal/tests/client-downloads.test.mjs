import test from 'node:test'
import assert from 'node:assert/strict'
import { clientDownloadURL, clientFileKey, clientsForPlatform, initialClientPlatform, selectedClientFile, clientSubscriptionFormat } from '../../shared/client-downloads.ts'

const base = { homepage: 'https://example.com', downloads: [], format: 'clash', source: 'official' }
const clients = [
  { ...base, id: 'second', name: 'Second', platform: 'windows', recommended: false, sort: 0 },
  { ...base, id: 'first', name: 'First', platform: 'windows', recommended: true, sort: 3 },
  { ...base, id: 'mobile', name: 'Mobile', platform: 'ios', recommended: true, sort: 0 },
]
test('only the requested platform appears, recommendation comes first without mutating the catalog', () => {
  assert.deepEqual(clientsForPlatform(clients, 'windows').map(c => c.id), ['first', 'second'])
  assert.deepEqual(clients.map(c => c.id), ['second', 'first', 'mobile'])
  assert.deepEqual(clientsForPlatform(clients, 'android'), [])
})
test('OS detection can choose published iPad entries but does not infer an installer architecture', () => {
  assert.equal(initialClientPlatform(clients, 'Mozilla/5.0 (Macintosh; Intel Mac OS X)', 5), 'ios')
  assert.equal(initialClientPlatform(clients, 'Android'), 'windows')
  const files = [{ id: 1, label: 'Intel', arch: 'x64', url: 'https://example.com/x64' }, { id: 2, label: 'ARM', arch: 'arm64', url: 'https://example.com/arm64' }]
  assert.equal(selectedClientFile(files, ''), undefined)
  assert.equal(selectedClientFile(files, 'stale'), undefined)
  assert.equal(selectedClientFile(files, clientFileKey(files[1])), files[1])
  assert.equal(selectedClientFile([files[0]], ''), files[0])
})
test('only controlled local downloads or credential-free HTTP(S) links can be opened', () => {
  for (const value of ['javascript:alert(1)', 'data:text/html,test', '//example.com/file', '/api/admin', '/downloads/clients/app/1/../file', '/downloads/clients/app/1/%2e%2e', '/downloads/clients/app/1/%2ffile', '/downloads/clients/app/1/%5cfile', '/downloads/clients/app/1/%0afile', '/downloads/clients/app/1/file?path=x', 'https://user:pass@example.com/file', ' https://example.com/file', 'https://example.com/\nfile']) assert.equal(clientDownloadURL(value), undefined, value)
  for (const value of ['https://example.com/client.zip', 'http://example.com/legacy', '/downloads/clients/clash-verge-rev/123/client%20arm64.dmg']) assert.equal(clientDownloadURL(value), value)
})
test('node-only format is explicit and unknown formats are not advertised as complete', () => {
  assert.equal(clientSubscriptionFormat('uri'), 'nodes')
  for (const format of ['clash', 'stash', 'surge']) assert.equal(clientSubscriptionFormat(format), 'full')
  assert.equal(clientSubscriptionFormat('custom'), 'unknown')
})
