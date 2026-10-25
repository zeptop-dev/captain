import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { stripTypeScriptTypes } from 'node:module'
import { expiryLabel } from './expiry-label.ts'

const labels = {
  en: ['Expired', 'Long-term', '1 day', '2 days', '1 day left', '2 days left'],
  'zh-CN': ['已过期', '长期', '1 天', '2 天', '剩余 1 天', '剩余 2 天'],
  'zh-TW': ['已過期', '長期', '1 天', '2 天', '剩餘 1 天', '剩餘 2 天'],
  ja: ['期限切れ', '無期限', '1 日', '2 日', '残り 1 日', '残り 2 日'],
  ko: ['만료됨', '무기한', '1일', '2일', '1일 남음', '2일 남음'],
  ru: ['Истекло', 'Бессрочно', '1 день', '2 дн.', 'Остался 1 день', 'Осталось 2 дн.'],
}

for (const [locale, expected] of Object.entries(labels)) {
  test(`expiry list and detail labels keep the selected ${locale} dictionary`, async () => {
    const dict = JSON.parse(await readFile(new URL(`./i18n/${locale}.json`, import.meta.url), 'utf8'))
    const translate = key => {
      assert(Object.hasOwn(dict, key), `missing ${locale} key: ${key}`)
      return dict[key]
    }
    assert.equal(expiryLabel('expired', -2, translate), expected[0])
    assert.equal(expiryLabel('long_term', 20000, translate), expected[1])
    assert.equal(expiryLabel('critical', 1, translate), expected[2])
    assert.equal(expiryLabel('normal', 2, translate), expected[3])
    assert.equal(expiryLabel('critical', 1, translate, true), expected[4])
    assert.equal(expiryLabel('warning', 2, translate, true), expected[5])
    assert.equal(expiryLabel('unknown', 0, translate, true), '-')

    // Exercise the production expiry helper too. Only replace its browser
    // dictionary and bundler aliases; keep date parsing and formatting intact.
    const source = (await readFile(new URL('./utils/tagHelper.ts', import.meta.url), 'utf8'))
      .replace("import { t } from '@shared/i18n'", `const dict = ${JSON.stringify(dict)}; const t = (key: string) => dict[key] ?? key`)
      .replace("'@shared/expiry-label'", JSON.stringify(new URL('./expiry-label.ts', import.meta.url).href))
      .replace("'dayjs'", JSON.stringify(import.meta.resolve('dayjs')))
    const moduleURL = 'data:text/javascript;base64,' + Buffer.from(stripTypeScriptTypes(source)).toString('base64')
    const { getExpireText } = await import(moduleURL)
    assert.equal(getExpireText(Date.now() - 86400000), expected[0])
    assert.equal(getExpireText(Date.now() + 40000 * 86400000), expected[1])
    assert.equal(getExpireText(Date.now() + 3600000), expected[2])
    assert.equal(getExpireText(Date.now() + 25 * 3600000, true), expected[5])
    assert.equal(getExpireText('invalid', true), '-')
  })
}
