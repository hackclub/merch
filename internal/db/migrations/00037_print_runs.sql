-- +goose Up
CREATE TABLE print_runs (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id),
 source_print_id bigint UNIQUE REFERENCES print_requests(id),
 source_author_id bigint UNIQUE REFERENCES author_submissions(id),
 origin text NOT NULL CHECK(origin IN ('staff','author','legacy')),
 stage text NOT NULL CHECK(stage IN ('submitted','approved','ordered','in_transit','received','available','rejected','cancelled')),
 version bigint NOT NULL DEFAULT 1,
 request jsonb NOT NULL,
 data jsonb NOT NULL DEFAULT '{}',
 internal_note text NOT NULL DEFAULT '',
 shipment_request_id bigint UNIQUE REFERENCES swag_requests(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE print_run_events (
 id bigserial PRIMARY KEY,
 run_id bigint NOT NULL REFERENCES print_runs(id),
 stage text NOT NULL,
 action text NOT NULL,
 actor_id bigint REFERENCES users(id),
 event_at timestamptz,
 recorded_at timestamptz NOT NULL DEFAULT now(),
 note text NOT NULL DEFAULT '',
 source text NOT NULL,
 version bigint NOT NULL,
 UNIQUE(run_id,version)
);
CREATE TABLE print_notifications (
 id bigserial PRIMARY KEY,
 run_id bigint NOT NULL REFERENCES print_runs(id),
 event_id bigint NOT NULL REFERENCES print_run_events(id),
 channel text NOT NULL CHECK(channel IN ('email','slack')),
 destination text NOT NULL,
 subject text NOT NULL,
 body text NOT NULL,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','sending','failed','sent','uncertain')),
 attempts int NOT NULL DEFAULT 0,
 first_attempt_at timestamptz,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 error text NOT NULL DEFAULT '',
 UNIQUE(event_id,channel)
);
ALTER TABLE sent_emails ADD COLUMN print_run_id bigint REFERENCES print_runs(id), ADD COLUMN print_event_id bigint REFERENCES print_run_events(id);
CREATE INDEX print_notifications_pending ON print_notifications(next_attempt_at) WHERE status IN ('queued','failed');
CREATE TABLE print_run_po_links (
 run_id bigint NOT NULL REFERENCES print_runs(id), po_id bigint NOT NULL, line_id bigint NOT NULL, sku text NOT NULL,
 evidence jsonb NOT NULL, confirmation_url text NOT NULL, revision int NOT NULL,
 PRIMARY KEY(po_id,line_id), UNIQUE(run_id,po_id,line_id)
);
CREATE TABLE print_po_observations (
 run_id bigint NOT NULL REFERENCES print_runs(id), po_id bigint NOT NULL, line_id bigint NOT NULL,
 synced_at timestamptz NOT NULL, polled_at timestamptz NOT NULL, evidence jsonb NOT NULL,
 PRIMARY KEY(run_id,po_id,line_id,synced_at)
);

-- Preserve source IDs and snapshots. Unknown stage dates stay NULL. Importing
-- history never queues mail or creates a parcel. Stocked needs a link audit.
INSERT INTO print_runs(user_id,source_print_id,origin,stage,request,data,shipment_request_id,created_at)
 SELECT user_id,id,'legacy',CASE status WHEN 'stocked' THEN 'received' ELSE status END,to_jsonb(p),
 jsonb_build_object('legacy_review',true,'legacy_status',status,'sku',sku),shipment_request_id,created_at FROM print_requests p;
INSERT INTO print_runs(user_id,source_author_id,origin,stage,request,data,created_at)
 SELECT user_id,id,'legacy',CASE status WHEN 'stocked' THEN 'received' WHEN 'printing' THEN 'submitted' ELSE status END,
 to_jsonb(a)||jsonb_build_object('product','custom','front_url',file_url,'note',description,'initial_quantity',0),
 jsonb_build_object('legacy_review',true,'legacy_status',status,'sku',sku),created_at FROM author_submissions a;
INSERT INTO print_run_events(run_id,stage,action,event_at,note,source,version)
 SELECT id,stage,'legacy_import',CASE WHEN stage='submitted' AND data->>'legacy_status'='submitted' THEN created_at ELSE NULL END,
 'Imported source snapshot; other stage dates unknown. Existing listing and shipment links require review.','legacy',1 FROM print_runs;

-- Inserts through either established entry path get a canonical identity in the
-- same transaction. The snapshot is immutable; reorders are distinct source rows.
-- +goose StatementBegin
CREATE FUNCTION new_print_run() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint; eid bigint; snapshot jsonb; src text;
BEGIN
 snapshot=to_jsonb(NEW);
 IF TG_TABLE_NAME='print_requests' THEN
  src='staff';
  INSERT INTO print_runs(user_id,source_print_id,origin,stage,request) VALUES(NEW.user_id,NEW.id,src,'submitted',snapshot) RETURNING id INTO rid;
 ELSE
  src='author'; snapshot=snapshot||jsonb_build_object('product','custom','front_url',NEW.file_url,'note',NEW.description,'initial_quantity',0);
  INSERT INTO print_runs(user_id,source_author_id,origin,stage,request) VALUES(NEW.user_id,NEW.id,src,'submitted',snapshot) RETURNING id INTO rid;
 END IF;
 INSERT INTO print_run_events(run_id,stage,action,actor_id,event_at,source,version) VALUES(rid,'submitted','submit',NEW.user_id,now(),src,1) RETURNING id INTO eid;
 INSERT INTO print_notifications(run_id,event_id,channel,destination,subject,body)
 SELECT rid,eid,'email',email,'Print request received',
 'We received your '||NEW.title||' print request. HQ will review the artwork and quantity. No order has been placed.' FROM users WHERE id=NEW.user_id;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER staff_print_run AFTER INSERT ON print_requests FOR EACH ROW EXECUTE FUNCTION new_print_run();
CREATE TRIGGER author_print_run AFTER INSERT ON author_submissions FOR EACH ROW EXECUTE FUNCTION new_print_run();

-- +goose Down
DROP TRIGGER author_print_run ON author_submissions;
DROP TRIGGER staff_print_run ON print_requests;
DROP FUNCTION new_print_run();
DROP TABLE print_po_observations;
DROP TABLE print_run_po_links;
ALTER TABLE sent_emails DROP COLUMN print_run_id, DROP COLUMN print_event_id;
DROP TABLE print_notifications;
DROP TABLE print_run_events;
DROP TABLE print_runs;
