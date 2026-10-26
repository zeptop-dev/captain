import assert from 'node:assert/strict'
import test from 'node:test'
import { subscriptionImportURL } from '../src/lib/connection.ts'

test('client import labels use the brand without changing the subscription URL', () => {
  const subscription = 'https://example.com/sub?token=example&format=clash'
  const brand = '示例 & Network #1'
  const imported = subscriptionImportURL(subscription, 'shadowrocket', brand)
  assert.equal(new URL(imported).searchParams.get('remark'), brand)
  assert.ok(imported.includes(btoa(subscription)))
  assert.equal(new URL(subscriptionImportURL(subscription, 'shadowrocket')).searchParams.get('remark'), 'Captain')
  assert.equal(subscriptionImportURL(subscription, 'clash', brand), subscriptionImportURL(subscription, 'clash'))
})
