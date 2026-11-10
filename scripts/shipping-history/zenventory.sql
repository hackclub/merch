-- Warehouse (AGH/Zenventory) shipments since 2025, one CSV line per shipment:
-- source,country,class,weight_oz,postage_cents,ordered_on,shipped_on,delivered_on,tracking
-- class: "letter" = USPS First Class Mail (stickers in an envelope, domestic or
-- international); "parcel" = everything else we ship by default: USPS Ground
-- Advantage, UPS SurePost, UPS Ground or FedEx in the US, Asendia abroad (up to 3 lb).
-- Boxes over about 10 lb go UPS Ground (2025-26: 87 of 108 US parcels of 10-40 lb).
-- Expedited and freight shipments are left out. delivered_on is filled in later
-- (Asendia, USPS and UPS tracking); tracking is dropped before import. ordered_on is left
-- empty: handling time comes from theseus-orders.sql, the orders placed the way
-- this app places them (these include bulk and hand-batched orders).
-- Asendia postage from 2026-10-01 through 2026-10-20 is left out (kept for transit):
-- those labels were logged at their country's 5 oz price whatever the
-- weight (a 39.4 oz parcel to India at $9.05, against $26.83 in September), so
-- it isn't the rate card. From 2026-10-21, labels match the weight-based cards
-- again and match the postage charges recorded by mail.hackclub.com.
select concat_ws(',',
    'zenventory',
    s.country,
    case when s.service ilike 'USPS First Class Mail%' then 'letter' else 'parcel' end,
    round((s.weight * 16)::numeric, 2),
    case when s.carrier = 'Asendia' and s.shipped_date >= '2026-10-01'
              and s.shipped_date < '2026-10-21' then ''
         else round((s.shipping_handling * 100)::numeric)::text end,
    '',
    s.shipped_date,
    '',
    case when s.carrier = 'Asendia' or (s.country = 'US' and s.service not ilike 'USPS First Class Mail%')
         then s.tracking_number else '' end
) r
from agh_fulfillment_zenventory.shipments s
join agh_fulfillment_zenventory.customer_orders o using (order_number)
where s.shipped_date >= '2025-01-01'
  and s.country ~ '^[A-Z]{2}$'
  and s.weight > 0 and s.shipping_handling > 0
  and s.shipped_date >= (o.ordered_date at time zone 'America/New_York')::date
  and (
    s.service ilike 'USPS First Class Mail%'
    or (s.country <> 'US' and s.carrier = 'Asendia' and s.weight <= 3)
    or (s.country = 'US' and (s.service in ('USPS Ground Advantage', 'UPS SurePost 1 lb or Greater', 'UPS SurePost Less than 1 lb',
                                            'UPS Ground', 'FedEx Ground', 'FedEx Home Delivery')
                              or (s.carrier = 'FedEx' and s.service is null)))
  )
order by s.shipped_date
