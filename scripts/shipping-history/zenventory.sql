-- Warehouse (AGH/Zenventory) shipments since 2025, one CSV line per shipment:
-- source,country,class,weight_oz,postage_cents,ordered_on,shipped_on,delivered_on,tracking
-- class: "letter" = USPS First Class Mail (stickers in an envelope, domestic or
-- international); "parcel" = everything else we ship by default: USPS Ground
-- Advantage, UPS SurePost or FedEx in the US, Asendia abroad (up to 3 lb).
-- Expedited and freight shipments are left out. delivered_on is filled in later
-- (Asendia tracking); tracking is dropped before import. ordered_on is left
-- empty: handling time comes from theseus-orders.sql, the orders placed the way
-- this app places them (these include bulk and hand-batched orders).
select concat_ws(',',
    'zenventory',
    s.country,
    case when s.service ilike 'USPS First Class Mail%' then 'letter' else 'parcel' end,
    round((s.weight * 16)::numeric, 2),
    round((s.shipping_handling * 100)::numeric),
    '',
    s.shipped_date,
    '',
    case when s.carrier = 'Asendia' then s.tracking_number else '' end
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
    or (s.country = 'US' and (s.service in ('USPS Ground Advantage', 'UPS SurePost 1 lb or Greater', 'UPS SurePost Less than 1 lb')
                              or (s.carrier = 'FedEx' and s.service is null)))
  )
order by s.shipped_date
