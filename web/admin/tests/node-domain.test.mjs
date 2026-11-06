import assert from 'node:assert/strict'
import { test } from 'node:test'
import { composeNodeDomain, incompleteNodeDomain, splitNodeDomain } from '../src/lib/node-domain.ts'

const zones = [{ name: 'example.com' }, { name: 'nodes.example.com' }, { name: 'example.co.uk' }]
test('registered suffixes use the longest match and retain all prefix labels', () => {
  for (const [value, prefix, zone] of [
    ['edge.jp.nodes.example.com', 'edge.jp', 'nodes.example.com'],
    ['JP1.EXAMPLE.CO.UK.', 'jp1', 'example.co.uk'],
  ]) {
    const draft = splitNodeDomain(value, zones)
    assert.equal(draft.value, value, 'opening the editor must not rewrite the stored value')
    assert.equal(draft.prefix, prefix)
    assert.equal(draft.zone, zone)
    assert.equal(draft.manual, false)
  }
})
test('legacy and apex names stay complete instead of becoming an empty prefix', () => {
  for (const value of ['example.com', 'nodes.example.com', 'edge.external.example', 'notexample.com']) {
    const draft = splitNodeDomain(value, zones)
    assert.equal(draft.value, value)
    assert.equal(draft.manual, true)
    assert.equal(draft.zone, null)
  }
})
test('empty optional names and half-filled split drafts are distinct', () => {
  assert.equal(splitNodeDomain('', zones).manual, false)
  for (const [prefix, zone, expected] of [['', null, false], ['', 'example.com', true], ['edge', null, true], ['edge', 'example.com', false]]) {
    const draft = { prefix, zone, value: composeNodeDomain(prefix, zone), manual: false }
    assert.equal(incompleteNodeDomain(draft), expected)
    assert.equal(draft.value, prefix && zone ? 'edge.example.com' : '')
  }
  assert.equal(composeNodeDomain(' ', 'example.com'), '', 'blank must never resolve to an apex name')
})
test('ASCII server names match Unicode catalog suffixes without rewriting the catalog selection', () => {
  const value = 'edge.xn--bcher-kva.example'
  const draft = splitNodeDomain(value, [{ name: 'bücher.example' }])
  assert.equal(draft.manual, false)
  assert.equal(draft.prefix, 'edge')
  assert.equal(draft.zone, 'bücher.example')
  assert.equal(draft.value, value)
  assert.equal(splitNodeDomain('xn--bcher-kva.example', [{ name: 'bücher.example' }]).manual, true)
})
