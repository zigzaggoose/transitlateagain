//go:build integration

package store

import (
	"context"
	"testing"
	"time"
)

func TestServices_EachLegsService_WithEveryTripIDsDaysAtTheDestination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx, embedded(t)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, sql := range []string{
		`INSERT INTO schedule_versions (id, feed_id, sha256, active) VALUES (1, 'f', 'a', true)`,
		// Two stations with platforms, and a stop with no parent.
		`INSERT INTO stops (version_id, stop_id, name, parent_station, location_type) VALUES
			(1, 'HBY', 'Hornsby', NULL, 1), (1, 'HBY1', 'Hornsby Platform 1', 'HBY', 0), (1, 'HBY2', 'Hornsby Platform 2', 'HBY', 0),
			(1, 'CEN', 'Central', NULL, 1), (1, 'CEN1', 'Central Platform 1', 'CEN', 0),
			(1, 'MID', 'Midway', NULL, 0)`,
		// T-742b is the same 07:42 under an older timetable's trip id.
		`INSERT INTO trip_services (feed_id, trip_id, route_id, direction_id, origin_stop_id, origin_dep_s, headsign, first_version_id) VALUES
			('f', 'T-742a', 'R', 0, 'HBY', 27720, 'Central', 1), ('f', 'T-742b', 'R', 0, 'HBY', 27720, 'Central', 0),
			('f', 'T-742c', 'R', 0, 'HBY', 27720, 'Central', 1), ('f', 'T-742d', 'R', 0, 'HBY', 27720, 'Ashfield', 1),
			('f', 'T-800', 'R', 0, 'HBY', 28800, NULL, 1), ('f', 'T-REV', 'R', 1, 'CEN', 27000, NULL, 1)`,
		`INSERT INTO stop_visits (service_date, feed_id, trip_id, stop_id, observed_delay_s, trip_rel, stop_time_rel) VALUES
			('2026-10-20', 'f', 'T-742a', 'CEN1', 120, 0, 0),
			('2026-10-21', 'f', 'T-742a', 'CEN1', NULL, 3, 0),
			('2026-10-13', 'f', 'T-742b', 'CEN1', 400, 0, 0),
			('2026-10-20', 'f', 'T-742a', 'MID', 999, 0, 0),
			('2026-10-25', 'f', 'T-742a', 'CEN1', 5, 0, 0),
			('2026-09-01', 'f', 'T-742a', 'CEN1', 5, 0, 0),
			('2026-10-24', 'f', 'T-800', 'CEN1', 0, 0, 0),
			('2026-10-20', 'f', 'T-REV', 'CEN1', 0, 0, 0)`,
	} {
		if _, err := s.pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

	// What match.Schedule.Legs gives for Hornsby to Central: not T-REV, which
	// runs the other way and has a visit at Central of its own.
	legs := []Leg{{"f", "T-742a", 27720, 30000}, {"f", "T-742c", 27720, 30000}, {"f", "T-742d", 27720, 30000}, {"f", "T-800", 28800, 31200}}
	q := ServiceQuery{Legs: legs, ToStops: []string{"CEN", "CEN1"}, FirstDay: day(12), LastDay: day(25), Weekdays: []int32{1, 2, 3, 4, 5}}
	got, err := s.Services(ctx, q)
	if err != nil {
		t.Fatalf("services: %v", err)
	}

	// Not T-800: it ran on a weekend only in the range.
	if len(got) != 1 {
		t.Fatalf("got %d services %+v, want the 07:42 only", len(got), got)
	}
	sv := got[0]
	if sv.OriginStopID != "HBY" || sv.OriginDepS != 27720 || sv.DepartS != 27720 || sv.ArriveS != 30000 {
		t.Errorf("service = %+v", sv)
	}
	t.Run("the headsign is the one most of its trips show, not the first alphabetically", func(t *testing.T) {
		if sv.Headsign != "Central" {
			t.Errorf("headsign = %q, want Central (two trips) over Ashfield (one)", sv.Headsign)
		}
	})
	t.Run("days come from every trip id of the service, at the destination only, on the weekdays asked for", func(t *testing.T) {
		if len(sv.Days) != 3 {
			t.Fatalf("days = %+v, want 13, 20 and 21 October", sv.Days)
		}
		want := []struct {
			date    time.Time
			delay   int32
			tripRel int32
		}{{day(13), 400, 0}, {day(20), 120, 0}, {day(21), 0, 3}}
		for i, w := range want {
			d := sv.Days[i]
			if !d.ServiceDate.Equal(w.date) || d.TripRel != w.tripRel {
				t.Errorf("day %d = %+v, want %s trip_rel %d", i, d, w.date.Format(time.DateOnly), w.tripRel)
			}
			if w.tripRel == 0 && (d.DelayS == nil || *d.DelayS != w.delay) {
				t.Errorf("day %d delay = %v, want %d", i, d.DelayS, w.delay)
			}
		}
	})
	t.Run("a cancelled day has no delay rather than the previous row's", func(t *testing.T) {
		if d := sv.Days[2]; d.DelayS != nil {
			t.Errorf("21 October delay = %d, want none", *d.DelayS)
		}
	})
	t.Run("the weekend asks for the weekend service", func(t *testing.T) {
		q := q
		q.Weekdays = []int32{6, 7}
		got, err := s.Services(ctx, q)
		if err != nil {
			t.Fatalf("services: %v", err)
		}
		if len(got) != 2 || got[0].DepartS != 27720 || len(got[0].Days) != 1 || got[1].DepartS != 28800 {
			t.Errorf("got %+v, want the 07:42 (Sunday 25th) then the 08:00", got)
		}
	})

	stations, err := s.Stations(ctx)
	if err != nil {
		t.Fatalf("stations: %v", err)
	}
	var names []string
	for _, st := range stations {
		names = append(names, st.StopID)
	}
	if len(names) != 3 || names[0] != "CEN" || names[1] != "HBY" || names[2] != "MID" {
		t.Errorf("stations = %v, want CEN HBY MID by name, no platforms", names)
	}
}
