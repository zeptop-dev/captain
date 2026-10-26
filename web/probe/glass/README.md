# Glassmorphism public frontend

This frontend is a React port of the public Vue UI from
[sanrokamlan-prog/komari-theme-Glassmorphism](https://github.com/sanrokamlan-prog/komari-theme-Glassmorphism),
version 3.3.7, commit `06999d5a77ebe521a02fd3e383e67a064e345e7e` (MIT; see LICENSE).
It reproduces the upstream templates, Tailwind styles, background, icons, globe,
node cards/list, details and ECharts components element for element. The footer
identifies Captain as the server and retains the theme attribution. The Komari
admin application is not included.

The port is held to the previous Vue build pixel for pixel:
`scripts/glass-diff.mjs` renders both under Playwright's virtual clock and
compares 16 screenshots (home with the latency dialog, list view, node detail;
light/dark; desktop/mobile) at zero tolerance, with `--dom` to explain any
difference. Framework-neutral logic (Captain adapter, stores' settings,
chart options, card builders) lives in `../glass-shared` and was shared with the
Vue build while both existed, so the two produced identical values. Vue's
compiled scoped styles are kept as plain CSS in `src/scoped/`, with the same
`data-v-<name>` scope attributes set by the components. `src/components/EChart.tsx`
mirrors vue-echarts 8.3.2, including its vendored update planner, and
`src/components/Transition*.tsx` mirror Vue's transition class sequence.

Not ported, because Captain can never show them: the cobe and tiled earth
renderers (Captain never sets `earthRenderer`), the visitor-info widget
(disabled on Captain), the finance details dialog (no card opens it) and the
admin-only home tools (topology, provider value, health, export, audit log;
Captain's public page is never logged in). The node comparison tool is ported.

The three-carrier panels and their colour thresholds additionally derive from
[vlongx/komari-theme-Glassmorphism-three-network](https://github.com/vlongx/komari-theme-Glassmorphism-three-network),
commit `2f172e7f9e8a87e0e3a1d0c5c2d2ef3555278b9c` (same MIT licence).
The base frontend remains the newer upstream version above.

Captain adaptations:

- `../glass-shared/captain.ts` is a read-only adapter to `/api/probe` and its per-node history
  endpoints. `src/utils/init.ts` fills the stores and polls Captain.
  No Komari server, agent, RPC endpoint, or additional port is required.
- `../glass-shared/ping-summary.ts` translates counted Captain samples into the original twenty
  latency/loss bars. Null gaps, weighted attempts and successful latency are
  preserved. The metric adapter does not invent raw attempts or quantiles.
- `../glass-shared/ping-selection.ts` selects the configured data source. With carrier monitoring
  enabled and CT/CU/CM configured, cards show the original two panels with three
  carrier rows. The list uses three compact pairs of twenty-segment bars with
  carrier dots and tooltips, fitting the upstream 64px row and its virtual-scroll
  spacing; card-sized panels must not be placed in the narrow uptime cell.
  Dual-stack probes add labeled v4/v6 values and history bars to each carrier;
  single-stack cards retain one value and bar. Both families keep the same one-hour
  averaging window. The compact list still fits the upstream 64px row.
  Missing samples leave only that family empty; unsupported agents and ICMP
  permission failures show an unavailable value with a reason.
  Otherwise the first enabled, node-assigned task by ID supplies both bars.
  `ping_tasks` in the public node snapshot contains IDs, labels, protocol, IP family
  and opaque configuration identities, never destinations. Histories match the
  exact task ID, name and series identity; changing a target or protocol cannot
  blend old samples into the new result. Selection never jumps to another task
  or IP family because samples are absent.
  Custom non-carrier labels are not presented as Chinese operators.
- CPU/resource validity, monthly monitoring traffic, section visibility and
  unavailable financial data follow Captain semantics. An unknown price does
  not mean free; unavailable cost/value estimates show a dash with an explanation.
  Cost cards read structured Captain billing directly from the current public
  snapshot. They show original-currency monthly averages and calendar-based
  remaining-value estimates; custom decimal years do not become invented days.
  Overview totals use the configured summary currency (default CNY) and Captain's
  cached Frankfurter reference rates, without a coverage count. Missing rates
  suppress incomplete totals; tooltips identify quote dates and older caches.
  Detail amounts stay in their original currencies. These cards
  follow the Information publication setting, without activating the upstream
  day-based finance adapter, static exchange rates or private finance dialog.
  All traffic totals and quota bars
  use persisted period counters (`traffic.used_up/used_down`), independently of
  live network rates. The overview adds each selected node’s current period;
  nodes may have different reset days. Restarts preserve recorded usage. Missing
  counters stay unknown rather than falling back to OS counters. GPU details remain private.
- Hash routes retain arbitrary status page mounts and dedicated host support.
  The snapshot's additive `appearance_scheme` resolves inherited site colours
  without needing `/api/site` on a dedicated status hostname.
- Favorites are managed in node details. Home cards and list rows show a filled
  star only for saved favorites; the marker is not a separate toggle. The home
  Favorites filter is hidden when the current group/search has no favorites and
  is cleared if its last matching favorite disappears, including changes from
  another browser tab. Existing browser-local favorites remain intact.
- Icons and images are local. `scripts/glass-icons.mjs` rebuilds the icon subset;
  `ICON-LICENSES.json` and `licenses/` record the third-party icon licences.
  The production bundle includes the theme and icon licence texts under `licenses/`.
  External visitor audit/fingerprinting, IP geolocation, exchange-rate fetches
  and Komari administrative operations are disabled.
- The search container expands along with its focused input, so the toolbar
  reserves its full width and wraps on narrow screens. Upstream's absolute input
  expanded outside its icon-sized container and could extend beyond the viewport.
- Core public labels have six-language dictionaries. The header language selector
  saves the current origin's `i18nextLng` preference and reloads the current URL,
  so module-initialized labels and chart options use the same language. The hash
  detail route, theme, group and favorites remain stored. Without a saved choice,
  language follows the browser; separate domains have separate preferences. The
  app locale for prices and locale-aware formatting follows the same selection;
  this does not translate every existing theme label or date format.
- Public storage cards read only the selected `node.storage` projection, never
  private resource snapshots or mount paths. One volume uses the existing disk
  cell; two volumes share it; further volumes open in a keyboard-accessible
  dialog. Empty selections stay empty, and only an absent field on an older
  server falls back to the root filesystem. Missing values remain unknown.
  The overview still reports root filesystem usage and is labelled accordingly;
  selected volumes are never summed because their capacities can overlap.
  Compact cards shorten units and round values to fit, while the dialog and
  detail page show each volume separately with IEC units and two decimals.
  The upstream mini-card continues to omit the disk cell.

Run `pnpm build` from `web/probe`; it tests the adapter and builds both status pages.
Run `node scripts/storage-browser.mjs` after building to check selected storage,
empty/missing/legacy values, visibility, expansion and keyboard interactions in
both themes at desktop, 390px and 320px widths. It uses fictitious public API data
and writes screenshots to `/tmp/probe-storage-public` by default.
`pnpm dev:glass` starts this frontend with the existing local API proxy. The
embedded handler selects this build when `appearance.preset` is `glass`.

When updating, compare against a separately built upstream checkout with the
same data, viewport, theme and browser. Do not replace the upstream layout with
an approximation. Account for Captain branding and unsupported data separately
from layout differences. Do not use screenshots with a frozen clock to assert
that the animated globe renders: check its real pixels with an advancing clock.
Do not use masked comparison images as product previews: all six overview cards,
including the unknown remaining-value card, must be visible in a full preview.
The globe has no scale-in dependency and falls back to a bundled static texture
when WebGL is unavailable or its context is lost.
