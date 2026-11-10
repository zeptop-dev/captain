# Mail branding and templates

In **Settings → Notifications & integrations → Mail**, delivery settings and
mail branding/templates have independent save buttons. The template editor
supports registration codes, password resets, expiry reminders, traffic
reminders, generic ticket/withdrawal notifications, and connection tests.

Set a separate mail brand name, HTTPS logo URL, button color (`#RRGGBB`), and
footer. Blank brand/footer values inherit the configured site name/brand name.
The SMTP sender name remains separate. Mail clients may block remote logos.

Choose the email type and language before editing subject, plain-text body and
(where applicable) button label. Other languages retain their built-in text.
Empty fields inherit defaults; restoring defaults removes only that language's
selected template, retaining other templates and branding. Save to persist.
HTML is escaped, not executed as custom markup. Verification codes are always
inserted separately by the system, even when removed from the editable text.
Button destinations remain the actual configured portal, not editable URLs.

Variables use `{{name}}` syntax and are checked per mail type:

| Type | Variables (in addition to `site`) |
| --- | --- |
| Registration/reset | `code` |
| Expiry | `expires_at`, `expires_date`, `portal_url`, `plan`, `used`, `total`, `reset_at` |
| Traffic | `used_percent`, `portal_url`, `plan`, `used`, `total`, `reset_at` |
| Generic notification | `subject`, `body` |
| Connection test | none |

Reminder usage, quota, reset and plan values refer to the selected subscription
row, not an invented aggregate date across several plans. Usage uses explicit
GiB units. Reset timestamps include UTC; absent reset is an em dash. Expiry uses
the original mail date/time convention. This change does not alter reminder
thresholds, deduplication, scheduling, entitlement or billing. Generic ticket
and withdrawal notices share one template; their `subject`/`body` values come
from the existing business notification. It does not add new event triggers.

Preview and sample sending use the current editor draft, including unsaved
changes. Samples use `123456`, an example plan, dates and usage; subjects start
with `[TEST]`. They do not create verification codes, read customer accounts,
modify subscriptions, or mark reminders as sent. The selected recipient must
be entered explicitly. SMTP credentials come from saved delivery settings.
A local preview cannot prove delivery or exact rendering in every mail client.

The administrator-only APIs are:

- `GET/PUT /api/admin/settings/mail/templates`: separate `mail_templates` settings
  document (`name`, `logo`, `color`, `footer`, language → type `templates`). GET
  also includes built-in defaults for all six supported languages and variables.
- `POST /api/admin/settings/mail/preview`: `{kind, language, design}` returns JSON
  `{subject, html}` with sample data. Omitted design uses saved templates.
- Existing `POST /api/admin/settings/mail/test` accepts optional `kind`, `language`,
  and `design`, plus the existing `To`. Omitting kind retains the old connection
  test behavior. Preview and send never persist a draft.

The editor previews HTML in a sandboxed, opaque-origin iframe with no scripts,
forms or navigation privileges. No server-side image fetch is performed.
Legacy SMTP saves cannot delete templates, and template saves cannot replace
SMTP credentials. No schema migration or bosun change is needed.
