//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/zigzaggoose/transitlateagain/internal/gtfsrt"
)

func TestUpsertAlerts_KeepsFirstSeenAndTheLatestWord(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx, embedded(t)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	at := func(h int) time.Time { return time.Date(2026, 10, 27, h, 0, 0, 0, time.UTC) }
	ptr := func(t time.Time) *time.Time { return &t }
	feed := func(ts time.Time, header string) gtfsrt.AlertFeed {
		return gtfsrt.AlertFeed{HeaderTS: ts, Alerts: []gtfsrt.Alert{
			{ID: "A", Cause: 9, Effect: 6, Header: header, RouteIDs: []string{"NSN_1a", "NSN_2k"},
				Periods: []gtfsrt.Period{{Start: ptr(at(10)), End: ptr(at(14))}, {Start: ptr(at(34))}}},
			{ID: "B", Header: "Always"},
		}}
	}
	if err := s.UpsertAlerts(ctx, feed(at(9), "first")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.UpsertAlerts(ctx, feed(at(11), "second")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A replay of an older body: nothing may go back.
	if err := s.UpsertAlerts(ctx, feed(at(10), "stale")); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var first, last time.Time
	var header string
	var routes, trips []string
	if err := s.pool.QueryRow(ctx, `SELECT first_seen_ts, last_seen_ts, header, route_ids, trip_ids FROM service_alerts WHERE alert_id = 'A'`).
		Scan(&first, &last, &header, &routes, &trips); err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Run("first seen stays, last seen and the text move forward only", func(t *testing.T) {
		if !first.Equal(at(9)) || !last.Equal(at(11)) || header != "second" {
			t.Errorf("first %s last %s header %q", first, last, header)
		}
		if len(routes) != 2 || trips == nil || len(trips) != 0 {
			t.Errorf("routes %v trips %v, want two routes and an empty, not null, list", routes, trips)
		}
	})
	t.Run("the active periods are a multirange, an open end unbounded", func(t *testing.T) {
		for _, c := range []struct {
			at   time.Time
			want bool
		}{{at(12), true}, {at(15), false}, {at(34 + 24*30), true}} {
			var in bool
			if err := s.pool.QueryRow(ctx, `SELECT active @> $1::timestamptz FROM service_alerts WHERE alert_id = 'A'`, c.at).Scan(&in); err != nil {
				t.Fatalf("read: %v", err)
			}
			if in != c.want {
				t.Errorf("active at %s = %v, want %v", c.at, in, c.want)
			}
		}
	})
	t.Run("an alert with no period is always active", func(t *testing.T) {
		var in bool
		if err := s.pool.QueryRow(ctx, `SELECT active @> '1999-01-01T00:00:00Z'::timestamptz FROM service_alerts WHERE alert_id = 'B'`).Scan(&in); err != nil || !in {
			t.Errorf("active = %v, err %v", in, err)
		}
	})
}
