package printpo

import (
	"context"
	"testing"

	"github.com/hackclub/merch/internal/db/dbtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScopeAllowsExtensionViewsButRejectsOtherData(t *testing.T) {
	owner := dbtest.New(t)
	ctx := context.Background()
	_, err := owner.Exec(ctx, `CREATE SCHEMA agh_fulfillment_zenventory;
 CREATE TABLE agh_fulfillment_zenventory.purchase_orders(id bigint,order_number text,supplier_name text,draft boolean,deleted boolean,completed boolean,created_date timestamptz,required_by_date date,completed_date timestamptz,_synced_at timestamptz,items jsonb,private_note text);
 CREATE EXTENSION hstore;
 CREATE VIEW public.demo_extension_view AS SELECT 1 AS example;
 ALTER EXTENSION hstore ADD VIEW public.demo_extension_view;
 GRANT SELECT ON public.demo_extension_view TO PUBLIC;
 CREATE VIEW public.demo_application_view AS SELECT 'invented private data' AS private_note;
 CREATE TABLE public.demo_application_table(private_note text);`)
	if err != nil {
		t.Fatal(err)
	}
	role := "print_scope_" + owner.Config().ConnConfig.Database
	ident := pgx.Identifier{role}.Sanitize()
	if _, err = owner.Exec(ctx, `CREATE ROLE `+ident+` NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Exec(ctx, `DROP OWNED BY `+ident); owner.Exec(ctx, `DROP ROLE `+ident) })
	if _, err = owner.Exec(ctx, `GRANT USAGE ON SCHEMA agh_fulfillment_zenventory TO `+ident+`;
 GRANT SELECT(id,order_number,supplier_name,draft,deleted,completed,created_date,required_by_date,completed_date,_synced_at,items) ON agh_fulfillment_zenventory.purchase_orders TO `+ident); err != nil {
		t.Fatal(err)
	}
	cfg := owner.Config().Copy()
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error { _, err := c.Exec(ctx, `SET ROLE `+ident); return err }
	scoped, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scoped.Close)
	if err = verifyScope(ctx, scoped); err != nil {
		t.Fatalf("PUBLIC-readable extension member must not block the landing role: %v", err)
	}
	for _, object := range []string{"public.demo_application_view", "public.demo_application_table"} {
		if _, err = owner.Exec(ctx, `GRANT SELECT ON `+object+` TO PUBLIC`); err != nil {
			t.Fatal(err)
		}
		if err = verifyScope(ctx, scoped); err == nil {
			t.Fatalf("accepted unrelated PUBLIC-readable object %s", object)
		}
		if _, err = owner.Exec(ctx, `REVOKE SELECT ON `+object+` FROM PUBLIC`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = owner.Exec(ctx, `GRANT SELECT ON pg_catalog.pg_authid TO `+ident); err != nil {
		t.Fatal(err)
	}
	if err = verifyScope(ctx, scoped); err == nil {
		t.Fatal("protected system data is not part of the PUBLIC catalog baseline")
	}
	if _, err = owner.Exec(ctx, `REVOKE SELECT ON pg_catalog.pg_authid FROM `+ident); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, `GRANT UPDATE ON public.demo_extension_view TO `+ident); err != nil {
		t.Fatal(err)
	}
	if err = verifyScope(ctx, scoped); err == nil {
		t.Fatal("extension ownership must not allow write privileges")
	}
	if _, err = owner.Exec(ctx, `REVOKE UPDATE ON public.demo_extension_view FROM `+ident+`; GRANT pg_read_all_stats TO `+ident); err != nil {
		t.Fatal(err)
	}
	if err = verifyScope(ctx, scoped); err == nil {
		t.Fatal("statistics privileges could expose other users' SQL")
	}
}
