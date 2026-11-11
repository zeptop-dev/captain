import test from 'node:test'
import assert from 'node:assert/strict'
import { importClients, subscriptionImportURL, subscriptionURLForClient } from '../src/lib/connection.ts'

test('Surge VLESS import persists explicit capability across later subscription updates', () => {
  const original = 'https://panel.example.com/sub/test?client=clash&entry=42'
  const imported = new URL(subscriptionImportURL(original, 'surge-vless'))
  assert.equal(imported.protocol, 'surge:')
  const subscription = new URL(imported.searchParams.get('url'))
  assert.equal(subscription.searchParams.get('client'), 'surge-vless')
  assert.equal(subscription.searchParams.get('entry'), '42')
  assert.equal(subscription.pathname, '/sub/test')
  assert.equal(new URL(subscriptionImportURL(original, 'surge')).searchParams.get('url'), original)
  assert.ok(importClients.find(c => c.value === 'surge-vless').label.includes('Mac 6.12+'))
})

test('copied links and QR contents retain the same explicit capability', () => {
 const source = 'https://panel.example.com/sub/test?entry=42'
 const selected = subscriptionURLForClient(source, 'surge-vless')
 assert.equal(new URL(selected).searchParams.get('client'), 'surge-vless')
 assert.equal(subscriptionURLForClient(source, 'surge'), source)
 assert.equal(subscriptionURLForClient('', 'surge-vless'), '')
 assert.equal(subscriptionURLForClient(selected, 'surge-vless'), selected)
})
