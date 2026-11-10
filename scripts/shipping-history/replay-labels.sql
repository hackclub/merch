-- Hack Club data warehouse, read-only. psql --csv -v since=YYYY-MM-DD
-- -v until=YYYY-MM-DD -f this.sql > labels.csv. until is exclusive.
-- Use order numbers, not tracking: letter tracking placeholders can be shared.
-- The private intermediate carries order keys; the replay JSON does not.
-- Include every service: substituted carriers are part of the quote error.
select order_number, country,
       case when service ilike 'USPS First Class Mail%' then 'letter' else 'parcel' end as class,
       weight * 16 as weight_oz,
       round((shipping_handling * 100)::numeric) as postage_cents
from agh_fulfillment_zenventory.shipments
where shipped_date >= :'since'::date and shipped_date < :'until'::date
  and weight > 0 and shipping_handling > 0
order by shipped_date, order_number
limit 10000;
