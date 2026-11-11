package store

import (
	"testing"
)

func TestPrintCreationInGoIsAtomic(t *testing.T) {
	for _, origin := range []string{"staff", "author"} {
		for _, failTable := range []string{"", "print_runs", "print_art_revisions", "print_run_events", "print_notifications"} {
			t.Run(origin+"/"+failTable, func(t *testing.T) {
				s := newStore(t)
				u := mkUser(t, s, "creator", "creator@x.com")
				var triggers int
				if err := s.pool.QueryRow(ctx, `select count(*) from pg_trigger where tgname in ('staff_print_run','author_print_run')`).Scan(&triggers); err != nil || triggers != 0 {
					t.Fatalf("runtime run creation must be Go, not a trigger: %d %v", triggers, err)
				}
				if failTable != "" {
					if _, err := s.pool.Exec(ctx, `alter table `+failTable+` add constraint demo_failure check(false) not valid`); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				if origin == "staff" {
					_, err = s.CreatePrintRequest(ctx, PrintRequest{UserID: u.ID, Product: "postcard_4x6", Title: "Demo cards", Quantity: 800, FrontURL: "https://example.com/art.png"}, testPrintMessages)
				} else {
					_, err = s.CreateSubmission(ctx, Submission{UserID: u.ID, Program: "Demo", Title: "Demo cards", Kind: "postcard", Quantity: 800, FileURL: "https://example.com/art.png"}, testPrintMessages)
				}
				if (err != nil) != (failTable != "") {
					t.Fatalf("creation error: %v", err)
				}
				want := 1
				if failTable != "" {
					want = 0
				}
				for _, table := range []string{"print_runs", "print_art_revisions", "print_run_events", "print_notifications"} {
					var count int
					if e := s.pool.QueryRow(ctx, `select count(*) from `+table).Scan(&count); e != nil || count != want {
						t.Fatalf("%s count %d want %d: %v", table, count, want, e)
					}
				}
				var sources int
				if e := s.pool.QueryRow(ctx, `select (select count(*) from print_requests)+(select count(*) from author_submissions)`).Scan(&sources); e != nil || sources != want {
					t.Fatalf("source survived failed run creation: %d %v", sources, e)
				}
				if failTable == "" {
					var channel, subject, body, source string
					if e := s.pool.QueryRow(ctx, `select n.channel,n.subject,n.body,r.origin from print_notifications n join print_runs r on r.id=n.run_id`).Scan(&channel, &subject, &body, &source); e != nil || channel != "email" || subject != "stage=submitted" || source != origin || body == "" {
						t.Fatalf("received notice %q %q %q %q: %v", channel, subject, body, source, e)
					}
				}
			})
		}
	}
}
