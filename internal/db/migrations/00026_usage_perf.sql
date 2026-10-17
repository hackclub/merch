-- +goose Up
-- How smoothly each page view ran (page_perf) and how each 3D view loaded and
-- drew (view3d). Both are sent as snapshots (when the tab is hidden, and at
-- the end), so these keep the latest one of each.
CREATE VIEW usage_page_perf AS
SELECT DISTINCT ON (props->>'pv')
    props->>'pv' AS page_view, at, visitor, visit, user_id, path,
    (props->>'item_id')::bigint AS item_id,
    (props->>'frames')::int AS frames,
    (props->>'fps')::float AS fps,
    (props->>'hz')::int AS hz,
    (props->>'frame_p50')::float AS frame_p50,
    (props->>'frame_p95')::float AS frame_p95,
    (props->>'frame_p99')::float AS frame_p99,
    (props->>'frame_max')::float AS frame_max,
    (props->>'jank')::int AS jank,
    (props->>'dropped_pct')::float AS dropped_pct,
    coalesce((props->>'loaf')::int, 0) AS loaf,
    coalesce((props->>'loaf_blocking_ms')::int, 0) AS loaf_blocking_ms,
    coalesce(props->'loaf_scripts', '[]') AS loaf_scripts,
    (props->>'views')::int AS views,
    (props->>'views_max')::int AS views_max,
    (props->>'lost')::int AS lost,
    (props->>'heap_mb')::int AS heap_mb,
    release
FROM usage_events
WHERE name = 'page_perf' AND props ? 'pv'
ORDER BY props->>'pv', (props->>'frames')::int DESC NULLS LAST, at DESC;

CREATE VIEW usage_view3d AS
SELECT DISTINCT ON (props->>'vid')
    props->>'vid' AS view_id, at, visitor, visit, user_id, path,
    props->>'view' AS view, props->>'kind' AS kind,
    (props->>'item_id')::bigint AS item_id,
    (props->>'load_ms')::int AS load_ms,
    (props->>'ready_ms')::int AS ready_ms,
    (props->>'alive_ms')::int AS alive_ms,
    (props->>'draws')::int AS draws,
    (props->>'render_p50')::float AS render_p50,
    (props->>'render_p95')::float AS render_p95,
    (props->>'render_max')::float AS render_max,
    (props->>'frames')::int AS frames,
    (props->>'fps')::float AS fps,
    (props->>'hz')::int AS hz,
    (props->>'frame_p95')::float AS frame_p95,
    (props->>'jank')::int AS jank,
    (props->>'dropped_pct')::float AS dropped_pct,
    props->>'px' AS px,
    (props->>'dpr')::float AS dpr,
    props->>'gpu' AS gpu,
    (props->>'lost')::boolean AS lost,
    (props->>'failed')::boolean AS failed,
    release
FROM usage_events
WHERE name = 'view3d' AND props ? 'vid'
ORDER BY props->>'vid', (props->>'final')::boolean DESC NULLS LAST, (props->>'alive_ms')::int DESC NULLS LAST, at DESC;

-- +goose Down
DROP VIEW usage_view3d;
DROP VIEW usage_page_perf;
