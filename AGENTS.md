# merch — agent guide

`CLAUDE.md` is a symlink to this file. Edit `AGENTS.md` only.

Hack Club's swag and warehouse app. It has a Go backend (`cmd/`, `internal/`), a SvelteKit PWA
(`web/`), and Postgres. Physical fulfillment goes through the mail.hackclub.com warehouse API.

**This repo is public (github.com/hackclub/merch), and git history is permanent.** Everything
you commit or push is world-readable: code, tests, fixtures, migrations, commit messages, and
this file. Secrets live in env vars only. Never commit:
- secrets: `.env` files, API keys, tokens, signed URLs (`?signature=…`), cookies;
- personal data: real people's names, emails, phone numbers, addresses, Slack IDs or messages,
  photos of people, or rows from the DB, Airtable, Fillout, or Zenventory exports. Tests use
  made-up people (`orpheus@hackclub.com`, `a@x.com`);
- internal money details: Stripe links, HCB or expense codes, per-person payments.

Item data that came from private sources (photos people posted, Slack credits) goes in the DB
through the app, not into migrations or seed files. Before every commit, read `git diff
--cached`. If something private was pushed, tell Zach; don't try to rewrite `main`.

## The three user flows

| Who | How we know | What they do |
| --- | --- | --- |
| Hack clubber (anyone signed in) | Hack Club Auth login | Request listed swag. US shipping is free. Outside the US the request starts in `awaiting_payment`, and the person pays the estimated postage by card or Apple Pay on the form (Stripe) before we ship (see "Shipping estimates and packing"). |
| YSWS author | Their HCA email matches `Hack Club Auth Email` in the **YSWS Authors** table of the Unified YSWS Projects DB (Airtable `app3A5kJwYqxMLOgh`) | Submit print-ready material (stickers, posters…) to be printed and stocked in the warehouse. |
| Admin | Email is in `ADMIN_EMAILS` (admins are also authors) | Manage global limits, list warehouse SKUs as items, set visibility and order, review requests (ship → tracking), and review author submissions. |
| Review agent | An API key an admin made (`Authorization: Bearer merch_…`) | Read pending requests, the requester and their history, add notes, ship (with changes) or reject. See "Agent API". |

Items can come in **sizes** (`items.variants`, e.g. the red Hack Club T-shirt `Swa/Shirt/HC` with
S/M/L/XL → `Swa/Shirt/HC/<size>`). Each size ships its own warehouse SKU; limits count the item as a
whole. There's no cooldown between requests and no cap on how many different items go in one
request (each request is reviewed before it ships); how many of each is the item's own `max_per_request`. Request lines store the exact SKU shipped (`swag_request_items.sku`, `variant`). When shipping, an admin
can change a line's quantity (0 = not sent), add a visible item without sizes, and add a note for the requester; `requested_quantity`
keeps what was asked for (0 = added), limits count what shipped, and the shipped email lists the changes. A SKU belongs
to one item: saving sizes folds any item listed under a size's SKU (e.g. imported per-size shirts)
into the sized item, history included, and the importer attaches size SKUs to the sized item.

Request status machine (`internal/swag`): `awaiting_payment → pending → dispatched`. From
`awaiting_payment` or `pending`, a request can also go to `rejected` or `cancelled`. Rejected and
cancelled requests don't count toward limits.

## Layout

```
cmd/server/          main: env config → migrate → wire deps → HTTP server
internal/config/     env-var config (12-factor), no file reads
internal/swag/       pure business rules: limits, intl shipping, status transitions,
                     packing (packing.go) and shipping estimates (estimate.go)
internal/store/      Postgres persistence (pgx); per-user lock around limit check + insert
internal/db/         pool, embedded goose migrations (internal/db/migrations/*.sql)
internal/db/dbtest/  per-test throwaway database (CREATE DATABASE test_<rand> … DROP)
internal/auth/       Hack Club Auth OAuth client (auth.hackclub.com, GET /api/v1/me)
internal/authors/    YSWS author lookup (Airtable REST, 10 min cache)
internal/theseus/    mail.hackclub.com warehouse client (SKUs, warehouse_orders)
internal/stripe/     Stripe PaymentIntents client (international shipping payments)
internal/loops/      Loops transactional email client (request emails)
internal/asendia/    Asendia tracking lookups (delivery dates for shipping history)
cmd/shiphistory/     loads past shipments for estimates (see "Shipping estimates and packing")
internal/cdn/        cdn.hackclub.com upload client (item images)
internal/imgprep/    shrinks uploaded artwork to ≤1600px JPEGs before upload
internal/httpapi/    routes, role middleware, CSRF guard, SPA static serving
web/                 SvelteKit SPA (adapter-static, ssr=false) + manifest
```

## Visual style

The shop looks like a streetwear store (Supreme, Palace, Stüssy), not a dashboard. Tokens live in
`web/src/app.css`: Hack Club red is the only accent, Phantom Sans (loaded from assets.hackclub.com)
for text, `var(--mono)` uppercase for small labels and facts, square corners everywhere, product
images on borderless `var(--tile)` (#f0f1ec, the sticker photos' own backdrop; other images use
`mix-blend-mode: multiply` so white backgrounds disappear). Reuse these instead of new colors or
rounded cards.

The shop home page (`web/src/routes/+page.svelte`) is the exception: Zach wants it as simple as the old
Fillout request forms (forms.hackclub.com/mini-magazine). It's one white column on the grey page,
in one font and size: plain intro text, one photo, then the questions, a compact item list, shipping
info and Submit. Keep it that simple; product pages carry the 3D views and the detail.
The item list opens with "Start here": the Mini-Magazine, the Welcome to Hack Club postcard,
the Haven postcard and the 11x17 Haven poster, which are what Zach most wants people to hand out. Everything else is listed
under "Extras", grouped by kind (`SECTIONS` in `web/src/lib/sections.ts`).

## TDD workflow (required)

1. Write or extend a failing test first. Run it and watch it fail for the right reason.
2. Write the smallest change that passes it.
3. Refactor with the tests green. Then run `make check test` before every commit.

Where tests go:
- Business rules → table tests in `internal/swag` (pure, no DB).
- SQL → `internal/store` tests against a real Postgres via `dbtest.New(t)`. Never mock the DB.
- Endpoints and role checks → `internal/httpapi` tests. They use a real test DB with fakes for
  HCA (`fakeProvider`), authors (`authors.Static`), and the warehouse (`fakeWarehouse`).
- External clients → `httptest.Server` fakes. Tests must never call auth.hackclub.com,
  mail.hackclub.com, or Airtable.
- Frontend logic → `web/src/lib/*.test.ts` (Vitest). Keep logic in `$lib`, not in `.svelte` files,
  so it stays testable.
- Usage events (see "Usage analytics") → part of the same red-green cycle as the feature. Assert
  server events in `internal/httpapi` tests with `h.events("name")` and `props(t, e)`. Assert
  browser event logic (what gets sent, what stays private) in `web/src/lib/track.test.ts`.

## Commands

```sh
make db        # shared dev Postgres via docker compose (127.0.0.1:54329)
make test      # go test ./... + vitest   (make test-go / make test-web)
make check     # gofmt, go vet, svelte-check
make dev       # Go server :8080 + Vite :5173 (proxies /api, /auth) — http://localhost:5173,
               # or http://porygon:5173 from other tailnet machines (Zach browses from crobat)
               # Only from the main worktree. See "Sharing porygon with other agents".
make server    # Go server only; serves web/build if built (make web)
make docker    # production image
scripts/prod-psql             # production SQL (read-only); see "Usage analytics"
```

One Postgres container serves every worktree. Each test gets its own database, so parallel
worktrees don't collide in `make test`. The dev database (`mailroom`) is shared, though. See
below. If Docker is down on porygon, OrbStack is the runtime (`orb start`). Its data dir must be
on the internal disk. See "Gotchas".

## Config: 12-factor, one shared `.env`

- The app reads **only env vars** (`internal/config`). It never reads files. `.env.example`
  lists every variable.
- In dev, `make` sources the **`.env` in the main worktree root**. Right now that's
  `~/dev/hackclub/merch/.env`. `scripts/env-file` finds it through
  `git rev-parse --git-common-dir`, so every worktree (`~/.paseo/worktrees/...`) shares it.
  Never copy `.env` into a worktree. `ENV_FILE=...` overrides the path.
- In production (Orchard), set the same variables on the deployment. There is no `.env` file.

## Landing work on main (worktrees)

Several agents usually work here at once, each in its own paseo worktree
(`~/.paseo/worktrees/…`) on a throwaway branch. Main moves every few minutes. On 2026-10-14,
five sessions landed seven commits in 90 minutes, and at least one rebase hit conflicts.
**Finished work lands on `main`. A task is not done until it's pushed.** Don't stop at "ready to
merge", and don't leave changes uncommitted in a worktree. The next agent can't see them, and
Zach won't find them.

Start every task by syncing, because worktrees are often created from an old commit:

```sh
git fetch origin && git rebase origin/main   # before you read or change code
(cd web && npm ci)                           # new worktrees have no node_modules
```

`make check` fails with `svelte-kit: command not found` when `web/node_modules` is missing.
Don't pipe `make check test` into `tail`/`grep` in an `&&` chain. The pipe hides the exit
code, and you'll commit on a red build.

`make test` includes the usage-analytics release gates (see "Usage analytics"). A user-facing
change isn't done until it's instrumented, tested, landed, and its events show up in production.

Land in small slices, as soon as each one is green. Don't save up one big change. A long
session with several asks (for example "give me a link" → product pages → 3D preview) should
land after each ask:

```sh
make check test                      # green first
git add -A && git diff --cached      # review: only your files, nothing private (public repo)
git commit                           # small, descriptive commits
git fetch origin
git rebase origin/main               # other agents may have landed work; rebase, don't merge
make check test                      # re-verify after the rebase
git push origin HEAD:main            # land it
# fast-forward the main worktree if it's clean:
git -C "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")" pull --ff-only
```

- If `git push` is rejected because main moved, fetch, rebase, re-test, and push again. Never
  force-push `main`, and never `git push --force` anywhere.
- When a rebase conflicts, read what landed (`git log -p ORIG_HEAD..origin/main -- <file>`)
  and keep both changes. Never resolve with `--ours`/`--theirs` across a whole file, and never
  revert another agent's landed work to make yours fit. If the two designs really conflict,
  build on the landed one and tell Zach.
- **Migrations:** pick your number at landing time, not when you start. After the rebase, check
  `ls internal/db/migrations`. If someone landed the same number, `git mv` yours to the next
  free number before you push. (On 2026-10-14, two branches both wrote `00007_*`.)
- Hot files (`internal/httpapi/httpapi.go`, `internal/store/store.go`, `web/src/lib/types.ts`,
  and this file) change in almost every task. Keep your edits there small and local: add new
  functions, don't reorder or reformat code you didn't need to touch.
- `git add -A` stages everything in *your* worktree, so check `git status` for stray files
  (screenshots, exports, scratch scripts) first.
- After you land, check that `git log origin/main` contains your commit, and give its hash in
  your final reply.

## Sharing porygon with other agents

Every agent runs on the same machine, Postgres container, and ports.

- **Ports:** `:8080` (Go) and `:5173` (Vite) are the shared dev server that Zach opens at
  http://porygon:5173. Run it only from the main worktree (`~/dev/hackclub/merch`) on
  up-to-date `main`. After you land, restart it there so Zach sees your work. If it's
  already running, check its cwd first (`lsof -a -p <pid> -d cwd`).
- **To try your branch before landing**, use your own ports. Pick a free one (`lsof -i :8081`)
  and run:
  `bash -c 'set -a; . "$(scripts/env-file)"; set +a; PORT=8081 go run ./cmd/server'`, then
  `cd web && BACKEND_URL=http://localhost:8081 npx vite dev --port 5174`. Hack Club Auth
  sign-in only redirects to `:5173`, so test signed-in flows on the shared server after you
  land, or use the httpapi tests.
- **Never kill a process you didn't start.** No `pkill -f server`, `pkill node`, or
  `killall`. Kill by the PID you started, or check a process's cwd before you stop it. (A broad
  `pkill` once took down another worktree's dev server.) If the shared server dies, restart it
  from the main worktree, not from yours.
- **The dev DB (`mailroom` on :54329) is shared, and so is its goose version table.** If you
  start the server from a branch with an unlanded migration, that migration runs against
  everyone's DB. If another branch then lands a different migration with the same number,
  goose silently skips it on dev. So land a migration before you run it against `mailroom`.
  If you need it applied first, use a scratch DB:
  `docker exec merch-postgres-1 createdb -U mailroom merch_<branch>`. Set
  `DATABASE_URL=postgres://mailroom:mailroom@127.0.0.1:54329/merch_<branch>?sslmode=disable`
  after the `. env-file` in the command above, and `dropdb` it when you're done.
- Data changes in the dev DB (making items visible, adding images) are visible to every agent
  and to Zach. Make them through the app or store code, and mention them in your reply.
- Use the git stash only with a unique message (the stash is shared across worktrees). A WIP
  commit is better.

## Importing historical requests (Fillout → Airtable era)

Before this app, each item had its own Fillout form. Rows went to Airtable `shipment_requests`
and then to Zenventory. `cmd/import` loads those exports:

```sh
make import DRY=1 FILES='"…/Fillout Hack Club Mini-Magazine Request results.csv" "…/T-Shirt….csv"'
make import TRACKING=/tmp/zmr-tracking.csv FILES='…'   # writes to $DATABASE_URL
```

- The import is idempotent on the Fillout `Submission ID` (`swag_requests.source='fillout'`,
  `external_id`), so re-exports are safe to re-run.
- Contents come from `custom_instructions` (`"10 Pri/Bok/Mini25/1st, 50 Sti/Bra/O&H/Lap"`).
  Unknown SKUs become **hidden** items, named from mail.hackclub.com.
- A row with an Airtable record (`Created records`) → `dispatched`, with `airtable_record_id`
  set. This holds even if `Send To Warehouse` is blank, because that flag lags: Zach confirmed
  that rows with a blank flag were already queued. Only rows with no record become `pending`.
- Imported requests can never be shipped from this app (`dispatch` returns 409). They ship
  through the Airtable warehouse base, and this guard prevents double shipments.
  "Requestor paid N USD" → `shipping_fee_cents` and `paid_at`.
- People are matched by email. A person with no account gets a placeholder user (`hca_id NULL`),
  which their first Hack Club Auth sign-in claims. Imported history then counts toward their
  per-person limits.
- Airtable `internal_notes` (Stripe links, expense codes) go to `internal_note`, which only
  admins see. `admin_note` is shown to the requester.
- Tracking: the Zenventory `order_number` equals the Airtable record id. Export it from the
  data warehouse (`agh_fulfillment_zenventory.customer_orders` JOIN `shipments`, columns
  `order_number,tracking_number,carrier,shipped_date`) and pass it with `-tracking`.
- **Exports and tracking files contain personal data. Keep them outside the repo (it's
  public).**
- Imported 2026-09-29 into the dev DB: Mini-Magazine (320), Staff T-Shirt (29), Hackpad Poster
  (66), Sunbeam Poster (34). That's 449 requests (all dispatched, 442 with tracking) across 372
  people.

## Deploying (Orchard)

- Production is **https://merch.hackclub.com**: Orchard org YSWS, project `merch`, deployment
  `merch` (`44052a13-2fe2-4f4c-94aa-5ec7102b4fd5`), Postgres `merch-db`
  (`4c2c324f-8b8f-4afe-94a5-3c9d1ba4a6f5`, PG 17, daily backup 03:00 UTC). DNS is a CNAME to
  `merch-merch-1aa3b1.k.hackclub.dev` in `hackclub/dns` (`hackclub.com.yaml`).
- Production is public since 2026-10-17: the `merch.hackclub.com` ingress has no Orchard login
  (protection `none`), and `ALLOWED_EMAILS` is unset, so anyone with Hack Club Auth can sign in
  and request swag. Every request is still reviewed before it ships. To close sign-in again, set
  `ALLOWED_EMAILS` (comma-separated) on the deployment with `add_env_vars`. (The auto-generated
  `*.k.hackclub.dev` host still has Orchard login.)
- **Every push to `main` builds and deploys to production** (Orchard auto-deploy, about 3
  minutes). Landing on `main` is shipping. Check a rollout with `list_pods` (the image tag) and
  `get_image_history`, not `get_deployment`.
- Production started on 2026-10-14 as a copy of the dev DB (items, the Fillout import, users).
  Since then the two have diverged: make catalog changes in production through the app. Dev
  sets `THESEUS_ORDER_TAG=merch-dev` and production uses `merch`, so the same request id in both
  databases never shares a warehouse idempotency key.
- The pod requests 100m CPU, so a few ms of CPU per request gets throttled into hundreds of ms.
  Do heavy work once and cache it (shipping history is indexed once and loaded at startup), and
  check `api` events' `ms` after you land.
- The build writes brotli and gzip copies of every JS, CSS and JSON file (adapter-static
  `precompress`), and the server sends the one the browser accepts (`serveStatic`). Nothing is
  compressed live. Before 2026-10-18 nothing was compressed: the home page loaded 965 KB of JS
  (three.js alone 585 KB, 121 KB as brotli).
- One container: `Dockerfile`, which builds the PWA and a static Go binary on distroless. It
  listens on `$PORT` (default 8080). Health check: `GET /healthz` (pings the DB).
- Migrations run automatically at startup (goose, embedded).
- **Production SQL:** `scripts/prod-psql` connects to `merch-db` through its public TLS endpoint
  (enabled 2026-10-14), using `PROD_DATABASE_URL` from the shared `.env`. It is read-only by
  default; pass `--write` first to change data (`scripts/prod-psql --write -c "update …"`). Wrap
  writes in `begin; … commit;`, match rows by id *and* name (ids differ from dev), and never
  print the URL. If the password is rotated, rebuild the URL from `get_database`.
- **Backups:** Orchard takes a base backup of `merch-db` daily at 03:00 UTC and archives WAL
  continuously to R2, so it can restore to any point since the first backup (2026-10-15 03:00
  UTC). Backups are managed only in Orchard; don't add off-platform dumps or cron jobs. Check
  with `get_database` (`liveStatus.archiving`, `lastSuccessfulBackup`) and
  `list_database_backups`; take one before risky changes with `create_database_backup`.
  `restore_database_backup` replaces the live data, and deleting the project deletes its
  backups.
- Required env: `DATABASE_URL`, `BASE_URL` (public https origin), `SESSION_SECRET`,
  `HCA_CLIENT_ID`, `HCA_CLIENT_SECRET`, `THESEUS_API_KEY`, `THESEUS_BILLING_PROFILE_ID`, `STRIPE_SECRET_KEY`,
  `STRIPE_PUBLISHABLE_KEY`, `ADMIN_EMAILS`. Also needed: `AIRTABLE_API_KEY` for authors,
  `HACKCLUB_CDN_API_KEY` for item image uploads, `GOOGLE_PLACES_API_KEY` for address
  autocomplete, and `LOOPS_API_KEY` for request emails.
- `$BASE_URL/auth/callback` must be registered as a redirect URI on the Hack Club Auth app.
- Read the `orchard-ops` skill before touching Orchard (`pdw call hack_club_orchard_k8s__…`).

## Shipping estimates and packing

International parcels go by **Asendia, up to 3 lb**. Stickers in an envelope go USPS First Class
letter mail (in the US and abroad). Other US parcels go USPS Ground Advantage or UPS SurePost.

- **Packing** (`internal/swag/packing.go`): `Pack` puts a request in the first of `swag.Packages`
  it fits. These are what AGH packs in: a #6x9 envelope (stickers, letter mail); the Uline Black
  Self-Seal Stayflats we stock there (S-11837 9x12, S-11838 11x13, S-11839 13x18; rigid, ¼"
  of contents, one stack); bubble mailers; 6x4x2 box mailers; and the cartons on AGH's invoices
  (8x6x6, 10x8x6, 13x10x4, 14x10x6, 18x12x6, 18x14x10). StayFlat sizes and weights come from
  Uline's specs (case weight / 100). Carton weights scale Uline's 10x8x6 (0.44 lb) by board
  area, and each holds up to 70 lb (the USPS Ground Advantage and UPS limit). The bubble mailer's size isn't on the invoices; it's modeled as a #4 (9x13½, 1 oz).
  Posters (thin, longer than 13½") ship in the 13x18 StayFlat, or flat on the bottom of an
  18-inch carton with whatever else is in the request (as AGH ships 50 posters, or posters with
  shirts). The envelope stops at 1.8 oz packed (30 stickers): AGH mails bigger sticker orders as
  parcels. What an item `ships_with` is packed too (one per unit). `Pack` uses item sizes
  (`width_in`, `height_in`, `thickness_in`; stickers and postcards have defaults; anything else
  counts as a bulky 10x8x1) and `items.weight_oz`. With no weight set, it works one out from
  the paper size (0.625 oz/in³). `Validate` refuses requests that don't fit in one package and
  international requests over 3 lb packed.
- **Warehouse weights include the packaging.** AGH logs each package at its packaging plus a
  fixed weight per SKU (a fit over 767 orders since 2025-06 leaves a median 0.04 oz unexplained),
  and buys labels at that weight. One poster logs 7.2 oz (1.2 oz of paper in a 6 oz StayFlat),
  n Mini-Magazines n × 0.96 + 1 oz, and a shirt in a mailer 5.5 (S), 6.0 (M), 6.6 (L) or
  6.9 oz (XL); two or three shirts share one mailer. So `items.weight_oz` is the item alone
  (a size's own `weight_oz` in `variants` when sizes differ), `Pack` adds the mailer's `TareOz`,
  and postage estimates match history on that packed weight.
- **Estimates** (`internal/swag/estimate.go`): arrival window = handling + transit, in business
  days (Mon–Fri; the shop intro gives transit only, after "Shipped next day Mon–Fri"), from the 20th to the 80th percentile of past shipments to the same country and class. With fewer than
  10 samples, it uses all US or all international shipments instead. US postage compares packages
  of about the same packed weight (US parcels are also priced by ZIP zone, which isn't known), from
  the last 180 days of history first (postage rises every
  year: a 5-9 oz US parcel went from $4.71 in early 2025 to $7.27 in mid-2026), then older history.
  Boxes over about 10 lb go UPS Ground, which the history left out until 2026-10-18 (big boxes got
  $0, then about $8.50 whatever their weight; with it, 20 lb is about $16 and 40 lb $34). Without 10
  parcels of about the same weight, postage takes the 10 nearest in weight (basis `US, nearest weights`).
  **Abroad, postage comes off rate cards** (`internal/swag/ratecard.go`): Asendia and USPS price
  each country by the whole ounce, base + per ounce, to the cent (Canada 2026: $8.90 + $0.414/oz;
  India $6.51 + $0.508/oz; letters $1.93 up to 1 oz, $3.85 up to 2 oz). A price index chained
  month to month brings old prices to today's rates (Asendia rose 5.3% in January 2025 and 15%
  in January 2026, France 28%), each country's card is read at the packed weight rounded up to
  the ounce, in-between weights read off the line, and countries with fewer than 3 packages use
  all international ones of that weight. Past a card's heaviest weight, it follows the slope of
  its heavier half when that's flatter: most countries stay on one line to 3 lb (Kenya's 7 oz card
  priced a 24 oz parcel to 5¢), but the dearest bend over a pound (Mexico, Colombia, Egypt), and
  following the whole line quoted Colombia $90 for 3 lb (now $47). Backtest any change with
  `MERCH_BACKTEST_CSV=<history export> go test ./internal/swag -run Backtest -v`, which prices each
  month's labels from the history before it. Backtested on 2025-26 parcels: 87-93% within 25¢ of
  the label (median-by-weight, used before 2026-10-17: 2-29%, undercharging about $2 on
  average). Ranges are shown only to admins (everyone sees the typical
  postage in the cost table). The shop intro, the request form, My requests and the admin
  request list show these estimates (`POST /api/shipping/estimate`, `GET /api/shipping`).
- **Backorders:** the mail API gives each SKU's `in_stock` (below zero when orders wait on it) and
  `inbound` (units on order) but no arrival date. When stock can't cover a line and more is
  inbound (`swag.Backordered`), the shop's item list says "15k are inbound to Hack Club HQ. Your
  package will ship on arrival." (`$lib/backorder`, from `/api/catalog`'s
  `stock`), and the estimate counts from the restock (`Estimate.AfterRestock`: no dates, 1–2
  extra business days for the warehouse to receive it). The date is in Zenventory purchase
  orders (`required_by_date`), which Theseus fetches but doesn't return yet.
  Only items with **"Can be backordered"** (`items.allow_backorder`, a checkbox on the admin Items
  tab) take requests past stock. For every other item, a new request can take only what's left:
  the warehouse's `in_stock` less what open (`awaiting_payment`, `pending`) requests hold
  (`Store.HeldQuantities`). `swag.Validate` refuses the rest ("out of stock: only 2 Hack Club
  enamel pin left", logged as `request_refused`), and the shop says "Out of stock." / "Only 2
  left." and hides Add. A SKU the warehouse doesn't list counts as out of stock; if the warehouse
  can't be reached, stock isn't checked. Since 2026-10-20 only the Haven postcard and the
  Haven poster can be backordered (Zach: not the pins). Admin shipping doesn't check stock.
- **International fee:** an international request pays its typical postage off the rate card,
  to the cent, the same
  number as the cost table's postage (`swag.InitialStatus`). The admin setting `international_shipping_fee_cents` is only the fallback
  when there's no postage history; 0 still makes international shipping free.
  Zach's standing decision (2026-10-16, repeated 2026-10-19): international requesters always
  pay their own postage. Never make it free or propose free or subsidized international postage.
- **Cost to Hack Club** (`internal/swag/costs.go`, shown to everyone above the request form's
  "I understand" checkbox, with "Cost to you": $0 in the US, the shipping fee abroad):
  items at the warehouse's unit costs (mail.hackclub.com `unit_cost`, ships-with SKUs included),
  AGH labor ($1.80 an order + $0.20 for each different SKU, from its invoices; the warehouse's
  `agh_fulfillment_zenventory.invoices`), the package's `CostCents`, and the typical postage. The table
  has one column, for the address's country (the US until one is entered). Out-of-stock SKUs have no unit cost and show as unpriced.
  - **What's worth it:** Hack Club is financially transparent (its live ledger is public at
    https://hcb.hackclub.com/hq). The marketing goal is to spend at most **$50 per new teenager
    who joins Hack Club and ships one weighted project** (10 approved hours) in one of our
    programs ($5 per approved hour). Above the cost table, `worthText` (`web/src/lib/shipping.ts`)
    says what makes the request worth it, for the destination's side: under $30, the hours one
    new teen ships ("ship 6 hours of work"); from $30, how many teens each ship a weighted
    project. Both round up. Judge swag, limits and shipping rules by this number.
- **Spend dashboard** (top of the Admin page's requests tab; `GET /api/admin/dashboard?days=`,
  `internal/httpapi/dashboard.go`, `swag.SummarizeSpend`): shipped requests' cost by day (New York,
  by `dispatched_at`; imports by `mailed_at`) as items, labor, packing and postage; the month so
  far plus the last 7 days' daily average for each day left; what's waiting to ship; and whether
  international requesters' payments (paid less refunded) cover their postage, by country. Each
  request uses mail.hackclub.com's own charges where it has them (`warehouse_*_cents`: contents and
  labor when it takes the order, postage once mailed; its labor runs higher than the AGH rates in
  `costs.go`), else `CostToShip` estimates. Nothing pulls charges on its own: the dashboard's "Pull
  costs from warehouse" (`POST /api/admin/requests/refresh`, up to 60 orders a click) and each
  request's refresh fetch them. The request list below loads every request once and filters in the
  browser (`$lib/dashboard`).
- **History** (`shipping_history`, no names or addresses) comes from the Hack Club data warehouse:
  - `scripts/shipping-history/zenventory.sql`: AGH shipments, for postage and Asendia tracking.
  - `theseus-orders.sql`: orders placed through mail.hackclub.com, for handling time.
  - `theseus-letters.sql`: letters. US transit is the day after the last USPS IV scan. Abroad,
    it's the day the recipient marked the letter received.
  - Asendia delivery dates come from Asendia's public tracking API
    (`cmd/shiphistory track`, `internal/asendia`). It keeps only about 6 months, so keep the
    `-cache` file.
  - US parcels: USPS and UPS block scripted tracking lookups, so
    `scripts/shipping-history/us-tracking.py` reads a random sample of them from their
    tracking pages in Safari (WebDriver) into the same `-cache` file. On 2026-10-16, 164
    parcels shipped from 2026-08 to 2026-10 took 2–5 business days (median 4). UPS SurePost
    takes 3–5 and USPS Ground Advantage 2–4. The US letter times (4–7) are slower because
    HQ batches letters before USPS first scans them.
  - India is the slowest common destination: 13–20 business days (19–28 calendar days),
    against 9–16 for all international parcels. About 15% of Indian parcels never show a
    delivery scan, and their last scans are 3–5 weeks out, so the real tail is a little longer.

  Refresh (keep every file outside the repo; the raw exports contain tracking numbers):

  ```sh
  # export each .sql to CSV (one `r` column per row; the query tool caps at 10k rows, so page by date)
  scripts/shipping-history/us-tracking.py -cache ~/merch-ship/asendia.csv \
      -since <2 months ago> -sample 250 zenventory.csv           # needs `safaridriver -p 4455`
  go run ./cmd/shiphistory track -since <6 months ago> -cache ~/merch-ship/asendia.csv \
      zenventory.csv letters.csv > history.csv
  DATABASE_URL=… go run ./cmd/shiphistory load history.csv orders.csv   # replaces those sources
  ```
  Asendia postage from 2026-10-01 on is left out (`zenventory.sql`): since then every label is
  logged at its country's 5 oz price whatever the weight, which isn't the rate card. Ask AGH
  what it really bills before bringing it back.
  Loaded 2026-10-15 into dev and production: 47,167 shipments since 2025-01. That's 499 Asendia
  deliveries since 2026-04-20, 5,027 US letter scans and 1,833 mail.hackclub.com orders. The
  warehouse's `theseus.*` tables are a snapshot from before 2026-09-14.

## Usage analytics

Everything people do on the site goes to the `usage_events` table, so agents can see how the shop is
used and improve it. **Use it as evidence, and keep it complete:**

1. **Before you change a flow, look at how it's used.** Query production yourself with
   `scripts/prod-psql` (read-only), starting from the question you have, not a canned report.
   Leave out admins and headless browsers (see below). Dev's table has only agent and test
   traffic. Mention what the data showed in your reply or commit message ("12 of 40 people
   who added a poster were refused at the estimate").
2. **Instrument every new feature, test first.** Every action records a server event that says
   what happened (`s.track(r, user, "name", props)`). Choices and interactions that never reach the
   server record a browser event (`track('name', props)` from `$lib/track`), or a `data-item` /
   `data-track` attribute so clicks and hovers name it. Write the assertion for the event
   (`h.events`, `track.test.ts`) in the same red step as the feature.
3. **Document it.** Add each new event and its props to the lists below.
4. **After you land, check that it arrives.** Once the deploy rolls out, use the feature (or wait
   for real use), then query `usage_events` in production for the new event names. An event that
   never shows up is a bug.

**Release gates** (`internal/httpapi/tracking_gate_test.go`, part of `make test`, so a red gate
blocks landing):
- `TestEveryActionIsTracked`: every POST/PUT/PATCH/DELETE route's handler must call `s.track`,
  directly or through a server method (`s.trackRequest`, `s.trackAdmin`). Admin routes count too.
  Only the explicit `untracked` list (each entry with its reason) is exempt. Don't add to `untracked` to
  get a build through; track the action.
- `TestEventsAreDocumented`: every event name passed to `s.track` or `track(` must have a valid
  name and appear in backticks in this section.
- `TestAPICallsAreLogged` and the privacy tests (no address in `request_created`, no typed text
  in `field`, no text from `data-private`) must stay green. Never weaken one to land a change.

- Columns: `at`, `source` (`client` from the browser, `server` from handlers), `name`, `visitor`
  (random id per browser, cookie `zmr_visitor`), `visit` (per tab; renewed after 30 idle minutes),
  `user_id` (when signed in), `path` (the page), `props` (jsonb), `release` (the web build's time,
  e.g. `20261017T141645Z`: the server's build on server events, the open tab's on browser
  events; `''` before 2026-10-17). Group by `release` to compare before and after a change;
  Orchard's `get_image_history` maps a build time to its commit.
- `visitor` links what someone did before they signed in to who they are (`sign_in` has both).
  Leave out admins (`users.email` in `ADMIN_EMAILS`) and `visit_start.props->>'automated' = 'true'`
  (headless browsers, agents) when you measure real people.
- Browser events (`web/src/lib/track.ts`, sent in batches to `POST /api/events`):
  `visit_start` (referrer, `utm_*`, screen, viewport, language, time zone, touch, PWA, user agent),
  `page_view` (`pv` id, `from`, `item_id` on product pages), `page_time` (`active_ms` with input
  in the last minute, `visible_ms`, `scroll_pct`, `clicks`, `seen`: the `data-item` ids at least
  half on screen for a second, in the order seen; snapshots, so use the
  **`usage_page_times`** view, which keeps the latest per page view), `page_return`, `click` (element,
  its text, `href`, `item_id` and `area` from the nearest `data-item`/`data-track`, `dead` when
  nothing clickable was hit), `field` (which field changed; the choice for radios, checkboxes and
  selects, only `chars` for typed text), `field_invalid` (the browser blocked a submit: `field`,
  `type`, `reason` such as `valueMissing` or `patternMismatch`; never the value), `cart` and `cart_size` (`item_id`, `sku`, `from`, `to`; from
  every page, since all cart changes go through `$lib/bag.svelte`), `purpose`, `sticker_nudge` (a club or
  event request picked 3 or more sticker designs at under 50 each and was shown the packs-of-50
  suggestion: `designs`, `loose`), `item_hover` (mouse
  over an item for ≥ 0.5 s), `payment_error` (a card didn't go through: `request_id`, `method` = `card` or `apple_pay`, and Stripe's `type`,
  `code`, `decline_code`; never the message), `api_error`, `js_error`, `pwa_installed`,
  `web_vital` (Core Web Vitals from the `web-vitals` library, once per page load: `metric` =
  `LCP`, `INP`, `CLS`, `FCP` or `TTFB`, `value` in ms (CLS unitless), `rating`, `nav`),
  `page_perf` (how smoothly a page view ran, from every animation frame while visible: `pv`,
  `frames`, `fps`, `hz` (the display's refresh rate), `frame_p50`/`p95`/`p99`/`max` in ms, `jank`
  (frames over 50 ms), `dropped_pct` (frames missed against `hz`); Chrome's long animation frames
  `loaf`, `loaf_ms`, `loaf_blocking_ms`, `loaf_max` and the top 3 `loaf_scripts` (`src` path, `fn`,
  `invoker`, `ms`, `n`); 3D views `views`, `views_max` (at once) and `lost` (WebGL contexts the
  browser took back); `heap_mb` (Chrome); snapshots like `page_time`, so use the
  **`usage_page_perf`** view), `view3d` (one 3D view's life, from `meterView` in `$lib/track`:
  `vid`, `view` = `product`, `tile` or `haul`, `kind` (`sheet`, `book`, `fold`, `stickers`,
  `postcards`, `shirt`, `pin`; the haul's `haul`, `table` or `compact`), `item_id`, `load_ms` (code
  downloaded and scene built), `ready_ms` (images in, drawn for real), `alive_ms`, `draws`,
  `render_p50`/`p95`/`max` (ms of main thread per draw; the GPU's share shows up as slow frames),
  `fps` and the frame stats above while it's moving, `px` (drawing buffer), `dpr`, `gl` (`shared`
  or `own` renderer), `gpu` (the GPU's name), `lost`, `failed` (with `error`: why, as the error's name and message, no query strings); snapshots, so use the **`usage_view3d`** view). Join them to
  `visit_start` on `visit` for the device: `visit_start` also has `cores`, `memory_gb` (Chrome),
  `net`, `save_data` and `reduced_motion`. `/admin` sends only page
  views, page time and these performance events.
- `api`: every `/api/*` and `/auth/*` call (except `/api/events`), logged by middleware: `route`
  (the mux pattern), `url` (the path, never the query), `status`, `ms`, and on a 500 the
  `error` that `serverErr` logged. Use it for errors and slow routes.
- Server events (`s.track` in `internal/httpapi`): `login_start`, `sign_in`, `sign_in_refused`,
  `sign_out`, `payment_started` (the card form is about to charge: `request_id`, `fee_cents`, `country`,
  `retry`), `payment_completed` (Stripe says it's paid in full; the request moved to `pending`; `wallet` = `apple_pay` when paid that way),
  `payment_refunded` (a rejected or cancelled request's card payment went back: `request_id`,
  `fee_cents`, `refund_cents`, `country`, `by` = `admin` or `requester`, `partial` when shipping
  less made postage cheaper, `existing` when the refund was already on Stripe, e.g. one held for approval), `email_sent` and `email_failed` (a request email: `kind` = `received`, `dispatched` or `rejected`,
  `request_id`, `email_id` (its `sent_emails` row, with the full text), and Loops' `error`), `shipping_estimate` (country, lines, package, weights, service, arrival window, fee,
  postage, `postage_basis` (the country's rate card, or `international` without one), sample counts, `restock` when a line is backordered, or `error` when it won't pack), `request_created`, `request_refused` (the
  rule it broke), `request_cancelled`, `submission_created`. They carry the page's `visit` (the
  `X-Merch-Visit` header) and `path` (from `Referer`), so they line up with browser events.
- Admin events (`internal/httpapi/insights.go`). Request events belong to the requester's
  `user_id`, with `admin_id` for who acted, and carry `request_id`, `country`, `purpose`, `source`,
  `lines`, `items_requested`, `items_shipped`, `fee_cents`, `wait_h` (hours since the request was
  made), `since_paid_h` and `refunded_cents`: `request_dispatched` (`changed` when quantities changed, `has_note`, `warning`),
  `request_rejected` (`from`, `has_note`, `refunded`), `tracking_updated` (`carrier`,
  `has_tracking`, `has_postage` once the warehouse has priced the label), `internal_note_updated` (admins' Markdown note on a request: `has_note`, `chars`,
  `from_chars`; never the text). Catalog and limits, with the admin's `user_id`: `item_created` (`item_id`,
  `sku`, `visible`), `item_updated` (`changed`: `{field: {from, to}}`, lists as their lengths),
  `item_media_added` (`kind` = `image`, `page`, `photo` or `print_file`), `settings_updated` (`changed`),
  `submission_updated` (`from`, `to`, `kind`, `has_sku`), `api_key_created` and `api_key_revoked`
  (`api_key_id`), `requests_refreshed` (the dashboard pulled tracking and charges from
  mail.hackclub.com: `checked`, `updated`, `mailed`, `failed`, `left`). Notes are never logged, only `has_note`. What an agent does with an API key
  (see "Agent API") logs the same events with `api_key_id` added; its notes are
  `internal_note_updated` with `appended`.
- **Never log what people type or where they live.** No addresses, names, notes, search text,
  or IPs: just countries, lengths and choices. Put `data-private` on any element that shows
  personal data, so clicks on it log no text. To track something new, add a `data-item` or `data-track` attribute,
  or call `track(name, props)` from `$lib/track`; record server outcomes with `s.track`.

```sql
-- what people add to their requests (last 30 days)
select i.name, count(*) adds, count(distinct e.visitor) people from usage_events e
join items i on i.id = (e.props->>'item_id')::bigint
where e.name = 'cart' and (e.props->>'to')::int > (e.props->>'from')::int and e.at > now() - interval '30 days'
group by 1 order by 2 desc;
-- time on product pages
select i.name, count(*) views, percentile_cont(.5) within group (order by active_ms)/1000 median_s
from usage_page_times t join items i on i.id = t.item_id group by 1 order by 2 desc;
-- funnel: visitors → picked something → signed in → requested
select count(distinct visitor) filter (where name = 'page_view') visitors,
       count(distinct visitor) filter (where name = 'cart') picked,
       count(distinct visitor) filter (where name = 'sign_in') signed_in,
       count(distinct visitor) filter (where name = 'request_created') requested
from usage_events where at > now() - interval '30 days';
-- why requests and estimates fail
select name, props->>'error' error, count(*) from usage_events
where props ? 'error' group by 1, 2 order by 3 desc;
-- items people saw but didn't add (product rows on screen ≥ 1 s vs cart adds)
with seen as (select distinct visitor, jsonb_array_elements_text(seen)::bigint item_id from usage_page_times),
     added as (select distinct visitor, (props->>'item_id')::bigint item_id from usage_events
               where name = 'cart' and (props->>'to')::int > 0)
select i.name, count(*) saw, count(a.visitor) added from seen s join items i on i.id = s.item_id
left join added a using (visitor, item_id) group by 1 order by 2 desc;
-- the same funnel before and after a release
select release, count(distinct visitor) filter (where name = 'page_view') visitors,
       count(distinct visitor) filter (where name = 'request_created') requested
from usage_events where release <> '' group by 1 order by 1;
```

### Self-improvement runs

When Zach asks you to improve the shop from evidence, run this loop end to end:

1. **Read the evidence.** Query production yourself (`scripts/prod-psql`) and think from first
   principles: what people came for, where they stopped, and why. Join `usage_events` (real
   people; with admins left in for errors and slow routes) with the domain tables: `swag_requests` and `swag_request_items` (what was asked for and
   shipped; `status`, `paid_at`, `refunded_cents`, `requested_quantity`; app requests have no
   dispatch time column, so use `request_dispatched.at` and its `wait_h`), `items`
   (limits, visibility, sizes), `settings`, `shipping_history` (postage and transit), and
   `author_submissions`. The admin events give you the timeline of what changed.
2. **Find the biggest leak.** Use the funnel (visited → picked → signed in → estimate → requested →
   paid → dispatched), where people stop (`page_time`, `field_invalid`, dead clicks,
   `request_refused`, `shipping_estimate` errors, `payment_error`), what they see but skip (`seen`
   vs `cart`), slow pages (`web_vital`, `api` `ms`), errors (`api` 5xx `error`, `js_error`,
   `email_failed`), and what admins change by hand (`request_dispatched.changed`, rejections).
   Count people, not events, and say how many there were. With fewer than about 20 people in a
   step, say the evidence is thin, and don't overfit to Zach's own testing.
3. **Change one thing** (TDD, instrumented, landed as usual), and name in the commit message the
   numbers that motivated it.
4. **Check it later.** Compare the same query grouped by `release` before and after. Write what
   you found, and what you'd try next, in your final reply.

## Agent API

A scheduled agent (about every 3 hours) reviews pending requests through `/api/agent/*`
(`internal/httpapi/agent.go`). Its runbook, run notes and state live in the private repo
`zachlatta/merch-ops` (`~/dev/hackclub/merch-ops` on porygon). Its docs are `internal/httpapi/agent_api.md`, served at
`GET /api/agent`; keep them in step with the code, because they're all the agent reads.
- Admins make and revoke keys on the Admin page's API keys tab (`/api/admin/api-keys`). The
  token (`merch_…`) is shown once; only its SHA-256 is stored (`api_keys`).
- A key acts for the admin who made it and stops working if they leave `ADMIN_EMAILS`. It
  works only on `/api/agent/*`, never on cookie routes, and can't make keys or change items
  or settings.
- `GET /api/agent/requests/{id}` adds `requester`, `history` (their other requests) and
  `same_address` (other accounts' requests to the same street and postal code, compared on
  letters and digits only).
- `POST …/notes` appends a signed paragraph to `internal_note`; it never replaces it.
  `POST …/dispatch` and `…/reject` are the admin handlers, so they ship real mail and send
  the same emails. Uncertain cases get a `Needs a human:` note and stay `pending`.
  `POST …/refresh` pulls tracking from mail.hackclub.com (the admin refresh handler); nothing
  pulls it automatically, so the review agent refreshes what it shipped on later runs.
- `merch.hackclub.com` has no Orchard login, so the key is the only lock on `/api/agent`. If
  protection is ever turned back on, add `/api/agent` to the ingress's `bypassPaths`. The
  scheduled agent's key ("Review agent") is `MERCH_AGENT_API_KEY` in the shared
  `.env`.

## External systems

- **Hack Club Auth**: OAuth2 authorization code flow. The token comes from `/oauth/token`, then
  identity from `GET /api/v1/me` (`identity.primary_email`, `id`, `slack_id`,
  `verification_status`, `phone_number`, `addresses[]` with a `primary` flag).
  - Scopes the app supports: `openid email name profile phone birthdate address
    verification_status slack_id legal_name basic_info`. We request
    `openid email name slack_id verification_status address phone`: the primary address and
    phone prefill the request form and cover international customs. Stored in
    `users.address`; a sign-in without the address scope keeps the old value.
  - Registered redirect URIs (app "Zach's Mail Room"): `http://localhost:5173/auth/callback`,
    `http://porygon:5173/auth/callback` and `https://merch.hackclub.com/auth/callback`.
  - The app serves several origins: `BASE_URL` plus `EXTRA_BASE_URLS`. Each request's `Host`
    picks the matching origin for the OAuth `redirect_uri`, cookies, and CSRF `Origin` checks,
    so the Vite proxy must keep `Host` (the default; no `changeOrigin`).
- **mail.hackclub.com (Theseus)** (source: github.com/hackclub/theseus):
  - Auth is `Authorization: Bearer <THESEUS_API_KEY>`.
  - `GET /api/v1/warehouse/skus` returns `{"skus":[…]}` (enabled and in stock).
  - `POST /api/v1/warehouse_orders` takes `{warehouse_order:{recipient_email, user_facing_title,
    idempotency_key, tags[], metadata}, address:{first_name,last_name,line_1,line_2,city,state,
    postal_code,country,phone_number}, contents:[{sku,quantity}]}`. It creates **and
    dispatches** a real order to the warehouse. Our idempotency key is
    `<THESEUS_ORDER_TAG>-request-<id>`, so retries replay the order instead of shipping twice.
  - International orders need a phone number for customs.
  - A billing profile is required. Every order sends `billing_profile_id` from
    `THESEUS_BILLING_PROFILE_ID` (the Hack Club Merch Requests HCB org; set in the shared `.env` and Orchard), so a
    change to the key's default can't move the charges. The profile must belong to the key's owner.
  - **Never call CreateOrder outside the admin "Ship" action (`adminDispatch`, which the
    agent API's dispatch also uses). It ships real mail.**
- **Stripe** (`internal/stripe`, `internal/httpapi/payments.go`, `web/src/lib/CardField.svelte`)
  takes international shipping payments with a card field right on the request form: Stripe's
  Payment Element, a card or Apple Pay (no Google Pay or Link). It asks for just the number,
  expiry and CVC (the shipping address is the billing address), and its iframe sends card
  numbers straight to Stripe. Apple Pay is a card payment to Stripe (`allowed_payment_method_types`
  stays `card`) and shows only in Safari with a card in Wallet, on https domains registered with
  Stripe: the server registers `BASE_URL` and `EXTRA_BASE_URLS` hosts at startup
  (`stripe.RegisterDomain`; no Apple file to host). Registering in live mode covers sandboxes, and
  plain-http dev origins never show it. Submit checks the card, creates the request, then `POST /api/requests/{id}/pay`
  starts a PaymentIntent (USD, the request's `shipping_fee_cents`, `metadata.request_id`;
  reused while it can still be paid) and the browser confirms it (`payShipping` in
  `$lib/payment`). A declined card lands on My requests, where an unpaid request has the same
  field and a Pay button. There is no webhook: loading
  My requests or the admin request list checks Stripe, and a PaymentIntent that `succeeded` for
  the full amount moves the request to `pending` (`paid_at`, `stripe_payment_intent`). Pay
  never starts a second payment while the first is processing or Stripe can't be reached.
  Rejecting or cancelling cancels an unpaid PaymentIntent and refunds a paid one in full
  (`refunded_at`); if Stripe won't refund, the request stays open so the admin can retry. The live
  account can hold a refund for a person to approve in the Stripe dashboard (403 with an `apreq_…`
  id, `stripe.ApprovalError`); Stripe runs it once approved, and rejecting again then counts the
  refund already on the payment instead of making a second one. When an admin ships less of a card-paid request, it's re-quoted on what
  shipped and the difference goes back to the card (`refunded_cents`; the shipped email says so). Card payment is the only way to pay
  (HCB links and "Mark paid" were removed 2026-10-17); the server won't start without
  `STRIPE_SECRET_KEY` and `STRIPE_PUBLISHABLE_KEY`. Dev uses the sandbox (`sk_test_…`/`pk_test_…`, test card
  4242 4242 4242 4242); the keys live only in the shared `.env` and Orchard, never in the repo.
  Stripe emails the requester a receipt whose only line is the PaymentIntent's `description`
  (newlines show as separate lines), so `paymentDescription` puts the detail there: request
  number, city/state/country, every item and quantity, total postage paid, and the requests
  link. Stripe caps it at 1000 characters; long requests end with "and N more items".
- **Loops** (`internal/loops`, `internal/httpapi/emails.go`) emails the requester through one
  transactional template (`LOOPS_TRANSACTIONAL_ID`) whose variables are `subject_line`,
  `preview_text` and `body_text` (plain text; the wording is in `emails.go`). Three emails:
  `received` when the request reaches `pending` (on submit in the US; abroad, once the postage is
  paid), `dispatched` when an admin ships it, and `rejected` with the admin's reason. The
  idempotency key `<THESEUS_ORDER_TAG>-request-<id>-<kind>` sends each one once. A failed send is
  logged (`email_failed`) and never blocks the action. Without `LOOPS_API_KEY`, no emails. The
  key lives only in the shared `.env` and Orchard.
- **Every email is saved word for word** in `sent_emails` (`kind`, `request_id`, `user_id`,
  `to_email`, `subject`, `preview`, `body`, `idempotency_key`, `status` = `sent`, `failed` or
  `unsent` (no `LOOPS_API_KEY`, so dev logs the wording too), `error`, `release`), so audits
  can read what people were told and catch confusing or odd wording. Add any new email through
  `emailRequest` (or `logEmail`) so it's logged too. Read them on the Admin page's emails tab,
  `GET /api/admin/emails` or `/api/agent/emails` (`?request_id=`, `?kind=`, `?status=`,
  `?before=`, `?limit=`), or `scripts/prod-psql -c "select at, kind, request_id, subject, body
  from sent_emails order by id desc limit 20"`. The table holds requesters' emails and names
  (the greeting): it's personal data, so never copy rows into the repo.
- **cdn.hackclub.com** hosts item images. Admins upload them on the admin Items tab; the
  server re-encodes them as ≤1600px JPEGs (print PNGs are ~9 MB) and stores the CDN URLs in
  `items.images` (ordered; the first is the thumbnail). Thumbnails and the
  home page strips load them through Cloudflare Image Transformations on cdn.hackclub.com
  (`/cdn-cgi/image/…`, `$lib/thumb`): sized for their box at 1x/2x/3x, AVIF or WebP. Each item has a product page at
  `/items/<id>-<slug>`. Printed items with `width_in`/`height_in` set open on a 3D view
  (`web/src/lib/Product3D.svelte` + `paper3d.ts`, three.js, lazy-loaded): images[0] and
  images[1] are the front and back of a sheet with real thickness (`thickness_in`) and paper
  surface (`finish`: matte, gloss, linen, uncoated), next to a phone at the same scale.
  `photos` holds real-world pictures people posted (`{url, caption, credit}`), shown as
  "In the wild" on the product page.
  `print_files` (`{url, label, bytes}`) are the item's original print files (PDF, PNG, JPEG or
  TIFF, up to 60 MB), uploaded untouched to the CDN from the admin Items tab and listed on the
  product page as "Print your own" (postcards, posters), so people can print their own. Clicks
  log as `click` with `area` = `print_file`.
  Booklets (`binding` = `staple` or `perfect`) open in the 3D view and flip through
  `items.pages` (page images, front cover first; `web/src/lib/book3d.ts`). The magazines'
  pages come from the public PDFs on magazine.hackclub.com, split into single pages.
  A trifold brochure (`binding` = `trifold`; images[0] is the outside, reading flap | back | cover,
  and images[1] the inside) is one sheet letter-folded in thirds (`fold.ts`, `fold3d.ts`): the
  product page opens it and folds it back up on a loop until touched, tiles unfold it on hover, and
  `Pack` treats it as folded (a third as wide, three sheets thick). The haul piles them folded but
  leaves room for the top one, which opens out flat and folds back up on the same loop.
  `ships_with` lists SKUs added to the warehouse order once per unit (the Magazine 2025
  always ships with its intro card, `Pri/M25/4x6/1st`), and the product page shows them.
  Enamel pins (`finish` = `enamel`; `pin.ts`, `pin3d.ts`) are modeled from images[0], a photo of
  the face on a plain backdrop: the outline becomes a beveled metal blank, the photo's gold
  stands proud of glossy enamel, and the back gets a post and butterfly clutch (two on a pin
  1.4" or wider). The product page floats it beside a phone; the haul lays them face up in rows
  of 5.
  Shop grid tiles (`web/src/lib/Tile3D.svelte`) show the same 3D views without the phone, still
  until touched. Tiles and the small request pile (`Haul3D` `compact`) mark their canvas
  `data-gl="shared"`: `createStage` (`paper3d.ts`) draws them all through one WebGL renderer and
  copies each frame onto the tile's 2D canvas, so hovering down the list doesn't make a context,
  compile shaders and build the environment for every row. Measured on 30 hovers: dropped frames
  went from 18–23% to 7%, and main-thread blocking from 3.1–4.7 s to 0.7–1.0 s. Give any new
  view that comes and goes with hovers or scrolling the same attribute.
  Tiles: hover or drag turns prints and stickers, and booklets flip through on hover.
  `clip` (an MP4, GIF or photo URL, set on the admin Items tab) replaces the tile's 3D view with a
  real-world loop that plays on hover, or a still photo; the Mini-Magazine uses a loop, and the red
  T-shirt shows a photo of someone wearing it. A still photo gives way to the item's 3D view on hover
  (the shop's hover peek shows the T-shirts in 3D).
- **Link previews** (Slack, iMessage, X): `web/src/app.html` holds the site's share tags between
  `<!-- share -->` markers. `og.gif` (900x473, the Mini-Magazine clip beside "Free swag, mailed
  to you.") animates where unfurlers allow it; `og.jpg` (1200x630, its first frame) is the X
  card. On `/items/<id>-…` the server swaps the block for that visible item's name, description
  and first image (`internal/httpapi/share.go`), since the SPA renders nothing server-side.
- **Print source files** for warehouse SKUs: the Marketing warehouse Airtable
  (`appK53aN0fz3sgJ4w`, table `warehouse_skus`) has `re_order_instructions`, e.g. "Order
  VP_MJR069GX on zach@hackclub.com Vistaprint". The Vistaprint order-confirmation email links a
  `storage.documents.cimpress.io` design document; its panels (Front/Back) link the original
  print files (`api.sherbert.cimpress.io/.../print?signature=…`). The Hackpad poster
  (`Pri/HPd/11x/1st`) and 4x6 card (`Pri/HPd/4x6/1st`) use the same front/back artwork.
- **Google Places API (New)** (`internal/places`): the request form's address line 1 suggests
  addresses as people type. The server proxies `/api/address/suggest` and `/api/address/place`
  (signed-in only) so the key stays secret; one session token per search keeps it to one billed
  session. Without `GOOGLE_PLACES_API_KEY` the endpoints return 404 and the field is a plain input.
- **Unified YSWS DB authors**: only about 64 of 114 authors had `Hack Club Auth Email` filled
  in (warehouse snapshot, 2026-09-29). An author missing it won't be recognized; fix it in
  Airtable.

## Gotchas

- porygon's OrbStack was once configured with `data_dir` on `/Volumes/PorygonWork`. When that
  drive was unmounted, Docker hung at "waiting for external data drive". On 2026-09-29 it was
  reset to the internal disk (`~/.orbstack/vmconfig.json`); the old config is saved as
  `vmconfig.json.bak-porygonwork-2026-09-29`.
- CSRF: mutating `/api/*` calls must be `Content-Type: application/json`, and their `Origin`
  (if present) must be `BASE_URL` or one of `EXTRA_BASE_URLS`. In dev those are the Vite
  origins (`http://localhost:5173`, `http://porygon:5173`).
- Security headers and rate limits (`internal/httpapi/guard.go`): every response gets
  `nosniff`, `X-Frame-Options: DENY`, a framing-only CSP and (on https) HSTS. `/api/*` and
  `/auth/*` calls are rate-limited per client address (the last `X-Forwarded-For` entry, which
  HAProxy appends): 600 then 10/s overall, and 120 then 2/s on costly routes (address lookups,
  estimates, sign-in, new requests, payments, submissions). The limits are generous because a
  whole school can share one address. A refused call is a 429, logged as an `api` event with
  `status` 429. Production turns this on (`Deps.RateLimit`); tests turn it on only where they
  test it.
- No service worker. The old offline cache (removed 2026-10-17) served stale or failed asset
  loads after a deploy, and the page came up blank. 0 of 56 visitors in 30 days had installed the
  PWA. `web/static/service-worker.js` is a kill switch: browsers that still have the old worker
  load it, and it clears the caches, unregisters, and reloads the open tabs. Keep it, and don't
  add `web/src/service-worker.ts` back (SvelteKit registers it automatically).
