import assert from 'node:assert/strict'
import test from 'node:test'
import { locationDraft, locationDraftError, locationSettings } from '../src/lib/location.ts'

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
