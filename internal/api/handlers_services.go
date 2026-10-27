package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/zigzaggoose/transitlateagain/internal/servicetime"
	"github.com/zigzaggoose/transitlateagain/internal/store"
)

// Both endpoints change at most once a rollup (hourly), and the site asks
// for the same few station pairs over and over: let Cloudflare answer them.
const (
	stationsCache = "public, max-age=3600"
	servicesCache = "public, max-age=300"
)

type stationRow struct {
	StopID string `json:"stop_id"`
	Name   string `json:"name"`
	FeedID string `json:"feed_id"`
}

func (s *server) stations(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSchedule(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()
	sts, err := s.Stations(ctx)
	if err != nil {
		s.Log.Error("stations query failed", "component", "api", "request_id", requestID(r.Context()), "err", err.Error())
		writeError(w, r, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	out := make([]stationRow, len(sts))
	for i, st := range sts {
		out[i] = stationRow{StopID: st.StopID, Name: st.Name, FeedID: st.FeedID}
	}
	w.Header().Set("Cache-Control", stationsCache)
	writeJSON(w, http.StatusOK, map[string]any{"stations": out, "count": len(out)})
}

type stopRef struct {
	StopID string `json:"stop_id"`
	Name   string `json:"name,omitempty"`
}

type servicesResponse struct {
	From     stopRef      `json:"from"`
	To       stopRef      `json:"to"`
	DayType  string       `json:"day_type"`
	FirstDay string       `json:"first_day"`
	LastDay  string       `json:"last_day"`
	Services []serviceRow `json:"services"`
	Count    int          `json:"count"`
	// Alerts is every alert a day below names, by id: TfNSW's own words on
	// what was disrupted. Days carry ids so one alert is not repeated across
	// the twenty services it touched.
	Alerts map[string]alertText `json:"alerts"`
}

type alertText struct {
	Header string `json:"header"`
	URL    string `json:"url,omitempty"`
}

type serviceRow struct {
	FeedID      string  `json:"feed_id"`
	RouteID     string  `json:"route_id"`
	ShortName   string  `json:"short_name,omitempty"`
	DirectionID *int16  `json:"direction_id"`
	Headsign    string  `json:"headsign,omitempty"`
	Origin      stopRef `json:"origin"`
	// OriginDeparts, with the route, direction and origin, is the service's
	// key: "the 07:30 from Berowra", which a rider boarding later still means.
	OriginDeparts string `json:"origin_departs"`
	Departs       string `json:"departs"` // scheduled at from, HH:MM on the Sydney clock
	Arrives       string `json:"arrives"` // scheduled at to

	NDays      int `json:"n_days"`
	NEarly     int `json:"n_early"`
	NOnTime    int `json:"n_on_time"`
	NLate      int `json:"n_late"`
	NVeryLate  int `json:"n_very_late"`
	NCancelled int `json:"n_cancelled"`
	NSkipped   int `json:"n_skipped"`
	NUnknown   int `json:"n_unknown"`
	// NAlertDays is how many of the days had a published alert covering the run.
	NAlertDays int `json:"n_alert_days"`
	// TypicalDelayS is the median arrival delay at to over the days that
	// have one, the lower middle value as the rollups' percentile_disc.
	TypicalDelayS *int32 `json:"typical_delay_s"`

	Days []serviceDay `json:"days"`
}

type serviceDay struct {
	ServiceDate string   `json:"service_date"`
	DelayS      *int32   `json:"delay_s"`
	Status      string   `json:"status"`
	AlertIDs    []string `json:"alert_ids,omitempty"`
}

// dayTypes are ISO weekdays, as Postgres's isodow numbers them.
var dayTypes = map[string][]int32{
	"weekday":  {1, 2, 3, 4, 5},
	"saturday": {6},
	"sunday":   {7},
	"all":      {1, 2, 3, 4, 5, 6, 7},
}

// wallClock renders seconds into a service day as the time a rider reads off a
// timetable: 25:10:00 is "01:10".
func wallClock(secs int32) string {
	secs %= 86400
	return fmt.Sprintf("%02d:%02d", secs/3600, secs%3600/60)
}

func (s *server) services(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	from, to := params.Get("from"), params.Get("to")
	if from == "" || to == "" || from == to {
		writeError(w, r, http.StatusBadRequest, "invalid_parameter", "from and to must be two different stop ids")
		return
	}
	dayType := params.Get("day_type")
	if dayType == "" {
		dayType = "weekday"
	}
	weekdays, ok := dayTypes[dayType]
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_parameter", fmt.Sprintf("day_type must be weekday, saturday, sunday or all, got %q", dayType))
		return
	}
	days, ok := intParam(w, r, "days", 28, 1, s.HistoryMaxDays)
	if !ok {
		return
	}
	t, ok := s.requireSchedule(w, r)
	if !ok {
		return
	}
	fromName, okFrom := t.stopName(from)
	toName, okTo := t.stopName(to)
	if !okFrom || !okTo {
		writeError(w, r, http.StatusNotFound, "stop_not_found", "from and to must both be stops in the active schedule")
		return
	}

	last := servicetime.CandidateServiceDates(s.Now(), 0)[0]
	first := last.AddDate(0, 0, 1-days)
	q := store.ServiceQuery{FirstDay: first, LastDay: last, Weekdays: weekdays}
	for feedID, sch := range t {
		legs, toStops := sch.Legs(from, to)
		for _, l := range legs {
			q.Legs = append(q.Legs, store.Leg{FeedID: feedID, TripID: l.TripID, DepartS: l.DepartS, ArriveS: l.ArriveS})
		}
		q.ToStops = append(q.ToStops, toStops...)
	}
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()
	svcs, alerts, err := s.Services(ctx, q)
	if err != nil {
		s.Log.Error("services query failed", "component", "api", "request_id", requestID(r.Context()), "err", err.Error())
		writeError(w, r, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	resp := servicesResponse{
		From:     stopRef{StopID: from, Name: fromName},
		To:       stopRef{StopID: to, Name: toName},
		DayType:  dayType,
		FirstDay: first.Format(time.DateOnly),
		LastDay:  last.Format(time.DateOnly),
		Services: make([]serviceRow, 0, len(svcs)),
		Alerts:   make(map[string]alertText, len(alerts)),
	}
	for id, a := range alerts {
		resp.Alerts[id] = alertText{Header: a.Header, URL: a.URL}
	}
	for _, sv := range svcs {
		resp.Services = append(resp.Services, s.serviceRow(t, sv))
	}
	resp.Count = len(resp.Services)
	w.Header().Set("Cache-Control", servicesCache)
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) serviceRow(t timetable, sv store.Service) serviceRow {
	rt, _ := t.route(sv.RouteID) // a route TfNSW has since dropped keeps its id and loses its name
	origin, _ := t.stopName(sv.OriginStopID)
	row := serviceRow{
		FeedID:        sv.FeedID,
		RouteID:       sv.RouteID,
		ShortName:     rt.ShortName,
		Headsign:      sv.Headsign,
		Origin:        stopRef{StopID: sv.OriginStopID, Name: origin},
		OriginDeparts: wallClock(sv.OriginDepS),
		Departs:       wallClock(sv.DepartS),
		Arrives:       wallClock(sv.ArriveS),
		NDays:         len(sv.Days),
		Days:          make([]serviceDay, 0, len(sv.Days)),
	}
	if sv.DirectionID >= 0 {
		row.DirectionID = &sv.DirectionID
	}
	var delays []int32
	for _, d := range sv.Days {
		st := s.status(d.DelayS, d.TripRel)
		if d.StopTimeRel == stopTimeRelSkipped && st != "cancelled" {
			st = "skipped"
		}
		switch st {
		case "early":
			row.NEarly++
		case "on_time":
			row.NOnTime++
		case "late":
			row.NLate++
		case "very_late":
			row.NVeryLate++
		case "cancelled":
			row.NCancelled++
		case "skipped":
			row.NSkipped++
		default:
			row.NUnknown++
		}
		if d.DelayS != nil && st != "cancelled" && st != "skipped" {
			delays = append(delays, *d.DelayS)
		}
		if len(d.AlertIDs) > 0 {
			row.NAlertDays++
		}
		row.Days = append(row.Days, serviceDay{ServiceDate: d.ServiceDate.Format(time.DateOnly), DelayS: d.DelayS, Status: st, AlertIDs: d.AlertIDs})
	}
	if len(delays) > 0 {
		slices.Sort(delays)
		m := delays[(len(delays)+1)/2-1]
		row.TypicalDelayS = &m
	}
	return row
}
