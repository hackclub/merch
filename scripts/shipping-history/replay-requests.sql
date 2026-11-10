-- Production, read-only. psql --csv -v since=YYYY-MM-DD -v until=YYYY-MM-DD
-- -f this.sql > requests.csv. Bounds apply to mailing dates; until is exclusive.
-- This private intermediate has request ids and warehouse order keys, never people
-- or addresses. shiphistory replay drops both keys from its JSON output.
-- Unchanged contents only: later admin edits must not look like packing errors.
with selected as (
    select * from swag_requests
    where source = 'app'
      and mailed_at >= :'since'::date and mailed_at < :'until'::date
    order by id limit 10000
)
select r.id, r.country,
       (r.created_at at time zone 'UTC')::date as created_on,
       'hack.club/' || r.theseus_order_id as order_number,
       r.shipping_fee_cents - r.refunded_cents as charged_cents,
       q.props->>'postage_typical_cents' as quoted_cents,
       q.props->>'weight_oz' as quoted_weight_oz,
       r.warehouse_postage_cents as actual_cents,
       c.props->'lines' as lines,
       not exists (
           select 1 from swag_request_items l
           where l.request_id = r.id and l.requested_quantity is not null
             and l.requested_quantity <> l.quantity
       ) as unchanged
from selected r
left join lateral (
    select visitor, props, at from usage_events
    where name = 'request_created'
      and at >= r.created_at - interval '1 minute'
      and at < r.created_at + interval '1 day'
      and props->>'request_id' = r.id::text
    order by at limit 1
) c on true
left join lateral (
    select e.props from usage_events e
    where e.name = 'shipping_estimate'
      and e.at <= c.at and e.at > c.at - interval '7 days'
      and (e.user_id = r.user_id or (e.visitor <> '' and e.visitor = c.visitor))
      and e.props->>'country' = r.country
      and e.props->'lines' = c.props->'lines'
      and e.props ? 'weight_oz'
    order by e.at desc limit 1
) q on true
order by r.id;
