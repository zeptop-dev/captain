import { test } from 'node:test'
import assert from 'node:assert/strict'
import { languages, readLanguage, saveLanguage } from './language.ts'

test('saved choice overrides the browser and supports all six languages', () => {
  for (const { value } of languages) {
    let stored
    assert.equal(saveLanguage(value, next => { stored = next }), true)
    assert.equal(readLanguage(() => stored, 'fr-FR'), value)
  }
})

test('browser regional tags and blocked storage have a usable fallback', () => {
  for (const [browser, expected] of [
    ['zh-Hant-TW', 'zh-TW'], ['zh-HK', 'zh-TW'], ['ZH_tw', 'zh-TW'],
    ['zh-Hans-CN', 'zh-CN'], ['zh', 'zh-CN'], ['en-US', 'en'],
    ['ja-JP', 'ja'], ['ko-KR', 'ko'], ['ru-RU', 'ru'], ['fr-FR', 'en'],
  ]) {
    assert.equal(readLanguage(() => null, browser), expected)
    assert.equal(readLanguage(() => { throw new Error('blocked') }, browser), expected)
  }
})

test('invalid selections and failed writes do not report a successful switch', () => {
  let writes = 0
  assert.equal(saveLanguage('unsupported', () => { writes++ }), false)
  assert.equal(writes, 0)
  assert.equal(saveLanguage('ja', () => { throw new Error('quota exceeded') }), false)
})
