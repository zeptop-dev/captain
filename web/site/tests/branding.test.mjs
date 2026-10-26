import assert from 'node:assert/strict'
import test from 'node:test'
import { brandLogo, safeBrandAssetURL, applyBrandMetadata, defaultBrandFavicon } from '../../shared/branding.ts'

test('logos select the displayed scheme with a safe alternate and built-in fallback', () => {
  const branding = { logo_light: '/brand-assets/light.png', logo_dark: 'https://example.com/dark.png' }
  assert.equal(brandLogo(branding, 'light'), branding.logo_light)
  assert.equal(brandLogo(branding, 'dark'), branding.logo_dark)
  assert.equal(brandLogo({ logo_dark: branding.logo_dark }, 'light'), branding.logo_dark)
  assert.equal(brandLogo({ logo_light: 'javascript:alert(1)', logo_dark: branding.logo_dark }, 'light'), branding.logo_dark)
  assert.equal(brandLogo(undefined, 'dark'), '')
})

test('brand images reject executable, cross-protocol, credentialed and malformed URLs', () => {
  for (const value of ['javascript:alert(1)', 'data:image/svg+xml,<svg/>', '//example.com/x.png', '/\\example.com/x.png', 'https://example.com/\nx.png', 'https://user:pass@example.com/x.png', 'not-an-url', ' https://example.com/logo.png ', '/%XX.png', '/%2fexample.com/logo.png', '/logo%00.png', 'https:example.com/logo.png', 'https://@example.com/logo.png']) assert.equal(safeBrandAssetURL(value), '')
  for (const value of ['/brand-assets/mark.png', 'https://example.com/mark.png', 'http://example.com/mark.png']) assert.equal(safeBrandAssetURL(value), value)
})

// A tiny document fixture exercises real metadata lifecycle semantics without a
// browser framework dependency: replacement, clearing and effect cleanup.
function documentFixture() {
  const head = {
    children: [],
    append(node) { node.parentNode = this; this.children.push(node) },
    insertBefore(node, next) { node.parentNode = this; const index = this.children.indexOf(next); this.children.splice(index < 0 ? this.children.length : index, 0, node) },
    querySelectorAll(selector) { return this.children.filter(node => (selector.includes('meta') && node.tagName === 'meta' && node.name === 'description') || (selector.includes('link') && node.tagName === 'link' && node.rel.split(' ').includes('icon'))) },
  }
  return {
    title: 'Original', head,
    createElement(tagName) {
      return {
        tagName, parentNode: null,
        get nextSibling() { const nodes = this.parentNode?.children || []; return nodes[nodes.indexOf(this) + 1] || null },
        remove() { if (this.parentNode) this.parentNode.children.splice(this.parentNode.children.indexOf(this), 1); this.parentNode = null },
      }
    },
  }
}

test('metadata follows the brand, clears removed values and restores the document on cleanup', () => {
  const oldDocument = globalThis.document
  const fixture = documentFixture()
  globalThis.document = fixture
  try {
    const oldIcon = fixture.createElement('link'); oldIcon.rel = 'icon'; oldIcon.href = '/original.png'; fixture.head.append(oldIcon)
    const oldDescription = fixture.createElement('meta'); oldDescription.name = 'description'; oldDescription.content = 'Original description'; fixture.head.append(oldDescription)
    const shortcutIcon = fixture.createElement('link'); shortcutIcon.rel = 'shortcut icon'; shortcutIcon.href = '/shortcut.png'; fixture.head.append(shortcutIcon)
    const clean = applyBrandMetadata({ name: 'Example', title: 'Example Network', description: '<b>Literal text</b>', favicon: '/brand-assets/logo.png' })
    assert.equal(fixture.title, 'Example Network')
    assert.deepEqual(fixture.head.children.map(node => node.content || node.href), ['<b>Literal text</b>', '/brand-assets/logo.png'])
    fixture.head.children[1].onerror()
    assert.equal(fixture.head.children[1].href, defaultBrandFavicon)
    clean()
    assert.equal(fixture.title, 'Original')
    assert.deepEqual(fixture.head.children, [oldIcon, oldDescription, shortcutIcon])
    const cleanEmpty = applyBrandMetadata({ name: 'New name', title: '', description: '', favicon: '' })
    assert.equal(fixture.title, 'New name')
    assert.equal(fixture.head.children.length, 1)
    assert.equal(fixture.head.children[0].href, defaultBrandFavicon)
    cleanEmpty()
    assert.deepEqual(fixture.head.children, [oldIcon, oldDescription, shortcutIcon])
  } finally {
    globalThis.document = oldDocument
  }
})
