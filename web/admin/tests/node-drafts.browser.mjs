// Compatibility entry point for the server draft browser regression.
// The former one-page editor is now split across overview, information and
// settings. The replacement keeps substantive save/discard, sibling-draft,
// modal, monitor-only and operator coverage with mocked APIs.
if (!process.env.SERVER_DETAIL_TEST_URL && process.env.ADMIN_TEST_URL) {
  process.env.SERVER_DETAIL_TEST_URL = process.env.ADMIN_TEST_URL
}
await import('./server-detail.browser.mjs')
