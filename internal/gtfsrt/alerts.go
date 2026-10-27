package gtfsrt

import (
	"fmt"
	"slices"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

// Alert is one service alert flattened, its text in English. Cause and
// effect are raw integers for the reason the schedule relationships are
// (§15): a producer may send a value the specification does not name.
type Alert struct {
	ID          string
	Cause       int32
	Effect      int32
	Header      string
	Description string // "" when absent
	URL         string // "" when absent
	// Periods is when the alert applies. Empty means always, as the
	// specification says; a nil Start or End is open on that side.
	Periods []Period
	// What it informs, each id once. An entity naming only an agency or a
	// route type is not kept: nothing here is keyed by either.
	RouteIDs, StopIDs, TripIDs []string
}

// Period is one active period. Nil is unbounded.
type Period struct{ Start, End *time.Time }

// AlertFeed is one decoded alerts body.
type AlertFeed struct {
	HeaderTS  time.Time // the feed's own timestamp; FetchedAt when it sent none
	FetchedAt time.Time
	Alerts    []Alert
}

// DecodeAlerts decodes a GTFS-realtime alerts body. Entities without an
// alert, or without an id or header, are skipped: there is nothing to key or
// show them by.
func DecodeAlerts(body []byte, fetchedAt time.Time) (AlertFeed, error) {
	var m gtfs.FeedMessage
	if err := proto.Unmarshal(body, &m); err != nil {
		return AlertFeed{}, fmt.Errorf("decode alerts (%d bytes, starting %s): %w", len(body), hexPrefix(body, 8), err)
	}
	f := AlertFeed{HeaderTS: fetchedAt, FetchedAt: fetchedAt}
	if ts := m.GetHeader().GetTimestamp(); ts > 0 {
		f.HeaderTS = time.Unix(int64(ts), 0).UTC()
	}
	for _, e := range m.GetEntity() {
		a := e.GetAlert()
		if a == nil || e.GetId() == "" {
			continue
		}
		al := Alert{
			ID:          e.GetId(),
			Cause:       int32(a.GetCause()),
			Effect:      int32(a.GetEffect()),
			Header:      english(a.GetHeaderText()),
			Description: english(a.GetDescriptionText()),
			URL:         english(a.GetUrl()),
		}
		if al.Header == "" {
			continue
		}
		for _, p := range a.GetActivePeriod() {
			per := Period{Start: unixOrNil(p.GetStart()), End: unixOrNil(p.GetEnd())}
			// A period ending before it starts is no time at all, and Postgres
			// rejects it as a range, which would lose the whole poll.
			if per.Start != nil && per.End != nil && !per.End.After(*per.Start) {
				continue
			}
			al.Periods = append(al.Periods, per)
		}
		for _, ie := range a.GetInformedEntity() {
			if id := ie.GetRouteId(); id != "" {
				al.RouteIDs = append(al.RouteIDs, id)
			}
			if id := ie.GetStopId(); id != "" {
				al.StopIDs = append(al.StopIDs, id)
			}
			if id := ie.GetTrip().GetTripId(); id != "" {
				al.TripIDs = append(al.TripIDs, id)
			}
		}
		// TfNSW repeats an entity once per direction (2,809 of 2,942 informed
		// entities name one, measured 2026-10-27), so a route comes twice.
		for _, ids := range []*[]string{&al.RouteIDs, &al.StopIDs, &al.TripIDs} {
			slices.Sort(*ids)
			*ids = slices.Compact(*ids)
		}
		f.Alerts = append(f.Alerts, al)
	}
	return f, nil
}

// english is the plain-text English translation: TfNSW sends "en" and
// "en/html" side by side. Failing those, the first, which is what a
// producer that sends one untagged translation means.
func english(t *gtfs.TranslatedString) string {
	tr := t.GetTranslation()
	for _, lang := range []string{"en", ""} {
		for _, x := range tr {
			if x.GetLanguage() == lang {
				return x.GetText()
			}
		}
	}
	for _, x := range tr {
		if x.GetLanguage() != "en/html" {
			return x.GetText()
		}
	}
	return ""
}

// unixOrNil reads an active period bound; 0 is the specification's "open".
func unixOrNil(s uint64) *time.Time {
	if s == 0 {
		return nil
	}
	t := time.Unix(int64(s), 0).UTC()
	return &t
}
