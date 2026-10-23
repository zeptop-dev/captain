import assert from 'node:assert/strict'
import test from 'node:test'
import { designCatalogue, designPolicyCanDirect, designSupportReasons, renameDesignGroup, selectedDesignFormats } from '../src/lib/sub-design.ts'

test('renaming a policy retains imported semantics and updates enhancement references', () => {
  const design = {
    groups: [
      { name: 'Proxy', type: 'url-test', members: ['@match:HK|hk'], timeout: 5, tolerance: 50, match_case_sensitive: true },
      { name: 'Final', type: 'select', members: ['Proxy'], future_field: { enabled: true } },
    ],
    rules: [{ set: 'official-1', policy: 'Proxy' }],
    final: 'Proxy',
    rule_sets: [{ key: 'official-1', name: 'Imported', url: 'https://example.com/rules.list' }],
    source: { key: 'acl4ssr-example', name: 'Example', revision: '1234' },
    enhancements: { claude: true, claude_policy: 'Proxy', dns_policy: 'Proxy', future_option: true },
  }
  const renamed = renameDesignGroup(design, 0, 'Renamed')
  assert.equal(design.groups[0].name, 'Proxy')
  assert.deepEqual(renamed.groups[1].members, ['Renamed'])
  assert.deepEqual(renamed.groups[1].future_field, { enabled: true })
  assert.equal(renamed.groups[0].timeout, 5)
  assert.equal(renamed.groups[0].tolerance, 50)
  assert.equal(renamed.groups[0].match_case_sensitive, true)
  assert.equal(renamed.rules[0].policy, 'Renamed')
  assert.equal(renamed.final, 'Renamed')
  assert.deepEqual(renamed.enhancements, { claude: true, claude_policy: 'Renamed', dns_policy: 'Renamed', future_option: true })
  assert.deepEqual(renamed.rule_sets, design.rule_sets)
  assert.deepEqual(renamed.source, design.source)
})

test('format selection never reapplies an unsupported format after options change', () => {
  const support = [
    { format: 'clash', supported: true, reasons: [] },
    { format: 'stash', supported: false, reasons: ['DNS options are unavailable'] },
    { format: 'surge', supported: true, reasons: [] },
  ]
  assert.deepEqual(selectedDesignFormats(support, null), ['clash', 'surge'])
  assert.deepEqual(selectedDesignFormats(support, ['stash', 'clash']), ['clash'])
  assert.deepEqual(selectedDesignFormats(support, []), [])
  assert.deepEqual(selectedDesignFormats([], ['clash']), [])
})

test('imported and unknown rule keys remain visible without duplicate select options', () => {
  const catalogue = [{ key: 'ads', name: 'Ads' }, { key: 'cn', name: 'China' }]
  const design = {
    groups: [], final: '',
    rules: [{ set: 'ads', policy: 'REJECT' }, { set: 'custom-inline', policy: 'DIRECT' }, { set: 'missing', policy: 'DIRECT' }],
    rule_sets: [{ key: 'ads', name: 'Imported Ads', url: 'https://example.com/ads.list' }, { key: 'custom-inline', name: 'Inline China', inline: 'GEOIP,CN' }],
  }
  const result = designCatalogue(catalogue, design)
  assert.equal(result.length, 4)
  assert.equal(result.filter(item => item.key === 'ads').length, 1)
  assert.equal(result.find(item => item.key === 'ads').name, 'Imported Ads')
  assert.equal(result.find(item => item.key === 'custom-inline').inline, 'GEOIP,CN')
  assert.deepEqual(result.find(item => item.key === 'missing'), { key: 'missing', name: 'missing' })
  assert.equal(catalogue[0].name, 'Ads')
})

test('DNS and Claude policy choices reject direct routes through nested groups', () => {
  const design = { groups: [
    { name: 'direct', type: 'select', members: ['DIRECT'] },
    { name: 'mixed', type: 'select', members: ['safe', 'direct'] },
    { name: 'nested', type: 'select', members: ['mixed'] },
    { name: 'safe', type: 'select', members: ['@all', 'REJECT'] },
    { name: 'cycle1', type: 'select', members: ['cycle2'] },
    { name: 'cycle2', type: 'select', members: ['cycle1', 'DIRECT'] },
  ], rules: [], final: 'safe' }
  for (const policy of ['DIRECT', 'direct', 'mixed', 'nested', 'cycle1', 'cycle2']) assert.equal(designPolicyCanDirect(design, policy), true)
  assert.equal(designPolicyCanDirect(design, 'safe'), false)
})

test('format restrictions are translated and deduplicated without hiding unknown reasons', () => {
  const support = [
    { format: 'clash', supported: true, reasons: [] },
    { format: 'stash', supported: false, reasons: ['DNS requires mihomo', 'New feature requires mihomo'], reason_codes: ['dns_mihomo', 'new_feature'] },
    { format: 'surge', supported: false, reasons: ['DNS requires mihomo'], reason_codes: ['dns_mihomo'] },
    { format: 'egern', supported: false, reasons: ['Older API reason'] },
  ]
  assert.deepEqual(designSupportReasons(support, { dns_mihomo: 'DNS 保护目前仅支持 mihomo' }), ['DNS 保护目前仅支持 mihomo', 'New feature requires mihomo', 'Older API reason'])
})
