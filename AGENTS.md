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
| Hack clubber (anyone signed in) | Hack Club Auth login | Request listed swag. US shipping is free. Outside the US the request starts in `awaiting_payment`, and the person pays a flat fee on HCB before we ship. |
| YSWS author | Their HCA email matches `Hack Club Auth Email` in the **YSWS Authors** table of the Unified YSWS Projects DB (Airtable `app3A5kJwYqxMLOgh`) | Submit print-ready material (stickers, posters…) to be printed and stocked in the warehouse. |
| Admin | Email is in `ADMIN_EMAILS` (admins are also authors) | Manage global limits, list warehouse SKUs as items, set visibility and order, review requests (mark paid → ship → tracking), and review author submissions. |

Items can come in **sizes** (`items.variants`, e.g. the red Hack Club T-shirt `Swa/Shirt/HC` with
S/M/L/XL → `Swa/Shirt/HC/<size>`). Each size ships its own warehouse SKU; limits count the item as a
whole. Request lines store the exact SKU shipped (`swag_request_items.sku`, `variant`). A SKU belongs
to one item: saving sizes folds any item listed under a size's SKU (e.g. imported per-size shirts)
into the sized item, history included, and the importer attaches size SKUs to the sized item.

Request status machine (`internal/swag`): `awaiting_payment → pending → dispatched`. From
`awaiting_payment` or `pending`, a request can also go to `rejected` or `cancelled`. Rejected and
cancelled requests don't count toward limits or cooldowns.

## Layout

```
cmd/server/          main: env config → migrate → wire deps → HTTP server
internal/config/     env-var config (12-factor), no file reads
internal/swag/       pure business rules: limits, cooldown, intl shipping, status transitions
internal/store/      Postgres persistence (pgx); per-user lock around limit check + insert
internal/db/         pool, embedded goose migrations (internal/db/migrations/*.sql)
internal/db/dbtest/  per-test throwaway database (CREATE DATABASE test_<rand> … DROP)
internal/auth/       Hack Club Auth OAuth client (auth.hackclub.com, GET /api/v1/me)
internal/authors/    YSWS author lookup (Airtable REST, 10 min cache)
internal/theseus/    mail.hackclub.com warehouse client (SKUs, warehouse_orders)
internal/cdn/        cdn.hackclub.com upload client (item images)
internal/imgprep/    shrinks uploaded artwork to ≤1600px JPEGs before upload
internal/httpapi/    routes, role middleware, CSRF guard, SPA static serving
web/                 SvelteKit SPA (adapter-static, ssr=false) + service worker + manifest
```

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
  limits and cooldown.
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
- Production is private for now (2026-10-14). `merch.hackclub.com` sits behind Orchard login
  (protection mode `orchard`; `/healthz` bypasses it), and `ALLOWED_EMAILS=zach@hackclub.com`
  refuses every other Hack Club Auth sign-in. To open it, set protection to `none` and remove
  `ALLOWED_EMAILS`.
- **Every push to `main` builds and deploys to production** (Orchard auto-deploy, about 3
  minutes). Landing on `main` is shipping. Check a rollout with `list_pods` (the image tag) and
  `get_image_history`, not `get_deployment`.
- Production started on 2026-10-14 as a copy of the dev DB (items, the Fillout import, users).
  Since then the two have diverged: make catalog changes in production through the app. Dev
  sets `THESEUS_ORDER_TAG=merch-dev` and production uses `merch`, so the same request id in both
  databases never shares a warehouse idempotency key.
- One container: `Dockerfile`, which builds the PWA and a static Go binary on distroless. It
  listens on `$PORT` (default 8080). Health check: `GET /healthz` (pings the DB).
- Migrations run automatically at startup (goose, embedded).
- Required env: `DATABASE_URL`, `BASE_URL` (public https origin), `SESSION_SECRET`,
  `HCA_CLIENT_ID`, `HCA_CLIENT_SECRET`, `THESEUS_API_KEY`, `ADMIN_EMAILS`. Also needed:
  `AIRTABLE_API_KEY` for authors, `HCB_SHIPPING_PAYMENT_URL` for international payments, and
  `HACKCLUB_CDN_API_KEY` for item image uploads.
- `$BASE_URL/auth/callback` must be registered as a redirect URI on the Hack Club Auth app.
- Read the `orchard-ops` skill before touching Orchard (`pdw call hack_club_orchard_k8s__…`).

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
  - A billing profile is required; it comes from the API key's default.
  - **Never call CreateOrder outside the admin "Ship" action. It ships real mail.**
- **cdn.hackclub.com** hosts item images. Admins upload them on the admin Items tab; the
  server re-encodes them as ≤1600px JPEGs (print PNGs are ~9 MB) and stores the CDN URLs in
  `items.images` (ordered; the first is the thumbnail). Each item has a product page at
  `/items/<id>-<slug>`. Printed items with `width_in`/`height_in` set open on a 3D view
  (`web/src/lib/Product3D.svelte` + `paper3d.ts`, three.js, lazy-loaded): images[0] and
  images[1] are the front and back of a sheet with real thickness (`thickness_in`) and paper
  surface (`finish`: matte, gloss, linen, uncoated), next to a phone at the same scale.
  `photos` holds real-world pictures people posted (`{url, caption, credit}`), shown as
  "In the wild" on the product page.
  Booklets (`binding` = `staple` or `perfect`) open in the 3D view and flip through
  `items.pages` (page images, front cover first; `web/src/lib/book3d.ts`). The magazines'
  pages come from the public PDFs on magazine.hackclub.com, split into single pages.
  `ships_with` lists SKUs added to the warehouse order once per unit (the Magazine 2025
  always ships with its intro card, `Pri/M25/4x6/1st`), and the product page shows them.
- **Print source files** for warehouse SKUs: the Marketing warehouse Airtable
  (`appK53aN0fz3sgJ4w`, table `warehouse_skus`) has `re_order_instructions`, e.g. "Order
  VP_MJR069GX on zach@hackclub.com Vistaprint". The Vistaprint order-confirmation email links a
  `storage.documents.cimpress.io` design document; its panels (Front/Back) link the original
  print files (`api.sherbert.cimpress.io/.../print?signature=…`). The Hackpad poster
  (`Pri/HPd/11x/1st`) and 4x6 card (`Pri/HPd/4x6/1st`) use the same front/back artwork.
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
- The service worker never caches `/api/*` or `/auth/*`. Keep it that way; limits and statuses
  must be live.
