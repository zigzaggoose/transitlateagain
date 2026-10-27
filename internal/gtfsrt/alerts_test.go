package gtfsrt

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

// testdata/alerts_all.pb is five alerts of the live v2/gtfs/alerts/all feed,
// recorded 2026-10-27: one of each informed-entity shape TfNSW sends.
func TestDecodeAlerts_RecordedFeed(t *testing.T) {
	body := fixture(t, "alerts_all.pb")
	fetched := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)

	f, err := DecodeAlerts(body, fetched)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !f.HeaderTS.Equal(time.Date(2026, 10, 27, 10, 48, 14, 0, time.UTC)) || len(f.Alerts) != 5 {
		t.Fatalf("header %s, %d alerts", f.HeaderTS, len(f.Alerts))
	}
	byID := map[string]Alert{}
	for _, a := range f.Alerts {
		byID[a.ID] = a
	}
	t1 := byID["975fad9c-1b68-567e-9c09-539dab3232f5"]
	t.Run("a route alert keeps each route once, its English text and both periods", func(t *testing.T) {
		if !slices.Equal(t1.RouteIDs, []string{"NSN_1a", "NSN_2a", "NSN_2i", "NSN_2k"}) {
			t.Errorf("routes = %v", t1.RouteIDs)
		}
		if !strings.HasPrefix(t1.Header, "T1 North Shore Line: Buses replace trains between Hornsby and Milsons Point.") {
			t.Errorf("header = %q", t1.Header)
		}
		if t1.Cause != 9 || t1.Effect != 6 || t1.URL != "https://transportnsw.info/alerts/details#/ems-80074" {
			t.Errorf("cause %d effect %d url %q", t1.Cause, t1.Effect, t1.URL)
		}
		if len(t1.Periods) != 2 || !t1.Periods[1].Start.Equal(time.Date(2026, 10, 27, 10, 20, 0, 0, time.UTC)) ||
			!t1.Periods[1].End.Equal(time.Date(2026, 10, 27, 14, 30, 0, 0, time.UTC)) {
			t.Errorf("periods = %+v", t1.Periods)
		}
	})
	t.Run("the description is the plain text, not the HTML translation beside it", func(t *testing.T) {
		if d := t1.Description; d == "" || d[0] == '<' {
			t.Errorf("description = %.60q", d)
		}
	})
	t.Run("a trip alert keeps its trip, and an open end is nil", func(t *testing.T) {
		a := byID["211d6aac-0777-54dd-8cb1-7c7124143748"]
		if !slices.Equal(a.TripIDs, []string{"N843.974.159.48.D.8.88564982"}) || len(a.RouteIDs) != 0 {
			t.Errorf("trips %v routes %v", a.TripIDs, a.RouteIDs)
		}
		if len(a.Periods) != 1 || a.Periods[0].Start == nil || a.Periods[0].End != nil {
			t.Errorf("periods = %+v, want a start and no end", a.Periods)
		}
	})
	t.Run("a stop alert keeps its stops", func(t *testing.T) {
		if a := byID["03456f5c-b973-5cdb-861e-832ad39c035b"]; !slices.Equal(a.StopIDs, []string{"200940", "200948"}) {
			t.Errorf("stops = %v", a.StopIDs)
		}
	})
}

func TestDecodeAlerts_EdgeCases(t *testing.T) {
	str := func(s string) *string { return &s }
	u64 := func(v uint64) *uint64 { return &v }
	text := func(pairs ...string) *gtfs.TranslatedString {
		ts := &gtfs.TranslatedString{}
		for i := 0; i < len(pairs); i += 2 {
			ts.Translation = append(ts.Translation, &gtfs.TranslatedString_Translation{Language: str(pairs[i]), Text: str(pairs[i+1])})
		}
		return ts
	}
	ver := "2.0"
	m := &gtfs.FeedMessage{
		Header: &gtfs.FeedHeader{GtfsRealtimeVersion: &ver},
		Entity: []*gtfs.FeedEntity{
			{Id: str("no-alert")},
			{Id: str("no-header"), Alert: &gtfs.Alert{}},
			{Id: str("html-only-first"), Alert: &gtfs.Alert{HeaderText: text("en/html", "<b>x</b>", "fr", "Retard")}},
			{Id: str("untagged"), Alert: &gtfs.Alert{HeaderText: text("", "Delays")}},
			{Id: str("backwards"), Alert: &gtfs.Alert{HeaderText: text("en", "Oops"), ActivePeriod: []*gtfs.TimeRange{
				{Start: u64(2000), End: u64(1000)}, {Start: u64(1000), End: u64(2000)},
			}}},
		},
	}
	body, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	fetched := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)

	f, err := DecodeAlerts(body, fetched)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	t.Run("no header timestamp falls back to our clock", func(t *testing.T) {
		if !f.HeaderTS.Equal(fetched) {
			t.Errorf("header ts = %s", f.HeaderTS)
		}
	})
	t.Run("entities with no alert or no header are skipped", func(t *testing.T) {
		if len(f.Alerts) != 3 {
			t.Fatalf("alerts = %+v", f.Alerts)
		}
	})
	t.Run("with no English, any text but HTML", func(t *testing.T) {
		if f.Alerts[0].Header != "Retard" || f.Alerts[1].Header != "Delays" {
			t.Errorf("headers %q %q", f.Alerts[0].Header, f.Alerts[1].Header)
		}
	})
	t.Run("no active period means always, as an empty list", func(t *testing.T) {
		if f.Alerts[1].Periods != nil {
			t.Errorf("periods = %+v", f.Alerts[1].Periods)
		}
	})
	t.Run("a period that ends before it starts is dropped, the others kept", func(t *testing.T) {
		if p := f.Alerts[2].Periods; len(p) != 1 || p[0].Start.Unix() != 1000 {
			t.Errorf("periods = %+v", p)
		}
	})
	t.Run("a body that is not protobuf is an error", func(t *testing.T) {
		if _, err := DecodeAlerts([]byte("<html>502</html>"), fetched); err == nil {
			t.Error("no error")
		}
	})
}
