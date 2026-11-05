import { test } from 'node:test'
import assert from 'node:assert/strict'
import { localizedName, monitorGroupOptions } from '../src/lib/localized-name.ts'

test('Chinese locales use Chinese names; every other locale uses English names', () => {
  for (const language of ['zh', 'zh-CN', 'zh-TW', 'zh-Hans', 'zh-Hant-TW', 'ZH_hk']) {
    assert.equal(localizedName(language, '亚洲', 'Asia', 'legacy'), '亚洲')
  }
  for (const language of ['en', 'en-US', 'ja', 'ko', 'ru', 'fr-FR', '']) {
    assert.equal(localizedName(language, '亚洲', 'Asia', 'legacy'), 'Asia')
  }
})

test('a missing language falls back to the other name and then the legacy value', () => {
  assert.equal(localizedName('en', '中文', undefined, 'legacy'), '中文')
  assert.equal(localizedName('zh-TW', '', 'English', 'legacy'), 'English')
  assert.equal(localizedName('ja', undefined, '  ', 'legacy'), 'legacy')
  assert.equal(localizedName('zh-CN', '  中文  ', 'English', 'legacy'), '中文')
  assert.equal(localizedName('en', undefined, undefined), '')
})


test('monitor filters retain exact stable identities for equal labels and punctuation', () => {
  const nodes = [
    { group: 'west;region', group_zh: '西区', group_en: 'Same, label | ;' },
    { group: 'east,region|pool', group_zh: '东区', group_en: 'Same, label | ;' },
    { group: 'west;region', group_zh: '西区', group_en: 'Same, label | ;' },
    { group: 'legacy' },
    {},
  ]
  assert.deepEqual(monitorGroupOptions(nodes, 'en'), [
    { value: 'west;region', label: 'Same, label | ;' },
    { value: 'east,region|pool', label: 'Same, label | ;' },
    { value: 'legacy', label: 'legacy' },
  ])
  assert.deepEqual(monitorGroupOptions(nodes, 'zh-Hant').map(option => option.label), ['西区', '东区', 'legacy'])
})
