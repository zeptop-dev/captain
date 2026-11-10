import assert from 'node:assert/strict'
import test from 'node:test'
import { nodeLocationDraft, locationDraft, locationDraftError, locationSettings } from '../src/lib/location.ts'

test('legacy location settings inherit and do not invent coordinates', () => {
  const draft = locationDraft()
  assert.equal(draft.mode, 'auto')
  assert.equal(draft.lat, '')
  assert.equal(draft.lng, '')
  assert.deepEqual(locationSettings(draft), { mode: 'auto' })
})

test('city-only and partially entered coordinates cannot be published as a city position', () => {
  const draft = { ...locationDraft(), mode: 'manual', country: 'US', city: 'Example City' }
  assert.equal(locationDraftError(draft), 'cityCoordinatesRequired')
  assert.equal(locationDraftError({ ...draft, lat: 0 }), 'cityCoordinatesRequired')
  assert.equal(locationDraftError({ ...draft, city: '', lat: 0, lng: 0 }), 'cityCoordinatesRequired')
  assert.equal(locationDraftError({ ...draft, lat: 91, lng: 0 }), 'invalidCoordinates')
  assert.equal(locationDraftError({ ...draft, lat: 0, lng: -181 }), 'invalidCoordinates')
  assert.equal(locationDraftError({ ...draft, lat: 'invalid', lng: 0 }), 'invalidCoordinates')
  assert.equal(locationDraftError({ ...draft, lat: 0, lng: 0 }), null)
})

test('country-only positions remain approximate and contain no fabricated city or coordinates', () => {
  const draft = { ...locationDraft(), mode: 'manual', country: 'us', subdivision: 'Example state' }
  assert.equal(locationDraftError(draft), null)
  const settings = locationSettings(draft)
  assert.equal(settings.manual.country, 'US')
  assert.equal(settings.manual.precision, 'country')
  assert.equal(settings.manual.subdivision, 'Example state')
  assert.equal(settings.manual.lat, undefined)
  assert.equal(settings.manual.lng, undefined)
  assert.equal(settings.manual.city, undefined)
})

test('switching auto/off retains a complete manual correction including zero coordinates', () => {
  const manual = { country: 'US', city: 'Example City', lat: 0, lng: 0, precision: 'city' }
  const saved = { mode: 'manual', manual }
  const draft = locationDraft(saved)
  for (const mode of ['auto', 'off']) {
    const settings = locationSettings({ ...draft, mode })
    assert.equal(settings.mode, mode)
    assert.equal(settings.manual.lat, 0)
    assert.equal(settings.manual.lng, 0)
    assert.equal(settings.manual.city, manual.city)
  }
  draft.city = 'Changed'
  assert.equal(saved.manual.city, 'Example City')
})


test('saving an older server preserves an explicit map opt-out without changing entry inheritance', () => {
  for (const missing of [undefined, null]) {
    assert.deepEqual(locationSettings(nodeLocationDraft(missing, 'off')), { mode: 'off' })
    assert.deepEqual(locationSettings(nodeLocationDraft(missing, 'auto')), { mode: 'auto' })
    assert.deepEqual(locationSettings(nodeLocationDraft(missing, 'manual')), { mode: 'auto' })
    assert.deepEqual(locationSettings(locationDraft(missing)), { mode: 'auto' })
  }
  assert.equal(nodeLocationDraft({ mode: 'auto' }, 'off').mode, 'auto')
  assert.equal(nodeLocationDraft({ mode: 'off' }, 'auto').mode, 'off')
})

const { cityLocationDraft, automaticLocation, locationsDiffer } = await import('../src/lib/location.ts')
const { locationCities } = await import('../src/lib/location-cities.ts')
test('city reference points generate complete manual drafts without network evidence', () => {
  assert.equal(new Set(locationCities.map(city => city.id)).size, locationCities.length)
  for (const city of locationCities) {
    const result = cityLocationDraft(city)
    assert.equal(result.mode, 'manual')
    assert.equal(result.subdivision, '')
    assert.equal(locationDraftError(result), null)
    assert.equal(locationSettings(result).manual.precision, 'city')
  }
  const london = locationCities.find(city => city.city === 'London')
  assert.equal(london.country, 'GB')
  assert.equal(cityLocationDraft(london).country, 'GB')
})
test('automatic evidence remains separate from a manual correction and missing data', () => {
  assert.equal(automaticLocation(), null)
  const auto = automaticLocation({ country: 'US' })
  assert.deepEqual(auto, { country: 'US', precision: 'country' })
  assert.equal(locationsDiffer(auto, { country: 'GB', city: 'London', precision: 'city' }), true)
  assert.equal(locationsDiffer(null, { country: 'GB', precision: 'country' }), false)
  assert.equal(locationsDiffer({ country: 'GB', precision: 'country' }, { country: 'GB', city: 'London', precision: 'city' }), false)
  assert.equal(locationsDiffer({ country: 'GB', city: 'London', precision: 'city' }, { country: 'GB', city: 'Manchester', precision: 'city' }), true)
  assert.equal(auto.country, 'US')
})
