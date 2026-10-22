# Hack Club Merch: agent API

This API lets an agent review swag requests on Hack Club Merch ({BASE_URL}). It can read
requests and who made them, leave notes for the admins, and ship or reject requests.
An admin makes the API key on the Admin page (API keys tab). The key acts for that admin,
but it only works on `/api/agent/*`. It can't change items, settings or keys.

## Calling it

- Send the key with every call: `Authorization: Bearer merch_…`.
- `POST` bodies must be JSON, sent with `Content-Type: application/json`. Send `{}` when there's nothing to say.
- Errors come back as `{"error": "…"}` with a 4xx or 5xx status. A 409 means the request
  isn't in a state that allows the action (for example, it's already shipped).
- `/api/agent` is open past the site's Orchard login. The key is all you need.

```sh
curl -H "Authorization: Bearer $MERCH_API_KEY" "{BASE_URL}/api/agent/requests?status=pending"
```

## Endpoints

### `GET /api/agent/requests?status=pending`

Lists requests, newest first (at most 500). `status` is one of `awaiting_payment`
(abroad, postage not paid yet; you can't ship these), `pending` (waiting for review), `dispatched`, `rejected` or
`cancelled`; leave it out to list all of them. Each request has:

- `id`, `status`, `created_at`, `user_id`, `user_email`, `purpose` (`personal`, `club` or `event`)
  and `note`: what the requester wrote, word for word (how they'll use it; club and event
  requests must write one, personal ones have none). Read it before you judge the quantities.
- `address` (name, street, city, state, postal code, country and phone).
- `lines`: `{item_id, sku, name, variant, quantity}`. `variant` is the size, for sized items.
- `shipping_fee_cents` and `paid_at`: what the requester paid for postage abroad. The US is free.
- `internal_note`: Markdown notes by admins and agents. The requester never sees it.
- `admin_note`: the note shown to the requester.
- `estimate`: the package, its weight, postage and arrival dates.
- `costs`: what sending it costs Hack Club (items, labor, packaging and postage), in cents.
  Shipped (`dispatched`) requests keep it, priced on what actually shipped: the parts the
  warehouse has charged for are its real figures, and `costs_actual` names them (`items` and
  `labor` once it takes the order, `postage` once it's mailed and you've refreshed). The rest
  are estimates. The dispatch and refresh replies include both.

### `GET /api/agent/requests/{id}`

One request, with the same fields, plus:

- `requester`: `id`, `email`, `name`, `slack_id`, `verification_status` (from Hack Club
  Auth; `verified` means their identity was checked), `created_at` (their first sign-in),
  `last_login_at` and `imported` (`true` means they're known only from the old request forms).
- `history`: all their other requests (any status, newest first, including requests from before this app).
  Use it to see how much they've already been sent, and how recently.
- `same_address`: other accounts' requests to the same street address and postal code.
  Siblings and club leaders can share an address. Many accounts at one address can also mean
  one person asking many times.
- `emails`: what we emailed the requester about this request, newest first (see below).

### `GET /api/agent/emails`

Every email the site sent, word for word, newest first (100 by default): `id`, `at`, `kind`
(`received`, `dispatched` or `rejected`), `request_id`, `to`, `subject`, `preview`, `body`
(plain text), `status` (`sent`, `failed`, or `unsent` when no mailer is set up) and `error`.
Filter with `?request_id=`, `?kind=`, `?status=`; page with `?before=<id>` and `?limit=` (up
to 500). Use it to audit what people are told: wording that's confusing, wrong, or reads oddly.

### `GET /api/agent/items`

Every item: its limits (`max_per_request`, `max_per_user`, `min_per_request`), who it's
for (`club_only`), `visible`, sizes (`variants`) and what it `ships_with`.

### `POST /api/agent/requests/{id}/notes`

`{"note": "…"}` (1 to 4000 characters of Markdown). Adds a paragraph to the request's internal note,
signed with the key's name and the time. It never replaces what is already there. Write
down what you checked and why you decided, so an admin (or your next run) can follow it.

### `POST /api/agent/requests/{id}/dispatch`

Approves a `pending` request and **sends it to the warehouse right away. This mails real
packages, and you can't undo it.** The requester gets an email that it shipped.

```json
{"lines": [{"sku": "Sti/Bra/O&H/Lap", "quantity": 20}], "note": "We sent 20 so there are enough to go around."}
```

- `lines` is optional. List only the lines you want to change. Use a line's `sku`, and
  `quantity` 0 to leave it out. The rest ship as requested. You can send more of a line than
  they asked for, and you can add a visible item without sizes that isn't in the request (its
  item `sku` from `/api/agent/items`, up to its `max_per_request`). You can't change sizes.
  An added line comes back with `requested_quantity` 0, and the shipped email lists it.
- `note` is optional and goes in the shipped email to the requester. Say why you sent less.
- If you send less of a request that was paid by card, the postage is quoted again and the difference
  goes back to the card. Check `warning` in the reply: it describes anything that an admin must fix by hand.
- The warehouse order has an idempotency key, so you can safely retry after a timeout.

### `POST /api/agent/requests/{id}/reject`

`{"note": "…"}` rejects a `pending` or `awaiting_payment` request. The note goes in the
email to the requester, so write it to them: kind, short and specific. A card payment is refunded in
full. If the refund fails, you get a 502 and the request stays open. If Stripe holds the refund
for a person to approve, the 502 says so: leave a `Needs a human:` note and don't retry until it's approved.

### `POST /api/agent/requests/{id}/refresh`

`{}` asks mail.hackclub.com for a `dispatched` request's tracking and saves its
`tracking_number` and `carrier` (empty until the warehouse mails it), `mailed_at`, and what the
warehouse charged (`warehouse_costs`: `contents_cents`, `labor_cents`, and `postage_cents` once
it's mailed). Returns the request, with `costs` and `costs_actual` updated. A 409
means it hasn't been dispatched. Nothing pulls tracking on its own, so check what you shipped
on later runs until it has a tracking number.

## Leaving a request for a human

If you aren't sure, don't ship or reject it. Add a note that starts with `Needs a human:`
and says what you'd need to know. It stays `pending`, so your next run will see it again.
If you find your own `Needs a human:` note and no admin has written since, skip the request.

## Reviewing

Each request is reviewed before it ships. There's no cooldown between requests. Item
limits are already checked when the request is made. Your job is the judgment that the
rules can't make. Hack Club aims to spend at most $50 per new teenager who joins and ships a
project. Use `costs` to see what a request costs, and judge whether it's worth it. Things to
weigh: whether the reason (`note`, `purpose`) fits the quantities; what they were sent before
(`history`); repeated requests to one address (`same_address`); and whether the requester is a real
Hack Club member (`verification_status`, `slack_id`, and how long ago they joined).
