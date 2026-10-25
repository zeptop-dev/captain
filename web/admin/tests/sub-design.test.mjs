import assert from 'node:assert/strict'
import test from 'node:test'
import { adoptClaudeRules, claudeAppliedFormats, designCatalogue, designPolicyCanDirect, designSupportReasons, designSupportWarnings, diffClaudeRules, draftClaudeRules, renameDesignGroup, selectedDesignFormats, setClaudeEnabled } from '../src/lib/sub-design.ts'

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

test('localized restrictions preserve dynamic rule and group validation details', () => {
  const support = [{ format: 'surge', supported: false, reasons: ['group Regional uses timeout 17', 'inline custom-2: unknown matcher'], reason_codes: ['group_timeout_unsupported', 'inline_rule_unsupported'] }]
  assert.deepEqual(designSupportReasons(support, {
    group_timeout_unsupported: detail => `超时参数不支持：${detail}`,
    inline_rule_unsupported: detail => `规则不支持：${detail}`,
  }), ['超时参数不支持：group Regional uses timeout 17', '规则不支持：inline custom-2: unknown matcher'])
})

const builtinClaude = { source: 'builtin', revision: 'captain-v1', rules: [{ type: 'domain', value: 'example.com' }] }
const updatedClaude = { source: 'v2fly', revision: 'a'.repeat(40), rules: [{ type: 'domain', value: 'example.com' }, { type: 'full', value: 'service.example.net' }] }

test('client warnings retain their scope and translate independently of blocking reasons', () => {
  const support = [
    { format: 'clash', supported: true, reasons: [] },
    { format: 'stash', supported: true, reasons: [], warnings: ['DNS differs', 'Future caveat'], warning_codes: ['dns_difference', 'future'] },
    { format: 'surge', supported: true, reasons: [], warnings: ['DNS differs'], warning_codes: ['dns_difference'] },
    { format: 'qx', supported: false, reasons: ['Cannot render this design'], warnings: ['Do not describe as supported'] },
  ]
  assert.deepEqual(designSupportWarnings(support, { dns_difference: 'DNS 行为差异' }), [
    { format: 'stash', messages: ['DNS 行为差异', 'Future caveat'] },
    { format: 'surge', messages: ['DNS 行为差异'] },
  ])
  assert.deepEqual(selectedDesignFormats(support, null), ['clash', 'stash', 'surge'])
})

test('applied Claude revisions remain per format with a Mihomo-only legacy fallback', () => {
  const legacy = { applied: { status: 'applied', snapshot: builtinClaude, applied_at: 1 } }
  const oldFormats = claudeAppliedFormats(legacy)
  assert.equal(oldFormats.length, 7)
  assert.deepEqual(oldFormats.find(item => item.format === 'clash').applied, legacy.applied)
  for (const item of oldFormats.filter(item => item.format !== 'clash')) assert.deepEqual(item.applied, { status: 'unknown' })
  const data = { ...legacy, applied_formats: { clash: { status: 'unknown' }, surge: { status: 'applied', snapshot: updatedClaude, applied_at: 2 }, qx: { status: 'disabled', applied_at: 3 } } }
  const formats = claudeAppliedFormats(data)
  assert.deepEqual(formats.find(item => item.format === 'clash').applied, { status: 'unknown' })
  assert.deepEqual(formats.find(item => item.format === 'surge').applied, data.applied_formats.surge)
  assert.deepEqual(formats.find(item => item.format === 'qx').applied, data.applied_formats.qx)
  assert.deepEqual(formats.find(item => item.format === 'stash').applied, { status: 'unknown' })
})

test('legacy Claude drafts remain pinned when the library is fetched or the option is toggled', () => {
  const legacy = { groups: [], rules: [], final: '', enhancements: { claude: true, claude_policy: 'Existing', future_option: true } }
  assert.deepEqual(draftClaudeRules(legacy, builtinClaude), builtinClaude)
  assert.equal(legacy.enhancements.claude_rules, undefined)
  // Turn off before the GET completes, then back on after a newer library is
  // available. Async library availability must not select rules for this draft.
  const off = setClaudeEnabled(legacy, false)
  assert.deepEqual(draftClaudeRules(off, builtinClaude), builtinClaude)
  const on = setClaudeEnabled(off, true)
  assert.equal(on.enhancements.claude_rules, undefined)
  assert.deepEqual(draftClaudeRules(on, builtinClaude), builtinClaude)
  assert.equal(on.enhancements.claude_policy, 'Existing')
  assert.equal(on.enhancements.future_option, true)
  assert.equal(legacy.enhancements.claude_rules, undefined)
  const fresh = setClaudeEnabled({ ...legacy, enhancements: { claude: false } }, true)
  assert.equal(fresh.enhancements.claude_rules, undefined)
  const pinned = setClaudeEnabled({ ...legacy, enhancements: { claude: false, claude_rules: updatedClaude } }, true)
  pinned.enhancements.claude_rules.rules.push({ type: 'full', value: 'draft.example.org' })
  assert.equal(updatedClaude.rules.length, 2)
})

test('Claude review distinguishes exact and suffix matches, deduplicates and ignores order', () => {
  const before = { ...builtinClaude, rules: [{ type: 'domain', value: 'example.com' }, { type: 'full', value: 'example.net' }] }
  const after = { ...updatedClaude, rules: [{ type: 'full', value: 'example.net' }, { type: 'full', value: 'example.com' }, { type: 'full', value: 'example.com' }] }
  assert.deepEqual(diffClaudeRules(before, after), { added: [{ type: 'full', value: 'example.com' }], removed: [{ type: 'domain', value: 'example.com' }] })
  assert.deepEqual(diffClaudeRules(before, { ...before, rules: [...before.rules].reverse() }), { added: [], removed: [] })
  assert.deepEqual(diffClaudeRules(before, { ...before, rules: before.rules.map(rule => ({ ...rule, value: rule.value.toUpperCase() })) }), { added: [], removed: [] })
})

test('adopting a reviewed Claude version preserves draft fields and rejects a stale review', () => {
  const draft = { groups: [{ name: 'Proxy', type: 'select', members: ['@all'] }], rules: [], final: 'Proxy', source: { key: 'custom', name: 'Custom' }, enhancements: { claude: true, dns_protection: true } }
  const expected = JSON.stringify(draft)
  const adopted = adoptClaudeRules(draft, expected, updatedClaude)
  assert.deepEqual(adopted.enhancements.claude_rules, updatedClaude)
  assert.equal(adopted.enhancements.dns_protection, true)
  assert.deepEqual(adopted.groups, draft.groups)
  assert.deepEqual(adopted.source, draft.source)
  assert.equal(draft.enhancements.claude_rules, undefined)
  adopted.enhancements.claude_rules.rules.pop()
  assert.equal(updatedClaude.rules.length, 2)
  assert.equal(adoptClaudeRules({ ...draft, final: 'Another' }, expected, updatedClaude), null)
  const disabled = { ...draft, enhancements: { claude: false } }
  assert.equal(adoptClaudeRules(disabled, JSON.stringify(disabled), updatedClaude), null)
})
