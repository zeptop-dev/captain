import { test } from 'node:test'
import assert from 'node:assert/strict'
import { resolveSiteAppearance, effectiveSiteScheme, parseSitePreference, readSitePreference, siteSchemeStorageKey } from '../src/appearance.ts'
import { globePalette } from '../src/components/geography-theme.ts'

test('operator palette and typography remain independent of retired scheme settings', () => {
  const expected = { primary: 'teal', radius: 'sm', font: 'Example Font' }
  for (const site_scheme of [undefined, 'light', 'dark', 'auto']) {
    assert.deepEqual(resolveSiteAppearance({ site_scheme, scheme: 'dark', primary: 'teal', radius: 'sm', font_family: 'Example Font' }), expected)
  }
  assert.equal(resolveSiteAppearance({ primary: 'not-a-palette', radius: 'invalid' }).primary, undefined)
  assert.equal(resolveSiteAppearance({ primary: 'not-a-palette', radius: 'invalid' }).radius, undefined)
})

test('new visitors and legacy automatic preferences use light; explicit choices survive', () => {
  for (const stored of [null, 'auto', '', 'system', 'invalid']) {
    assert.equal(effectiveSiteScheme(parseSitePreference(stored)), 'light')
  }
  for (const stored of ['dark', 'light']) {
    assert.equal(effectiveSiteScheme(parseSitePreference(stored)), stored)
  }
})

test('only the public-site key is read; blocked storage falls back to light', () => {
  const values = new Map([['mantine-color-scheme-value', 'dark'], ['themeMode', 'dark']])
  const storage = { getItem(key) { assert.equal(key, siteSchemeStorageKey); return values.get(key) ?? null } }
  assert.equal(readSitePreference(storage), null)
  for (const preference of ['dark', 'light']) {
    values.set(siteSchemeStorageKey, preference)
    assert.equal(effectiveSiteScheme(readSitePreference(storage)), preference)
  }
  for (const invalid of ['', 'auto', 'system', 'null', '"dark"']) {
    values.set(siteSchemeStorageKey, invalid)
    assert.equal(effectiveSiteScheme(readSitePreference(storage)), 'light')
  }
  assert.equal(effectiveSiteScheme(readSitePreference({ getItem() { throw new Error('blocked') } })), 'light')
  assert.equal(values.get('mantine-color-scheme-value'), 'dark')
  assert.equal(values.get('themeMode'), 'dark')
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
