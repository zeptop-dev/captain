import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createInstance } from 'i18next'
import { localizeSiteDefaults } from '../src/lib/site-defaults.ts'
import { bytes, money } from '../src/lib/format.ts'

const locales = ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko']
const resources = Object.fromEntries(await Promise.all(locales.map(async locale => [locale, { translation: JSON.parse(await readFile(new URL(`../src/i18n/${locale}.json`, import.meta.url), 'utf8')) }])))

// Start with the actual backend defaults so a newly added or edited default
// cannot silently stay Chinese in otherwise translated public pages.
const goSource = await readFile(new URL('../../../internal/http/site/site.go', import.meta.url), 'utf8')
const defaults = goSource.split('func Defaults(name string) Settings {')[1].split('// Register')[0]
const baseSite = {
  name: 'Example 品牌',
  tagline: defaults.match(/Tagline: "([^"]+)"/)[1],
  description: defaults.match(/Description: "([^"]+)"/)[1],
  features: [...defaults.matchAll(/\{Title: "([^"]+)", Text: "([^"]+)", Icon: "([^"]+)"\}/g)].map(([, title, text, icon]) => ({ title, text, icon })),
  faq: [...defaults.matchAll(/\{Q: "([^"]+)", A: "([^"]+)"\}/g)].map(([, q, a]) => ({ q, a })),
  locations: [{ name: 'Custom location', lat: 10, lng: 20 }],
  hub: null, links: {}, registration: true, show_plans: true,
  stats: { nodes: 1, online: 1, users: 0, locations: 1 },
}
const original = JSON.stringify(baseSite)
const freeze = value => {
  if (value && typeof value === 'object') { Object.values(value).forEach(freeze); Object.freeze(value) }
  return value
}
freeze(baseSite)

for (const locale of locales) {
  test(`homepage defaults have complete ${locale} translations without modifying source settings`, async () => {
    const i18n = createInstance()
    await i18n.init({ lng: locale, fallbackLng: false, resources, interpolation: { escapeValue: false } })
    const translated = localizeSiteDefaults(baseSite, key => {
      assert.ok(i18n.exists(key), `missing ${locale}: ${key}`)
      return i18n.t(key)
    })
    assert.equal(translated.name, baseSite.name)
    assert.strictEqual(translated.locations, baseSite.locations)
    assert.equal(translated.features.length, baseSite.features.length)
    assert.equal(translated.faq.length, baseSite.faq.length)
    if (locale !== 'zh-CN') {
      assert.notEqual(translated.tagline, baseSite.tagline)
      assert.notEqual(translated.description, baseSite.description)
      translated.features.forEach((item, index) => {
        assert.notEqual(item.title, baseSite.features[index].title)
        assert.notEqual(item.text, baseSite.features[index].text)
      })
      translated.faq.forEach((item, index) => {
        assert.notEqual(item.q, baseSite.faq[index].q)
        assert.notEqual(item.a, baseSite.faq[index].a)
      })
    }
    for (const count of [1, 2, 5, 21, 1000]) {
      for (const key of ['home.plans.days', 'home.plans.devices']) {
        const text = i18n.t(key, { count })
        assert.notEqual(text, key)
        assert.ok(text.includes(count.toLocaleString(locale)))
        assert.ok(!text.includes('{{'))
      }
    }
    assert.equal(JSON.stringify(baseSite), original)
  })
}

test('custom content and partially edited defaults are never rewritten', () => {
  const custom = freeze({
    ...baseSite,
    tagline: `${baseSite.tagline} `,
    description: 'Custom description',
    features: [
      { ...baseSite.features[0], text: 'Custom protocol description' },
      { ...baseSite.features[1], title: 'Custom feature title' },
    ],
    faq: [{ ...baseSite.faq[0], a: 'Custom FAQ answer' }],
  })
  const translated = localizeSiteDefaults(custom, () => { throw new Error('custom content must not be translated') })
  assert.deepEqual(translated, custom)
  assert.notStrictEqual(translated, custom)
})

test('English and Russian quantities select native plural forms', async () => {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', fallbackLng: false, resources })
  assert.equal(i18n.t('home.plans.days', { count: 1 }), '1 day')
  assert.equal(i18n.t('home.plans.days', { count: 2 }), '2 days')
  assert.equal(i18n.t('home.plans.devices', { count: 1 }), '1 device')
  assert.equal(i18n.t('home.plans.devices', { count: 2 }), '2 devices')
  await i18n.changeLanguage('ru')
  for (const [count, days, devices] of [[1, 'день', 'устройство'], [2, 'дня', 'устройства'], [5, 'дней', 'устройств'], [21, 'день', 'устройство']]) {
    assert.equal(i18n.t('home.plans.days', { count }), `${count} ${days}`)
    assert.equal(i18n.t('home.plans.devices', { count }), `${count} ${devices}`)
  }
})

test('localized amounts keep the same currency and binary traffic units', () => {
  assert.equal(money(123456, '¥', 'en'), '¥1,234.56')
  assert.equal(money(123456, '¥', 'ru'), '¥1\u00a0234,56')
  assert.equal(bytes(1536, 'en'), '1.50 KiB')
  assert.equal(bytes(1536, 'ru'), '1,50 KiB')
  assert.equal(money(123456), '¥1234.56')
  assert.equal(bytes(1536), '1.50 KiB')
})
