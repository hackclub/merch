-- +goose Up
-- Which build of the site each event came from (the web build's time, e.g.
-- 2026-10-17T14:16:45Z), so changes can be compared before and after they
-- shipped. Server events carry the server's build, browser events the tab's.
ALTER TABLE usage_events ADD COLUMN release TEXT NOT NULL DEFAULT '';

-- Time on each page view, now with its release and the items that were on
-- screen (at least half, for a second) during it.
DROP VIEW usage_page_times;
CREATE VIEW usage_page_times AS
SELECT DISTINCT ON (props->>'pv')
    props->>'pv' AS page_view, at, visitor, visit, user_id, path,
    (props->>'item_id')::bigint AS item_id,
    (props->>'active_ms')::bigint AS active_ms,
    (props->>'visible_ms')::bigint AS visible_ms,
    (props->>'scroll_pct')::int AS scroll_pct,
    (props->>'clicks')::int AS clicks,
    coalesce(props->'seen', '[]') AS seen,
    release
FROM usage_events
WHERE name = 'page_time' AND props ? 'pv'
ORDER BY props->>'pv', (props->>'visible_ms')::bigint DESC, at DESC;

-- +goose Down
DROP VIEW usage_page_times;
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
ALTER TABLE usage_events DROP COLUMN release;
