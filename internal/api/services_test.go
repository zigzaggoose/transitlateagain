package api

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/zigzaggoose/transitlateagain/internal/match"
	"github.com/zigzaggoose/transitlateagain/internal/store"
)

// servicesHarness serves svcs from a fake store and records the query.
func servicesHarness(svcs []store.Service, err error) (harness, *store.ServiceQuery) {
	return servicesHarnessWithAlerts(svcs, nil, err)
}

func servicesHarnessWithAlerts(svcs []store.Service, alerts map[string]store.AlertText, err error) (harness, *store.ServiceQuery) {
	var got store.ServiceQuery
	return harness{h: NewHandler(Options{
		Schedules: func() []*match.Schedule { return []*match.Schedule{fixtureSchedule()} },
		Services: func(_ context.Context, q store.ServiceQuery) ([]store.Service, map[string]store.AlertText, error) {
			got = q
			return svcs, alerts, err
		},
		Stations: func(context.Context) ([]store.Station, error) {
			return []store.Station{{FeedID: "trains", StopID: "s1", Name: "Stop s1"}}, err
		},
		HistoryMaxDays: 90,
		OnTime:         thresholds,
		Now:            func() time.Time { return now },
		Log:            slog.New(slog.DiscardHandler),
	})}, &got
}

func sday(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }

func TestServices_ReportCardPerService(t *testing.T) {
	h, q := servicesHarnessWithAlerts([]store.Service{{
		FeedID: "trains", RouteID: "R1", DirectionID: 0, OriginStopID: "s9", OriginDepS: 7 * 3600,
		Headsign: "Emu Plains", DepartS: 7*3600 + 42*60, ArriveS: 25*3600 + 10*60,
		Days: []store.ServiceDay{
			{ServiceDate: sday(15), DelayS: ptr(int32(400)), AlertIDs: []string{"trackwork"}}, // late, with an alert
			{ServiceDate: sday(16), DelayS: ptr(int32(30))},                                   // on time
			{ServiceDate: sday(17), DelayS: ptr(int32(-120))},                                 // early
			{ServiceDate: sday(18), TripRel: 3},                                               // cancelled
			{ServiceDate: sday(19), DelayS: ptr(int32(1000))},                                 // very late
			{ServiceDate: sday(22), StopTimeRel: 1},                                           // skipped
			{ServiceDate: sday(23)},                                                           // no delay reported
		},
	}}, map[string]store.AlertText{"trackwork": {Header: "Buses replace trains", URL: "https://example.org/a"}}, nil)

	rec, body := h.get(t, "/v1/services?from=s1&to=s2")

	if rec.Code != 200 || body["count"] != 1.0 || body["day_type"] != "weekday" {
		t.Fatalf("got %d %v", rec.Code, body)
	}
	t.Run("the default asks for the last 28 service days, weekdays only, for the timetable's legs", func(t *testing.T) {
		if !q.LastDay.Equal(sday(23)) || !q.FirstDay.Equal(sday(23).AddDate(0, 0, -27)) || len(q.Weekdays) != 5 {
			t.Errorf("query = %+v", *q)
		}
		// trip-a calls at s1 at 11:10 and s2 at 11:20; trip-b and trip-c never reach s2.
		want := store.Leg{FeedID: "trains", TripID: "trip-a", DepartS: 11*3600 + 600, ArriveS: 11*3600 + 1200}
		if len(q.Legs) != 1 || q.Legs[0] != want || len(q.ToStops) != 1 || q.ToStops[0] != "s2" {
			t.Errorf("legs %+v to stops %v, want [%+v] to [s2]", q.Legs, q.ToStops, want)
		}
		if body["first_day"] != "2026-08-27" || body["last_day"] != "2026-09-23" {
			t.Errorf("range %v to %v", body["first_day"], body["last_day"])
		}
	})
	t.Run("history changes hourly, so a cache may keep it five minutes", func(t *testing.T) {
		if got := rec.Header().Get("Cache-Control"); got != servicesCache {
			t.Errorf("Cache-Control = %q", got)
		}
	})
	sv := body["services"].([]any)[0].(map[string]any)
	t.Run("times are the rider's clock, past midnight included", func(t *testing.T) {
		if sv["origin_departs"] != "07:00" || sv["departs"] != "07:42" || sv["arrives"] != "01:10" || sv["short_name"] != "T1" || sv["direction_id"] != 0.0 {
			t.Errorf("service = %v", sv)
		}
		if o := sv["origin"].(map[string]any); o["stop_id"] != "s9" || o["name"] != "Stop s9" {
			t.Errorf("origin = %v", o)
		}
	})
	t.Run("every day is counted once under its status", func(t *testing.T) {
		want := map[string]float64{"n_days": 7, "n_late": 1, "n_on_time": 1, "n_early": 1, "n_cancelled": 1, "n_very_late": 1, "n_skipped": 1, "n_unknown": 1}
		for k, v := range want {
			if sv[k] != v {
				t.Errorf("%s = %v, want %v", k, sv[k], v)
			}
		}
		days := sv["days"].([]any)
		if d := days[3].(map[string]any); d["service_date"] != "2026-09-18" || d["status"] != "cancelled" || d["delay_s"] != nil {
			t.Errorf("18th = %v", d)
		}
	})
	t.Run("a day's alerts are ids, their text given once", func(t *testing.T) {
		if d := sv["days"].([]any)[0].(map[string]any); len(d["alert_ids"].([]any)) != 1 || sv["n_alert_days"] != 1.0 {
			t.Errorf("15th = %v, n_alert_days %v", d, sv["n_alert_days"])
		}
		if _, ok := sv["days"].([]any)[1].(map[string]any)["alert_ids"]; ok {
			t.Error("a day with no alert carries alert_ids")
		}
		a := body["alerts"].(map[string]any)["trackwork"].(map[string]any)
		if a["header"] != "Buses replace trains" || a["url"] != "https://example.org/a" {
			t.Errorf("alerts = %v", body["alerts"])
		}
	})
	t.Run("the typical delay is the median of the days with one", func(t *testing.T) {
		// -120, 30, 400, 1000: the lower middle, as percentile_disc gives.
		if sv["typical_delay_s"] != 30.0 {
			t.Errorf("typical_delay_s = %v, want 30", sv["typical_delay_s"])
		}
	})
}

func TestServices_DayTypeAndRange(t *testing.T) {
	h, q := servicesHarness(nil, nil)

	rec, body := h.get(t, "/v1/services?from=s1&to=s2&day_type=saturday&days=7")

	if rec.Code != 200 || body["count"] != 0.0 {
		t.Fatalf("got %d %v", rec.Code, body)
	}
	if len(q.Weekdays) != 1 || q.Weekdays[0] != 6 || !q.FirstDay.Equal(sday(17)) {
		t.Errorf("query = %+v, want Saturdays from the 17th", *q)
	}
	if svcs, ok := body["services"].([]any); !ok || len(svcs) != 0 {
		t.Errorf("services = %v, want an empty list, not null", body["services"])
	}
}

func TestServices_BadInput_ReturnsTheEnvelope(t *testing.T) {
	h, _ := servicesHarness(nil, nil)
	cases := []struct {
		name, path string
		status     int
		code       string
	}{
		{"no to", "/v1/services?from=s1", 400, "invalid_parameter"},
		{"from and to the same", "/v1/services?from=s1&to=s1", 400, "invalid_parameter"},
		{"an unknown day type", "/v1/services?from=s1&to=s2&day_type=holiday", 400, "invalid_parameter"},
		{"a range past HISTORY_MAX_DAYS", "/v1/services?from=s1&to=s2&days=91", 400, "invalid_parameter"},
		{"a stop the timetable lacks", "/v1/services?from=s1&to=nowhere", 404, "stop_not_found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, body := h.get(t, c.path)
			assertEnvelope(t, rec, body, c.status, c.code)
		})
	}
	t.Run("a failing store is a 500 that says nothing of the cause", func(t *testing.T) {
		h, _ := servicesHarness(nil, errors.New("connection refused: password=hunter2"))
		rec, body := h.get(t, "/v1/services?from=s1&to=s2")
		assertEnvelope(t, rec, body, 500, "internal")
	})
}

func TestStations_ListsEveryStationCacheably(t *testing.T) {
	h, _ := servicesHarness(nil, nil)

	rec, body := h.get(t, "/v1/stations")

	if rec.Code != 200 || body["count"] != 1.0 || rec.Header().Get("Cache-Control") != stationsCache {
		t.Fatalf("got %d %v %v", rec.Code, body, rec.Header())
	}
	if st := body["stations"].([]any)[0].(map[string]any); st["stop_id"] != "s1" || st["name"] != "Stop s1" || st["feed_id"] != "trains" {
		t.Errorf("station = %v", st)
	}
}
