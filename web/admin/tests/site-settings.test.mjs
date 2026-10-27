import assert from 'node:assert/strict'
import test from 'node:test'
import { siteValues, sitePayload, missingMapEntryIDs } from '../src/components/site/site-form.ts'

const settings = {
  homepage_enabled: true,
  map_revision: 17, map_source: 'entries', status_map_source: 'manual', map_entry_ids: [11, 22],
  branding: { logo_light: 'https://example.com/light.png', logo_dark: 'https://example.com/dark.png', favicon: 'https://example.com/icon.png', title: 'Example tab' },
  name: 'Example', tagline: 'A tagline', description: 'Custom description', show_plans: false,
  links: { telegram: 'https://example.com/chat', tos: 'https://example.com/terms', download: 'https://example.com/download', github: 'https://example.com/source' },
  hub: { name: 'Hub', lat: 0, lng: 135 }, locations: [{ name: 'Point', lat: 35, lng: 0, tag: 'POP' }],
  features: [{ title: 'Custom feature', text: 'Keep this text', icon: 'world' }], faq: [{ q: 'Custom question', a: 'Custom answer' }],
  theme: { primary: 'teal', radius: 'sm', scheme: 'dark', site_scheme: 'auto', font_family: 'sans-serif', portal_title: 'Members' },
  inject_head: '<meta name="example" content="head">', inject_body: '<div id="example-widget"></div>',
}

test('reorganized site editor round-trips the full existing settings contract', () => {
  assert.deepEqual(sitePayload(siteValues(settings)), settings)
})

test('editing homepage copy preserves closed advanced values and the hidden probe theme', () => {
  const draft = siteValues(settings)
  draft.tagline = 'Updated tagline'
  const saved = sitePayload(draft)
  assert.deepEqual(saved, { ...settings, tagline: 'Updated tagline' })
  assert.equal(saved.theme.site_scheme, 'auto')
  assert.equal(saved.map_revision, 17)
})

test('legacy homepage settings stay enabled and saves write the effective boolean', () => {
  const input = structuredClone(settings)
  delete input.homepage_enabled
  assert.equal(siteValues(input).homepage_enabled, true)
  assert.deepEqual(sitePayload(siteValues(input)), settings)
})

test('disabling and reenabling the homepage preserves its draft and shared configuration', () => {
  const draft = siteValues(settings)
  draft.tagline = 'Draft retained while disabled'
  draft.homepage_enabled = false
  const disabled = sitePayload(draft)
  assert.deepEqual(disabled, { ...settings, homepage_enabled: false, tagline: draft.tagline })
  const reloaded = siteValues(disabled)
  assert.equal(reloaded.homepage_enabled, false)
  reloaded.homepage_enabled = true
  assert.deepEqual(sitePayload(reloaded), { ...settings, tagline: draft.tagline })
})

test('legacy missing public scheme is not silently replaced when saving another section', () => {
  const input = structuredClone(settings)
  delete input.theme.site_scheme
  const saved = JSON.parse(JSON.stringify(sitePayload(siteValues(input))))
  assert.equal('site_scheme' in saved.theme, false)
})

test('missing map ids require explicit removal even when the manual source is selected', () => {
  const original = { ...settings, map_source: 'manual', map_entry_ids: [11, 99] }
  const draft = siteValues(original)
  assert.deepEqual(missingMapEntryIDs(draft.map_entry_ids, [{ ID: 11 }]), [99])
  assert.deepEqual(sitePayload(draft).map_entry_ids, [11, 99])
  draft.map_entry_ids.splice(1, 1)
  assert.deepEqual(missingMapEntryIDs(draft.map_entry_ids, [{ ID: 11 }]), [])
  assert.deepEqual(original.map_entry_ids, [11, 99])
  assert.deepEqual(missingMapEntryIDs([11], undefined), [11])
})

test('folded JSON remains real configuration and invalid JSON cannot produce a partial payload', () => {
  const draft = siteValues(settings)
  draft.hub = ' '
  assert.equal(sitePayload(draft).hub, null)
  for (const field of ['features', 'faq', 'locations', 'hub']) {
    const invalid = { ...draft, [field]: '{' }
    assert.throws(() => sitePayload(invalid), SyntaxError)
  }
})
