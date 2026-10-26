import assert from 'node:assert/strict'
import test from 'node:test'
import { manualStatusMap, mapLocations, statusMapLocations } from '../src/lib/map-locations.ts'

const west = {country:'US',city:'Los Angeles',lat:34.05,lng:-118.24,precision:'city'}
const east = {country:'US',city:'New York',lat:40.71,lng:-74,precision:'city'}

test('map metadata resolves country approximations without changing manual coordinates', () => {
  const manual = {name:'Manual zero',lat:0,lng:0}
  const source = [manual, {name:'West',...west}, {name:'Country',country:'US',precision:'country'}]
  const copy = structuredClone(source)
  const points = mapLocations(source)
  assert.equal(points.length, 3)
  assert.deepEqual(points[0], manual)
  assert.deepEqual(points[1], source[1])
  assert.equal(points[2].precision, 'country')
  assert.ok(Number.isFinite(points[2].lat))
  assert.notEqual(points[2].lng, west.lng)
  assert.deepEqual(source, copy)
  assert.deepEqual(mapLocations([{name:'Bad',lat:NaN,lng:10}, {name:'Missing',lat:0}, {name:'Bad city',...west,lng:999}]), [])
})

test('status maps use only visible entries and their exit locations, never the flag or hidden inventory', () => {
  const entries = [
    {id:1,name:'Front to west',region:'HK',location:west},
    {id:2,name:'East',region:'HK',location:east},
    {id:3,name:'Hidden location',region:'JP',location:null},
    {id:4,name:'Old status API',region:'US'},
  ]
  const points = statusMapLocations(entries)
  assert.deepEqual(points.map(point => [point.name,point.lat,point.lng]), [['Front to west',34.05,-118.24],['East',40.71,-74]])
  assert.deepEqual(statusMapLocations(entries.slice(1,2)).map(point => point.city), ['New York'])
  assert.deepEqual(statusMapLocations([]), [])
})

test('explicit empty status map settings never revive homepage entry locations', () => {
  const locations = [{name:'Homepage entry',...west}], hub = {name:'Old hub',lat:10,lng:20}
  assert.deepEqual(manualStatusMap({locations,hub}), {locations,hub})
  assert.deepEqual(manualStatusMap({locations,hub,status_locations:[],status_hub:null}), {locations:[],hub:null})
  assert.deepEqual(manualStatusMap({locations,hub,status_locations:null,status_hub:null}), {locations:[],hub:null})
  assert.deepEqual(manualStatusMap(), {locations:[],hub:null})
})
