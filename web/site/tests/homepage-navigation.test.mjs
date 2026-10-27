import assert from 'node:assert/strict'
import test from 'node:test'
import { brandDestination, navigation, portalEntryURL } from '../src/lib/site-navigation.ts'

const site = { features: [{}], faq: [{}], links: {}, show_plans: true }
const t = key => key
const links = value => navigation(t, value, true).footer.flatMap(group => group.links)

test('missing homepage setting keeps section destinations in the footer', () => {
  assert.equal(brandDestination(site), '/')
  for (const href of ['/#coverage', '/#platform', '/#features', '/#faq', '/#plans']) {
    assert.ok(links(site).some(link => link.href === href), href)
  }
})

test('disabled homepage removes section links and empty groups, retaining standalone pages', () => {
  const disabled = { ...site, homepage_enabled: false, service_status_url: '/service-status', probe_url: '/status', links: { telegram: 'https://example.com/contact', tos: 'https://example.com/terms' } }
  assert.equal(brandDestination(disabled), '/portal/')
  assert.ok(!links(disabled).some(link => link.href === '/' || link.href.startsWith('/#')))
  for (const href of ['/downloads', '/portal/plans', '/portal/connect', '/service-status', 'https://example.com/contact', 'https://example.com/terms']) {
    assert.ok(links(disabled).some(link => link.href === href), href)
  }
  assert.ok(!links(disabled).some(link => link.href === '/status' || link.href === '/portal/account'))
  assert.ok(!navigation(t, { ...site, homepage_enabled: false }).footer.some(group => group.id === 'links'))
  const optional = navigation(t, { ...site, probe_url: '/status' })
  assert.deepEqual(optional.primary.map(item => item.id), ['plans', 'support'])
  const configured = navigation(t, { ...disabled, links: { download: 'https://example.com/download' } }, true)
  assert.equal(configured.primary.find(item => item.id === 'downloads').href, 'https://example.com/download')
  assert.equal(configured.primary.find(item => item.id === 'status').href, '/service-status')
})

test('client root redirect only carries an encoded invitation', () => {
  assert.equal(portalEntryURL('?ref=A%26B&next=https://example.com'), '/portal/?ref=A%26B')
  assert.equal(portalEntryURL('?ref=%20&error=oops'), '/portal/')
})
