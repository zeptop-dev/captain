import { test } from 'node:test'
import assert from 'node:assert/strict'
import { resolveSiteAppearance, siteColorSchemeManager } from '../src/appearance.ts'
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

test('public auto mode ignores persisted choices and never changes portal preferences', () => {
  const old = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, get() { throw new Error('Public appearance must not access visitor theme storage') } })
  try {
    const manager = siteColorSchemeManager('auto')
    assert.equal(manager.get(), 'auto')
    manager.set('dark')
    manager.clear()
    assert.equal(manager.get(), 'auto')
    assert.equal(siteColorSchemeManager('light').get(), 'light')
  } finally {
    if (old) Object.defineProperty(globalThis, 'localStorage', old)
    else delete globalThis.localStorage
  }
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
