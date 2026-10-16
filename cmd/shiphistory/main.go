// Command shiphistory loads past shipments for shipping estimates.
//
//	go run ./cmd/shiphistory track [-cache asendia.csv] raw.csv … > history.csv   # add Asendia delivery dates
//	go run ./cmd/shiphistory load history.csv …              # replace shipping_history in $DATABASE_URL
//
// The CSVs come from the data warehouse (scripts/shipping-history/*.sql). They
// have no names or addresses, but the raw exports carry tracking numbers: keep
// them out of the repo. track drops tracking numbers from its output. Asendia
// only keeps about six months of tracking, so -cache keeps every delivery date
// found (tracking,date lines) for the next refresh. USPS and UPS block scripted
// lookups, so their delivery dates go into the same cache from Safari
// (scripts/shipping-history/us-tracking.py).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hackclub/merch/internal/asendia"
	"github.com/hackclub/merch/internal/db"
	"github.com/hackclub/merch/internal/importer"
	"github.com/hackclub/merch/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "track":
		fs := flag.NewFlagSet("track", flag.ExitOnError)
		workers := fs.Int("workers", 4, "lookups at once")
		since := fs.String("since", "", "only look up parcels shipped on or after this date (YYYY-MM-DD)")
		cache := fs.String("cache", "", "CSV of tracking,delivered_on to reuse and extend")
		fs.Parse(os.Args[2:])
		err = track(*workers, *since, *cache, fs.Args())
	case "load":
		err = load(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "shiphistory:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: shiphistory track [-workers N] raw.csv… > history.csv | shiphistory load history.csv…")
	os.Exit(2)
}

func read(files []string) ([]importer.Shipment, error) {
	var all []importer.Shipment
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		ships, err := importer.ParseShipments(fh)
		fh.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		all = append(all, ships...)
	}
	return all, nil
}

// track fills in delivery dates for Asendia parcels from Asendia's tracking.
func track(workers int, since, cachePath string, files []string) error {
	ships, err := read(files)
	if err != nil {
		return err
	}
	cache, err := readCache(cachePath)
	if err != nil {
		return err
	}
	ctx := context.Background()
	c := &asendia.Client{}
	todo := applyCache(ships, cache, since)
	fmt.Fprintf(os.Stderr, "looking up %d Asendia parcels\n", len(todo))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done, delivered, failed := 0, 0, 0
	for range max(workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				at, ok, err := c.Delivered(ctx, ships[i].Tracking)
				mu.Lock()
				done++
				switch {
				case err != nil:
					failed++
					if failed <= 5 {
						fmt.Fprintln(os.Stderr, "  ", err)
					}
				case ok:
					d := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
					ships[i].DeliveredOn = &d
					cache[ships[i].Tracking] = d
					delivered++
				}
				if done%250 == 0 {
					fmt.Fprintf(os.Stderr, "  %d/%d (%d delivered, %d failed)\n", done, len(todo), delivered, failed)
				}
				mu.Unlock()
			}
		}()
	}
	for _, i := range todo {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	fmt.Fprintf(os.Stderr, "%d delivered, %d not yet or unknown, %d failed\n", delivered, len(todo)-delivered-failed, failed)
	if err := writeCache(cachePath, cache); err != nil {
		return err
	}
	return importer.WriteShipments(os.Stdout, ships, false)
}

// applyCache fills in delivery dates from the cache, whatever the carrier, and
// returns the Asendia parcels still to look up (shipped on or after since).
func applyCache(ships []importer.Shipment, cache map[string]time.Time, since string) []int {
	var todo []int
	for i, s := range ships {
		if s.DeliveredOn != nil || s.Tracking == "" {
			continue
		}
		if d, ok := cache[s.Tracking]; ok {
			ships[i].DeliveredOn = &d
		} else if strings.HasPrefix(s.Tracking, "AS") && (since == "" || s.ShippedOn.Format(time.DateOnly) >= since) {
			todo = append(todo, i)
		}
	}
	return todo
}

func readCache(path string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if path == "" {
		return out, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return out, nil
	} else if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		tn, date, ok := strings.Cut(line, ",")
		if !ok {
			continue
		}
		if d, err := time.Parse(time.DateOnly, date); err == nil {
			out[tn] = d
		}
	}
	return out, nil
}

func writeCache(path string, cache map[string]time.Time) error {
	if path == "" {
		return nil
	}
	var b strings.Builder
	for tn, d := range cache {
		fmt.Fprintf(&b, "%s,%s\n", tn, d.Format(time.DateOnly))
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func load(files []string) error {
	ships, err := read(files)
	if err != nil {
		return err
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.New(pool).ReplaceShippingHistory(ctx, ships); err != nil {
		return err
	}
	fmt.Printf("loaded %d shipments\n", len(ships))
	return nil
}
