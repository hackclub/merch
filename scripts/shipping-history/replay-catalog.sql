-- Production, read-only: psql -At -f this.sql > catalog.json
-- Only packing inputs; no descriptions, credits, artwork, photos or people.
select coalesce(jsonb_agg(to_jsonb(i) order by i.id), '[]'::jsonb)
from (
    select id, sku, width_in, height_in, thickness_in, weight_oz,
           variants, packs, binding, ships_with
    from items order by id limit 10000
) i;
