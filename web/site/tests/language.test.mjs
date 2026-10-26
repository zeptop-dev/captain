import assert from 'node:assert/strict'
import test from 'node:test'
import { matchSiteLanguage, resolveSiteLanguage, readSiteLanguage, siteLanguageStorageKey } from '../src/i18n/language.ts'

test('first visit follows supported browser languages in preference order', () => {
  assert.equal(resolveSiteLanguage(null, ['fr-FR', 'ja-JP', 'en-US']), 'ja')
  assert.equal(resolveSiteLanguage(null, ['ko-KR', 'zh-CN']), 'ko')
  assert.equal(resolveSiteLanguage(null, ['ru-RU']), 'ru')
  assert.equal(resolveSiteLanguage(null, ['en-GB']), 'en')
  assert.equal(resolveSiteLanguage(null, ['fr-FR']), 'en')
  assert.equal(resolveSiteLanguage(null, []), 'en')
})

test('Chinese script takes precedence over region with familiar regional fallbacks', () => {
  for (const tag of ['zh', 'zh-CN', 'zh-SG', 'zh-Hans-HK']) assert.equal(matchSiteLanguage(tag), 'zh-CN', tag)
  for (const tag of ['zh-TW', 'zh-HK', 'zh-MO', 'zh-Hant', 'zh-Hant-CN', 'ZH_hant_tw']) assert.equal(matchSiteLanguage(tag), 'zh-TW', tag)
})

test('manual choice wins, unknown saved choices return to browser detection', () => {
  assert.equal(resolveSiteLanguage('en', ['zh-CN']), 'en')
  assert.equal(resolveSiteLanguage('zh-TW', ['en-US']), 'zh-TW')
  assert.equal(resolveSiteLanguage('auto', ['ja-JP']), 'ja')
  assert.equal(resolveSiteLanguage('bad', ['ru-RU']), 'ru')
})

test('storage is isolated from other SPAs and blocked storage allows system detection', () => {
  const keys = []
  assert.equal(readSiteLanguage({ getItem(key) { keys.push(key); return null } }), null)
  assert.deepEqual(keys, [siteLanguageStorageKey])
  assert.notEqual(siteLanguageStorageKey, 'i18nextLng')
  assert.equal(readSiteLanguage({ getItem() { throw new Error('blocked') } }), null)
  assert.equal(readSiteLanguage({ getItem() { return 'ko' } }), 'ko')
})
