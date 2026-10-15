-- +goose Up
-- How people use the site: page views and time on page, clicks, cart changes,
-- shipping estimates, requests. Agents and admins read it to improve the site
-- (AGENTS.md, "Usage analytics"). No IPs, addresses or typed text.
CREATE TABLE usage_events (
    id      BIGSERIAL PRIMARY KEY,
    at      TIMESTAMPTZ NOT NULL DEFAULT now(),          -- when it happened (client clocks corrected to the server's)
    source  TEXT NOT NULL CHECK (source IN ('client', 'server')),
    name    TEXT NOT NULL,                               -- page_view, click, cart, shipping_estimate…
    visitor TEXT NOT NULL DEFAULT '',                    -- random id per browser (zmr_visitor cookie)
    visit   TEXT NOT NULL DEFAULT '',                    -- random id per tab, renewed after 30 idle minutes
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    path    TEXT NOT NULL DEFAULT '',                    -- the page it happened on
    props   JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX usage_events_at ON usage_events (at);
CREATE INDEX usage_events_name_at ON usage_events (name, at);
CREATE INDEX usage_events_visitor ON usage_events (visitor, at);
CREATE INDEX usage_events_user ON usage_events (user_id, at) WHERE user_id IS NOT NULL;

-- Time on each page view. The browser sends page_time snapshots (when the tab
-- hides and when the page closes); the latest one has the totals.
CREATE VIEW usage_page_times AS
SELECT DISTINCT ON (props->>'pv')
    props->>'pv' AS page_view, at, visitor, visit, user_id, path,
    (props->>'item_id')::bigint AS item_id,
    (props->>'active_ms')::bigint AS active_ms,
    (props->>'visible_ms')::bigint AS visible_ms,
    (props->>'scroll_pct')::int AS scroll_pct,
    (props->>'clicks')::int AS clicks
FROM usage_events
WHERE name = 'page_time' AND props ? 'pv'
ORDER BY props->>'pv', (props->>'visible_ms')::bigint DESC, at DESC;

-- +goose Down
DROP VIEW usage_page_times;
DROP TABLE usage_events;
