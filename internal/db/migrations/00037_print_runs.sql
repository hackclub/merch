-- +goose Up
ALTER TABLE print_requests ADD COLUMN slack_opt_in boolean NOT NULL DEFAULT false;
ALTER TABLE author_submissions ADD COLUMN slack_opt_in boolean NOT NULL DEFAULT false,
 ADD COLUMN initial_quantity int NOT NULL DEFAULT 0, ADD COLUMN address jsonb, ADD COLUMN deadline text NOT NULL DEFAULT '';
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
 details jsonb NOT NULL DEFAULT '{}',
 UNIQUE(run_id,version)
);
CREATE TABLE print_notifications (
 id bigserial PRIMARY KEY,
 run_id bigint NOT NULL REFERENCES print_runs(id),
 event_id bigint NOT NULL REFERENCES print_run_events(id),
 channel text NOT NULL CHECK(channel IN ('email','slack','shipment_email')),
 request_id bigint REFERENCES swag_requests(id),
 destination text NOT NULL,
 subject text NOT NULL,
 preview text NOT NULL DEFAULT '',
 body text NOT NULL,
 idempotency_key text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','sending','failed','sent','uncertain','cancelled','skipped')),
 attempts int NOT NULL DEFAULT 0,
 first_attempt_at timestamptz,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 error text NOT NULL DEFAULT '',
 UNIQUE(event_id,channel)
);
CREATE TABLE print_art_revisions (
 run_id bigint NOT NULL REFERENCES print_runs(id), revision int NOT NULL,
 front_url text NOT NULL, back_url text NOT NULL DEFAULT '', created_at timestamptz,
 PRIMARY KEY(run_id,revision)
);
CREATE TABLE print_printer_confirmations (
 run_id bigint NOT NULL REFERENCES print_runs(id),version bigint NOT NULL,url text NOT NULL,
 actor_id bigint REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(run_id,version)
);
ALTER TABLE sent_emails ADD COLUMN print_run_id bigint REFERENCES print_runs(id), ADD COLUMN print_event_id bigint REFERENCES print_run_events(id);
CREATE INDEX print_notifications_pending ON print_notifications(next_attempt_at) WHERE status IN ('queued','failed');
CREATE TABLE print_run_po_links (
 run_id bigint NOT NULL REFERENCES print_runs(id), po_id bigint NOT NULL, line_id bigint NOT NULL, sku text NOT NULL,
 evidence jsonb NOT NULL, confirmation_url text NOT NULL, revision int NOT NULL,
 PRIMARY KEY(po_id,line_id), UNIQUE(run_id,po_id,line_id),
 FOREIGN KEY(run_id,revision) REFERENCES print_art_revisions(run_id,revision)
);
CREATE TABLE print_po_observations (
 run_id bigint NOT NULL REFERENCES print_runs(id), po_id bigint NOT NULL, line_id bigint NOT NULL,
 synced_at timestamptz NOT NULL, polled_at timestamptz NOT NULL, evidence jsonb NOT NULL,
 PRIMARY KEY(run_id,po_id,line_id,synced_at)
);

-- Preserve source IDs and snapshots. Unknown stage dates stay NULL. Importing
-- history never queues mail or creates a parcel. Stocked needs a link audit.
INSERT INTO print_runs(user_id,source_print_id,origin,stage,request,data,shipment_request_id,created_at)
 SELECT user_id,id,'legacy',CASE status WHEN 'stocked' THEN 'received' ELSE status END,to_jsonb(p)||jsonb_build_object('needed_by',needed_by::timestamp AT TIME ZONE 'UTC'),
 jsonb_build_object('legacy_review',true,'legacy_status',status,'sku',sku,'public_note',admin_note),shipment_request_id,created_at FROM print_requests p;
INSERT INTO print_runs(user_id,source_author_id,origin,stage,request,data,created_at)
 SELECT user_id,id,'legacy',CASE status WHEN 'stocked' THEN 'received' WHEN 'printing' THEN 'approved' ELSE status END,
 to_jsonb(a)||jsonb_build_object('product','custom','front_url',file_url,'note',description,'initial_quantity',0),
 jsonb_build_object('legacy_review',true,'legacy_status',status,'sku',sku,'public_note',admin_note),created_at FROM author_submissions a;
INSERT INTO print_art_revisions(run_id,revision,front_url,back_url,created_at)
 SELECT id,1,request->>'front_url',coalesce(request->>'back_url',''),created_at FROM print_runs;
INSERT INTO print_run_events(run_id,stage,action,event_at,note,source,version)
 SELECT id,stage,'legacy_import',CASE WHEN stage='submitted' AND data->>'legacy_status'='submitted' THEN created_at ELSE NULL END,
 'Imported source snapshot; other stage dates unknown. Existing listing and shipment links require review.','legacy',1 FROM print_runs;
INSERT INTO print_run_events(run_id,stage,action,event_at,note,source,version)
 SELECT id,'submitted','legacy_submission',created_at,'Original request creation time','legacy',0 FROM print_runs
 WHERE stage<>'submitted' OR data->>'legacy_status'<>'submitted';
-- Recover only transitions whose event times were actually saved. Negative
-- versions distinguish imported audit records from the new optimistic version.
WITH transitions AS (
 SELECT DISTINCT ON (r.id,e.props->>'to',e.at) r.id AS run_id,e.props->>'to' AS original_stage,e.at,e.id AS event_id
 FROM usage_events e JOIN print_runs r ON
 (e.name='print_request_updated' AND e.props->>'print_request_id'=r.source_print_id::text) OR
 (e.name='submission_updated' AND e.props->>'submission_id'=r.source_author_id::text)
 WHERE e.props->>'to' IN ('submitted','approved','ordered','printing','stocked','rejected')
 ORDER BY r.id,e.props->>'to',e.at,e.id
)
INSERT INTO print_run_events(run_id,stage,action,event_at,note,source,version)
 SELECT run_id,CASE original_stage WHEN 'stocked' THEN 'received' WHEN 'printing' THEN 'approved' ELSE original_stage END,
 'legacy_status_change',at,'Original status: '||original_stage,'usage_events',
 -row_number() over(partition by run_id order by at,event_id) FROM transitions;

-- Inserts through either established entry path get a canonical identity in the
-- same transaction. The snapshot is immutable; reorders are distinct source rows.
-- +goose StatementBegin
CREATE FUNCTION new_print_run() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint; eid bigint; snapshot jsonb; src text;
BEGIN
 snapshot=to_jsonb(NEW);
 IF TG_TABLE_NAME='print_requests' THEN
  src='staff';
  snapshot=snapshot||jsonb_build_object('needed_by',NEW.needed_by::timestamp AT TIME ZONE 'UTC');
  INSERT INTO print_runs(user_id,source_print_id,origin,stage,request,data) VALUES(NEW.user_id,NEW.id,src,'submitted',snapshot,jsonb_build_object('slack_opt_in',NEW.slack_opt_in)) RETURNING id INTO rid;
 ELSE
  src='author'; snapshot=snapshot||jsonb_build_object('product','custom','front_url',NEW.file_url,'note',NEW.description);
  INSERT INTO print_runs(user_id,source_author_id,origin,stage,request,data) VALUES(NEW.user_id,NEW.id,src,'submitted',snapshot,jsonb_build_object('slack_opt_in',NEW.slack_opt_in)) RETURNING id INTO rid;
 END IF;
 INSERT INTO print_art_revisions(run_id,revision,front_url,back_url,created_at) VALUES(rid,1,snapshot->>'front_url',coalesce(snapshot->>'back_url',''),NEW.created_at);
 INSERT INTO print_run_events(run_id,stage,action,actor_id,event_at,source,version) VALUES(rid,'submitted','submit',NEW.user_id,now(),src,1) RETURNING id INTO eid;
 INSERT INTO print_notifications(run_id,event_id,channel,destination,subject,body)
 SELECT rid,eid,'email',email,'Print request received',
 'We received your '||NEW.title||' print request. HQ will review the artwork and quantity. No order has been placed.' FROM users WHERE id=NEW.user_id;
 IF NEW.slack_opt_in THEN
 INSERT INTO print_notifications(run_id,event_id,channel,destination,subject,body)
 SELECT rid,eid,'slack',slack_id,'Print request received','We received your print request. HQ will review the artwork and quantity. No order has been placed.' FROM users WHERE id=NEW.user_id AND slack_id<>'';
 END IF;
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
DROP TABLE print_art_revisions;
DROP TABLE print_printer_confirmations;
ALTER TABLE sent_emails DROP COLUMN print_run_id, DROP COLUMN print_event_id;
DROP TABLE print_notifications;
DROP TABLE print_run_events;
DROP TABLE print_runs;
ALTER TABLE print_requests DROP COLUMN slack_opt_in;
ALTER TABLE author_submissions DROP COLUMN slack_opt_in,DROP COLUMN initial_quantity,DROP COLUMN address,DROP COLUMN deadline;
