import assert from 'node:assert/strict'
import test from 'node:test'
import { summarizeClusters } from './composables/geoClusters.ts'
import { snapshot, clientFor } from './captain.ts'
import { locateGeo } from '../../shared/geolocation.ts'

const city = (name, lat, lng) => ({ country: 'US', city: name, lat, lng, precision: 'city' })
const node = (location, online = true) => ({ region: 'US', location, online })

test('two cities in one country stay separate; only identical locations cluster', () => {
  const west = city('Los Angeles', 34.05, -118.24), east = city('New York', 40.71, -74)
  const { clusters, totalServers, onlineServers } = summarizeClusters([node(west), node(east), node(west, false)])
  assert.equal(clusters.length, 2)
  assert.equal(totalServers, 3)
  assert.equal(onlineServers, 2)
  assert.deepEqual(clusters[0].coord, [34.05, -118.24])
  assert.equal(clusters[0].servers, 2)
  assert.equal(clusters[0].onlineServers, 1)
  assert.equal(clusters[0].precision, 'city')
  assert.match(clusters[0].label, /Los Angeles/)
})

test('hidden or unknown location stays hidden; only missing old fields fall back to country', () => {
  const { clusters } = summarizeClusters([node(null), node(undefined), node({country:'US',precision:'country'})])
  assert.equal(clusters.length, 1)
  assert.equal(clusters[0].servers, 2)
  assert.equal(clusters[0].precision, 'country')
  assert.deepEqual(summarizeClusters([node(null)]).clusters, [])
})

test('zero coordinates are valid; invalid city coordinates and unknown countries do not invent positions', () => {
  assert.deepEqual(locateGeo(city('Zero', 0, 0)), city('Zero', 0, 0))
  for (const invalid of [city('Bad', 91, 0), city('Bad', 0, 181), city('Bad', NaN, 0), city('Bad', Infinity, 0), city('', 10, 10), {country:'__proto__',precision:'country'}, {country:'ZZ',precision:'country'}]) assert.equal(locateGeo(invalid), null)
  const country = locateGeo({ country:'US', precision:'country', city:'Stale city', lat:0, lng:0 })
  assert.equal(country.city, undefined)
  assert.notEqual(country.lng, 0)
})

test('Captain adapter preserves null and old missing semantics and obeys public info visibility', () => {
  const n = { id: 1, name: 'Example', info: {region:'US',location:city('Example city',0,0)}, host:null, traffic:{}, last_seen:null }
  snapshot.value = {public_sections:['info'],nodes:[n]}
  assert.deepEqual(clientFor(n, 0).location, n.info.location)
  n.info.location = null
  assert.equal(clientFor(n, 0).location, null)
  delete n.info.location
  assert.equal(clientFor(n, 0).location, undefined)
  snapshot.value.public_sections = ['system']
  assert.equal(clientFor(n, 0).location, null)
})
