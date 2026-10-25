import { test } from 'node:test'
import assert from 'node:assert/strict'
import { resolveSiteAppearance, effectiveSiteScheme, parseSitePreference, readSitePreference, siteSchemeStorageKey } from '../src/appearance.ts'
import { globePalette } from '../src/components/geography-theme.ts'

test('landing appearance follows its own setting, independently of the portal', () => {
  assert.equal(resolveSiteAppearance({ scheme: 'light' }).scheme, 'dark')
  for (const scheme of ['light', 'dark', 'auto']) {
    assert.equal(resolveSiteAppearance({ site_scheme: scheme, scheme: 'dark' }).scheme, scheme)
  }
  assert.equal(resolveSiteAppearance({ site_scheme: 'invalid' }).scheme, 'dark')
  assert.deepEqual(resolveSiteAppearance({ site_scheme: 'light', primary: 'teal', radius: 'sm', font_family: 'Example Font' }), { scheme: 'light', primary: 'teal', radius: 'sm', font: 'Example Font' })
  assert.equal(resolveSiteAppearance({ primary: 'not-a-palette', radius: 'invalid' }).primary, undefined)
  assert.equal(resolveSiteAppearance({ primary: 'not-a-palette', radius: 'invalid' }).radius, undefined)
})

test('visitor choices override operator defaults and auto follows the system', () => {
  for (const configured of ['light', 'dark', 'auto']) {
    for (const systemDark of [false, true]) {
      for (const preference of [null, 'light', 'dark', 'auto']) {
        const selected = preference ?? configured
        assert.deepEqual(effectiveSiteScheme(preference, configured, systemDark), {
          selected, computed: selected === 'auto' ? (systemDark ? 'dark' : 'light') : selected,
        })
      }
    }
  }
  // Late operator data cannot replace a visitor's explicit current-page choice.
  assert.deepEqual(effectiveSiteScheme('light', 'dark', true), effectiveSiteScheme('light', 'auto', true))
})

test('only the public-site key is read; blocked, absent and invalid storage use the operator default', () => {
  const values = new Map([['mantine-color-scheme-value', 'light'], ['themeMode', 'light']])
  const storage = { getItem(key) { assert.equal(key, siteSchemeStorageKey); return values.get(key) ?? null } }
  assert.equal(readSitePreference(storage), null)
  for (const preference of ['auto', 'dark', 'light']) {
    values.set(siteSchemeStorageKey, preference)
    assert.equal(readSitePreference(storage), preference)
  }
  for (const invalid of ['', 'system', 'null', '"dark"']) {
    values.set(siteSchemeStorageKey, invalid)
    assert.equal(readSitePreference(storage), null)
  }
  assert.equal(readSitePreference({ getItem() { throw new Error('blocked') } }), null)
  assert.equal(parseSitePreference(null), null)
  assert.equal(values.get('mantine-color-scheme-value'), 'light')
  assert.equal(values.get('themeMode'), 'light')
})

test('globe labels and markers retain contrast on both ocean palettes', () => {
  const luminance = rgb => rgb.map(x => x / 255).map(x => x <= .04045 ? x / 12.92 : ((x + .055) / 1.055) ** 2.4).reduce((v, x, i) => v + x * [.2126, .7152, .0722][i], 0)
  const hex = value => value.slice(1).match(/../g).map(x => parseInt(x, 16))
  const contrast = (a, b) => { const values = [luminance(a), luminance(b)].sort((a, b) => b - a); return (values[0] + .05) / (values[1] + .05) }
  for (const scheme of ['light', 'dark']) {
    const colors = globePalette(scheme)
    const background = colors.labelBackground.match(/[\d.]+/g).slice(0, 3).map(Number)
    assert.ok(contrast(hex(colors.label), background) >= 4.5, `${scheme} label contrast`)
    for (const ocean of colors.ocean) assert.ok(contrast(hex(colors.marker), hex(ocean)) >= 3, `${scheme} marker contrast`)
  }
})
