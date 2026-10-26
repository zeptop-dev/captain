import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { siteLanguages } from '../src/i18n/language.ts'

const flatten = (value, prefix = '') => Object.fromEntries(Object.entries(value).flatMap(([key, text]) => {
  const path = prefix ? prefix + '.' + key : key
  return typeof text === 'string' ? [[path, text]] : Object.entries(flatten(text, path))
}))
const resources = Object.fromEntries(siteLanguages.map(language => [language, flatten(JSON.parse(readFileSync(new URL('../src/i18n/' + language + '.json', import.meta.url), 'utf8')))]))
const variables = text => [...text.matchAll(/{{\s*([^,}\s]+)/g)].map(match => match[1]).sort()
for (const language of siteLanguages) {
  test(language + ' has complete translations with matching interpolation variables', () => {
    assert.deepEqual(Object.keys(resources[language]).sort(), Object.keys(resources.en).sort())
    for (const [key, text] of Object.entries(resources[language])) {
      assert.ok(text.trim(), 'empty: ' + key)
      assert.deepEqual(variables(text), variables(resources.en[key]), 'variables: ' + key)
    }
  })
}
