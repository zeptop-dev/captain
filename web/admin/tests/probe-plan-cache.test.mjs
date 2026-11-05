import assert from 'node:assert/strict'
import test from 'node:test'
import { QueryClient } from '@tanstack/react-query'
import { clearProbePlanResults } from '../src/lib/probe-plan-cache.ts'

test('saving one server clears every range and prevents a late old response restoring its results', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key = ['network-quality', 1, '24h']
  client.setQueryData(key, { protocol: 'tcp', latency: 12 })
  client.setQueryData(['network-quality', 1, '7d'], { protocol: 'tcp' })
  client.setQueryData(['network-quality', 2, '24h'], { protocol: 'icmp' })
  client.setQueryData(['node-probe-plan', 1], { mode: 'custom' })
  let reply
  const oldRead = client.fetchQuery({ queryKey: key, queryFn: () => new Promise(resolve => { reply = resolve }) }).catch(() => {})
  await clearProbePlanResults(client, 1)
  reply({ protocol: 'tcp', latency: 10 })
  await oldRead
  assert.equal(client.getQueryData(key), undefined)
  assert.equal(client.getQueryData(['network-quality', 1, '7d']), undefined)
  assert.deepEqual(client.getQueryData(['network-quality', 2, '24h']), { protocol: 'icmp' })
  assert.deepEqual(client.getQueryData(['node-probe-plan', 1]), { mode: 'custom' })
  client.clear()
})

test('global save clears network caches without erasing the active settings form query', async () => {
  const client = new QueryClient()
  client.setQueryData(['network-quality', 1, '24h'], { protocol: 'tcp' })
  client.setQueryData(['network-quality', 2, '7d'], { protocol: 'icmp' })
  client.setQueryData(['probe-settings'], { enabled: true })
  await clearProbePlanResults(client)
  assert.equal(client.getQueryCache().findAll({ queryKey: ['network-quality'] }).length, 0)
  assert.deepEqual(client.getQueryData(['probe-settings']), { enabled: true })
  client.clear()
})
