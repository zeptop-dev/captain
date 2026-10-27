// Local Vite only. APIs are mocked and external requests blocked.
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.PORTAL_TEST_URL || 'http://127.0.0.1:5622/portal/'
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) })
const errors = []
const state = { loggedIn: false, homepage: false, refGate: null, failRef: false, missingRef: false, codes: [], events: [], registered: null, oauthNext: '' }
const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } })
context.on('page', page => page.on('pageerror', error => errors.push(error.message)))
await context.addInitScript(() => { if (location.protocol === 'http:') localStorage.setItem('i18nextLng', 'en') })
await context.route('**/*', async route => {
  const req = route.request(), url = new URL(req.url())
  if (url.origin !== new URL(base).origin) return route.abort()
  if (!url.pathname.startsWith('/api/')) return route.continue()
  const send = (body, status = 200, headers = {}) => route.fulfill({ status, headers, contentType: 'application/json', body: JSON.stringify(body) })
  if (url.pathname === '/api/site') return send({ name: 'Example', homepage_enabled: state.homepage, probe_url: 'https://example.com/server-monitoring' })
  if (url.pathname === '/api/portal/me') return state.loggedIn ? send({ id: 1, email: 'user@example.com', balance_cents: 0, subscriptions: [], subscription_url: '', online_devices: 0 }) : send({ error: 'Unauthorized' }, 401)
  if (url.pathname === '/api/portal/service-status') return send({ generated_at: Date.now() / 1000, refresh_seconds: 60, entries: [] })
  if (url.pathname === '/api/portal/notice') return send({ enabled: false })
  if (url.pathname === '/api/portal/me/lang') return send({})
  if (url.pathname === '/api/portal/register/policy') return send({ open: true, verify: false, reset: true, invite_only: false, email_suffixes: [] })
  if (url.pathname === '/api/oauth/providers') return send({ providers: [{ id: 'example', name: 'Example OAuth' }], password_login: true })
  if (url.pathname === '/api/portal/ref') {
    const code = req.postDataJSON().Code
    state.codes.push(code)
    if (state.refGate) await state.refGate
    if (state.missingRef) return send({ error: 'Invitation not found' }, 404)
    if (state.failRef) return send({ error: 'Invalid invitation' }, 400)
    state.events.push('saved:' + code)
    return send({}, 200, { 'set-cookie': 'captain_ref=' + code + '; Path=/; HttpOnly; SameSite=Lax' })
  }
  if (url.pathname === '/api/oauth/example/start') {
    state.events.push('oauth:' + req.headers().cookie)
    state.oauthNext = url.searchParams.get('next')
    return route.fulfill({ contentType: 'text/html', body: '<h1>OAuth destination</h1>' })
  }
  if (url.pathname === '/api/portal/register') { state.registered = req.postDataJSON(); state.loggedIn = true; return send({}) }
  throw new Error('Unexpected API ' + url.pathname)
})
try {
  const page = await context.newPage()
  await page.goto(base + '?ref=INVITE&next=https://example.com')
  await page.getByRole('heading', { name: 'Welcome back', exact: true }).waitFor()
  assert.equal(new URL(page.url()).search, '?ref=INVITE')
  await page.getByRole('link', { name: 'Example user center', exact: true }).waitFor()
  assert.equal(await page.getByRole('link', { name: 'Back to the site', exact: true }).count(), 0)
  assert.equal(await page.getByRole('link', { name: 'Example user center', exact: true }).getAttribute('href'), '/portal/?ref=INVITE')
  await page.getByRole('link', { name: 'Forgot password?', exact: true }).click()
  await page.getByRole('link', { name: 'Back to sign in', exact: true }).click()
  await page.getByRole('link', { name: 'No account? Sign up', exact: true }).click()
  assert.equal(await page.getByRole('textbox', { name: 'Invite code (optional)', exact: true }).inputValue(), 'INVITE')
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('user@example.com')
  await page.locator('input[type="password"]').fill('ExamplePassword123')
  await page.getByRole('button', { name: 'Sign up', exact: true }).click()
  await page.locator('#portal-main').waitFor()
  assert.equal(state.registered.Invite, 'INVITE')
  // Already authenticated root visitors enter their account, not a login loop.
  await page.goto(base)
  await page.locator('#portal-main').waitFor()
  assert.equal(new URL(page.url()).pathname, '/portal/')
  // Customer navigation never advertises the server probe, even when its URL
  // remains in the compatible site API. Keep service status usable on both sizes.
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.getByRole('button', { name: 'Account', exact: true }).click()
    await page.getByRole('menuitem', { name: 'Service status', exact: true }).waitFor()
    assert.equal(await page.getByRole('menuitem', { name: 'Account', exact: true }).getAttribute('href'), '/portal/account')
    assert.equal(await page.getByRole('menuitem', { name: 'Log out', exact: true }).count(), 1)
    assert.equal(await page.getByRole('menuitem', { name: 'Server monitoring', exact: true }).count(), 0)
    assert.equal(await page.locator('a[href="https://example.com/server-monitoring"]').count(), 0)
    await page.getByRole('menuitem', { name: 'Service status', exact: true }).click()
    await page.getByRole('heading', { name: 'Service status', exact: true }).waitFor()
    assert.equal(new URL(page.url()).pathname, '/portal/status')
    await page.goto(base)
    await page.locator('#portal-main').waitFor()
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  state.loggedIn = false
  let releaseRef
  state.refGate = new Promise(resolve => { releaseRef = resolve })
  state.codes = []; state.events = []
  await page.goto(base + 'register?ref=FIRST&next=https://example.com')
  await page.getByRole('textbox', { name: 'Invite code (optional)', exact: true }).fill('SECOND')
  await page.getByRole('button', { name: 'Continue with Example OAuth', exact: true }).click()
  assert.equal(new URL(page.url()).pathname, '/portal/register')
  assert.deepEqual(state.codes, ['FIRST'])
  assert.deepEqual(state.events, [])
  releaseRef()
  await page.getByRole('heading', { name: 'OAuth destination', exact: true }).waitFor()
  assert.deepEqual(state.codes, ['FIRST', 'SECOND'])
  assert.deepEqual(state.events, ['saved:FIRST', 'saved:SECOND', 'oauth:captain_ref=SECOND'])
  assert.equal(state.oauthNext, '/portal/')
  state.refGate = null; state.failRef = true
  await page.goto(base + 'login?ref=RETRY')
  await page.getByRole('button', { name: 'Continue with Example OAuth', exact: true }).click()
  await page.getByText('Could not save the invitation code. Check the code and try again.', { exact: true }).waitFor()
  assert.equal(new URL(page.url()).pathname, '/portal/login')
  state.failRef = false
  await page.getByRole('button', { name: 'Continue with Example OAuth', exact: true }).click()
  await page.getByRole('heading', { name: 'OAuth destination', exact: true }).waitFor()
  assert.ok(state.events.at(-1).includes('captain_ref=RETRY'))
  state.missingRef = true; state.events = []
  await page.goto(base + 'login?ref=REMOVED')
  await page.getByRole('button', { name: 'Continue with Example OAuth', exact: true }).click()
  await page.getByRole('heading', { name: 'OAuth destination', exact: true }).waitFor()
  assert.equal(state.events.length, 1)
  assert.ok(state.events[0].startsWith('oauth:'))
  assert.equal(state.oauthNext, '/portal/')
  state.missingRef = false
  // Navigating within the portal while the cookie is pending cancels the old
  // page's OAuth transition; a late response must not hijack the new form.
  state.refGate = new Promise(resolve => { releaseRef = resolve })
  state.events = []
  await page.goto(base + 'register?ref=CANCEL')
  await page.getByRole('button', { name: 'Continue with Example OAuth', exact: true }).click()
  await page.getByRole('link', { name: 'Have an account? Sign in', exact: true }).click()
  await page.getByRole('heading', { name: 'Welcome back', exact: true }).waitFor()
  const saved = page.waitForResponse(response => new URL(response.url()).pathname === '/api/portal/ref')
  releaseRef()
  await saved
  await page.getByRole('heading', { name: 'Welcome back', exact: true }).waitFor()
  assert.equal(new URL(page.url()).pathname, '/portal/login')
  assert.deepEqual(state.events, ['saved:CANCEL'])
  state.refGate = null
  state.homepage = undefined
  await page.goto(base + 'login')
  await page.getByRole('link', { name: 'Back to the site', exact: true }).waitFor()
  assert.equal(await page.getByRole('link', { name: 'Example home', exact: true }).getAttribute('href'), '/')
  assert.deepEqual(errors, [])
  console.log('PASS: desktop/mobile customer-only navigation and service status, anonymous/authenticated entry, ref registration and forgot paths, ordered OAuth cookie, failure retry, missing invite OAuth, navigation cancellation, legacy enabled default')
} finally { await browser.close() }
