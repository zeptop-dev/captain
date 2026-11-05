import assert from 'node:assert/strict'
import test from 'node:test'
import { localizedName, localizedNameDraft } from '../src/lib/localized-name.ts'

test('Chinese locales prefer Chinese labels; every other locale prefers English', () => {
  for (const language of ['zh', 'zh-CN', 'zh-TW', 'zh_Hant']) assert.equal(localizedName(language, '亚洲', 'Asia', 'legacy'), '亚洲')
  for (const language of ['en', 'en-US', 'ja', 'ko', 'ru', 'fr']) assert.equal(localizedName(language, '亚洲', 'Asia', 'legacy'), 'Asia')
})

test('single-language names and legacy names remain visible in every language', () => {
  for (const language of ['zh-CN', 'zh-TW', 'en', 'ja', 'ko', 'ru']) {
    assert.equal(localizedName(language, ' 亚洲 ', '', 'legacy'), '亚洲')
    assert.equal(localizedName(language, '', ' Asia ', 'legacy'), 'Asia')
    assert.equal(localizedName(language, undefined, undefined, 'legacy'), 'legacy')
    assert.equal(localizedName(language, ' ', ' ', 'legacy'), 'legacy')
  }
})

test('editing preserves an intentionally empty translation and legacy text without guessing its language', () => {
  assert.deepEqual(localizedNameDraft('', 'Asia', 'Asia'), { zh: '', en: 'Asia' })
  assert.deepEqual(localizedNameDraft('亚洲', '', '亚洲'), { zh: '亚洲', en: '' })
  assert.deepEqual(localizedNameDraft('亚洲', 'Asia', 'legacy'), { zh: '亚洲', en: 'Asia' })
  assert.deepEqual(localizedNameDraft(undefined, undefined, 'legacy'), { zh: 'legacy', en: '' })
})
