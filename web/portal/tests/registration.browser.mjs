// Local preview only: backend contracts have separate real HTTP/SQLite tests.
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.PORTAL_TEST_URL || 'http://127.0.0.1:5628/portal/'
const browser = await chromium.launch({ headless: true, channel: 'chrome' })
const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } })
let sent = 0, request, verified = false, signedIn = false, denial = false
const errors = []
const policy = { open: true, verify: true, reset: true, email_suffixes: [], legal: { terms_url: '/terms', privacy_url: '/privacy', document_version: 'v1' }, legal_version: 'version1', captcha: { provider: 'turnstile', site_key: 'test-key' } }
await context.addInitScript(() => {
  localStorage.setItem('i18nextLng', 'en')
  window.turnstile = { render: (element, options) => { element.textContent = 'Test human verification'; options.callback('fresh-token'); return 'widget' } }
})
await context.route('**/*', async route => {
  const url = new URL(route.request().url())
  if (url.hostname === 'challenges.cloudflare.com') return route.fulfill({ contentType: 'application/javascript', body: '' })
  if (url.origin !== new URL(base).origin) return route.abort()
  if (url.pathname === '/oauth-destination') return route.fulfill({contentType:'text/html',body:'<h1>OAuth destination</h1>'})
  if (!url.pathname.startsWith('/api/')) return route.continue()
  const send = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
  switch (url.pathname) {
    case '/api/site': return send({ name: 'Example', homepage_enabled: false })
    case '/api/oauth/providers': return send({ providers: [{ id: 'example', name: 'Example' }], password_login: true })
    case '/api/oauth/example/start':
      assert.equal(route.request().method(), 'POST')
      assert.equal(route.request().postDataJSON().LegalVersion, 'version1')
      assert.equal(route.request().postDataJSON().LegalAccepted, true)
      return send({url:new URL(base).origin+'/oauth-destination'})
    case '/api/oauth/identities': return send([])
    case '/api/portal/register/policy': return send(policy)
    case '/api/portal/me': return signedIn ? send({ id: 1, email: 'member@example.com', balance_cents: 0, subscriptions: [], email_verification: { verified, verified_at: verified ? new Date().toISOString() : null }, email_verification_available: true }) : send({ error: 'unauthorized' }, 401)
    case '/api/portal/verify/send': sent++; assert.equal(route.request().postDataJSON().Captcha, 'fresh-token'); return send({ sent: true, retry_after: 60 })
    case '/api/portal/register':
      request = route.request().postDataJSON()
      if (denial) return send({ error: 'this email domain is not accepted', code: 'email_domain' }, 403)
      signedIn = true; return send({})
    case '/api/portal/me/email/verification/send': return send({ sent: true, retry_after: 60 })
    case '/api/portal/me/email/verification': verified = true; return send({})
    default: return send({})
  }
})
try {
  const page = await context.newPage()
  page.on('pageerror', e => errors.push(e.message))
  await page.goto(base + 'register')
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('member@example.com')
  await page.locator('input[type="password"]').fill('ExamplePassword123')
  assert.equal(await page.getByRole('button', { name: 'Sign up', exact: true }).isDisabled(), true)
  assert.equal(await page.getByRole('button', { name: 'Continue with Example', exact: true }).isDisabled(), true)
  await page.getByRole('button', { name: 'Send code', exact: true }).click()
  await page.getByText('Code sent to member@example.com. Valid for 10 minutes.').waitFor()
  assert.equal(sent, 1)
  assert.equal(await page.getByRole('button', { name: /Resend in/ }).isDisabled(), true)
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('different@example.com')
  assert.equal(await page.getByText('Code sent to different@example.com. Valid for 10 minutes.').count(), 0)
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('member@example.com')
  await page.getByRole('checkbox').check()
  await page.getByRole('textbox', { name: 'Code', exact: true }).fill('123456')
  denial = true
  await page.getByRole('button', { name: 'Sign up', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: 'This email domain is not accepted.' }).waitFor()
  assert.equal(await page.getByText('Registration is closed.', { exact: true }).count(), 0)
  denial = false
  await page.getByRole('button', { name: 'Sign up', exact: true }).click()
  await page.waitForURL(url => url.pathname.replace(/\/$/, '') === '/portal')
  assert.equal(request.LegalAccepted, true)
  assert.equal(request.LegalVersion, 'version1')
  assert.equal(request.Code, '123456')
  await page.goto(base + 'account')
  await page.getByText('No email verification record', { exact: true }).waitFor()
  await page.getByRole('button', { name: 'Send code', exact: true }).click()
  await page.getByRole('textbox', { name: 'Code', exact: true }).fill('123456')
  await page.getByRole('button', { name: 'Verify email', exact: true }).click()
  await page.getByText('Email verified', { exact: true }).waitFor()
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    signedIn = false
    await page.goto(base + 'register')
    await page.getByRole('checkbox').waitFor()
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false)
    if (process.env.REGISTRATION_SCREENSHOT_DIR) await page.screenshot({ path: `${process.env.REGISTRATION_SCREENSHOT_DIR}/register-${width}.png`, fullPage: true })
  }
  await page.getByRole('checkbox').check()
  await page.getByRole('button', {name:'Continue with Example',exact:true}).click()
  await page.getByRole('heading',{name:'OAuth destination'}).waitFor()
  assert.deepEqual(errors, [])
  console.log('Registration browser scenarios passed: consent, captcha renewal, cooldown, email changes, precise errors, verification and mobile layout.')
} catch (error) { console.log('Page errors:', errors); console.log((await context.pages()[0]?.locator('body').innerText())?.slice(0,4000)); throw error } finally { await context.close(); await browser.close() }
