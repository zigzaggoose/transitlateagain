package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Station is a place a rider boards: a parent station, or a stop that has
// none, in a feed's active timetable.
type Station struct {
	FeedID string
	StopID string
	Name   string
}

// Stations lists every station of every active timetable, by name.
func (s *Store) Stations(ctx context.Context) ([]Station, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.feed_id, s.stop_id, s.name
		FROM stops s JOIN schedule_versions v ON v.id = s.version_id AND v.active
		WHERE s.parent_station IS NULL AND s.location_type IN (0, 1)
		ORDER BY s.name, v.feed_id, s.stop_id`)
	if err != nil {
		return nil, fmt.Errorf("stations: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Station, error) {
		var st Station
		err := r.Scan(&st.FeedID, &st.StopID, &st.Name)
		return st, err
	})
	if err != nil {
		return nil, fmt.Errorf("stations: %w", err)
	}
	return out, nil
}

// ServiceQuery asks how the direct services from one station to another
// have run. The legs come from the in-memory timetable (match.Schedule.Legs).
type ServiceQuery struct {
	Legs              []Leg
	ToStops           []string  // every stop id of the destination station
	FirstDay, LastDay time.Time // service dates, inclusive
	Weekdays          []int32   // ISO days of the week to count, Monday 1 to Sunday 7
}

// Leg is one active-timetable trip's ride between the two stations.
type Leg struct {
	FeedID, TripID   string
	DepartS, ArriveS int32
}

// Service is one scheduled service (migrations/0007) that rides the legs,
// with what happened at the destination on each day it ran.
type Service struct {
	FeedID       string
	RouteID      string
	DirectionID  int16 // -1 when the timetable has none
	OriginStopID string
	OriginDepS   int32
	Headsign     string // "" when the timetable has none
	DepartS      int32  // scheduled at the origin of the leg, seconds into the service day
	ArriveS      int32  // scheduled at the destination
	Days         []ServiceDay
}

// ServiceDay is the service's last observation at the destination on one
// service date.
type ServiceDay struct {
	ServiceDate time.Time
	DelayS      *int32
	TripRel     int32
	StopTimeRel int32
}

// Services returns, in order of scheduled departure, every service among the
// legs' trips that has at least one visit at the destination in the query's
// dates. A service with none is not running on those days of the week, or
// has no history yet; either way there is nothing to report.
func (s *Store) Services(ctx context.Context, q ServiceQuery) ([]Service, error) {
	if len(q.Legs) == 0 {
		return nil, nil
	}
	feeds := make([]string, len(q.Legs))
	trips := make([]string, len(q.Legs))
	departs := make([]int32, len(q.Legs))
	arrives := make([]int32, len(q.Legs))
	for i, l := range q.Legs {
		feeds[i], trips[i], departs[i], arrives[i] = l.FeedID, l.TripID, l.DepartS, l.ArriveS
	}
	// services: the legs' trips by service key (trip_services), with the
	// earliest times any of its trips has and the headsign most of them
	// show: one short working to Ashfield must not rename the Penrith train.
	// The days then come from every trip id the service has ever had, not
	// just today's, which is the point of keying by service: TfNSW issues
	// trip ids per timetable. Each lookup is equalities on (feed_id,
	// trip_id, stop_id) and a range on service_date, stop_visits' key order.
	//
	// Searching the timetable for the legs in SQL as well took 335 ms for
	// Hornsby to Central (stop_times heap reads of every call at Central's
	// platforms, 48,668 rows) against a few in Go over memory it already has.
	rows, err := s.pool.Query(ctx, `
		WITH legs AS (
			SELECT * FROM unnest($1::text[], $2::text[], $3::int[], $4::int[]) AS l(feed_id, trip_id, depart_s, arrive_s)
		),
		services AS (
			SELECT ts.feed_id, ts.route_id, ts.direction_id, ts.origin_stop_id, ts.origin_dep_s,
			       coalesce(mode() WITHIN GROUP (ORDER BY ts.headsign), '') AS headsign, min(l.depart_s) AS depart_s, min(l.arrive_s) AS arrive_s
			FROM legs l JOIN trip_services ts ON ts.feed_id = l.feed_id AND ts.trip_id = l.trip_id
			GROUP BY 1, 2, 3, 4, 5
		)
		SELECT s.feed_id, s.route_id, s.direction_id, s.origin_stop_id, s.origin_dep_s, s.headsign,
		       s.depart_s, s.arrive_s, d.service_date, d.observed_delay_s, d.trip_rel, d.stop_time_rel
		FROM services s
		CROSS JOIN LATERAL (
			-- A service runs once a day; should two of its trip ids both
			-- report one day, the first is kept rather than counting it twice.
			SELECT DISTINCT ON (sv.service_date)
			       sv.service_date, sv.observed_delay_s, sv.trip_rel, sv.stop_time_rel
			FROM trip_services t
			JOIN stop_visits sv ON sv.feed_id = t.feed_id AND sv.trip_id = t.trip_id
			WHERE t.feed_id = s.feed_id AND t.route_id = s.route_id AND t.direction_id = s.direction_id
			  AND t.origin_stop_id = s.origin_stop_id AND t.origin_dep_s = s.origin_dep_s
			  AND sv.service_date BETWEEN $5 AND $6
			  AND extract(isodow FROM sv.service_date)::int = ANY($7)
			  AND sv.stop_id = ANY($8)
			ORDER BY sv.service_date, t.trip_id
		) d
		ORDER BY s.depart_s, s.feed_id, s.route_id, s.direction_id, s.origin_stop_id, s.origin_dep_s, d.service_date`,
		feeds, trips, departs, arrives, q.FirstDay, q.LastDay, q.Weekdays, q.ToStops)
	if err != nil {
		return nil, fmt.Errorf("services for %d legs: %w", len(q.Legs), err)
	}
	var out []Service
	var sv Service
	var day ServiceDay
	_, err = pgx.ForEachRow(rows, []any{
		&sv.FeedID, &sv.RouteID, &sv.DirectionID, &sv.OriginStopID, &sv.OriginDepS, &sv.Headsign,
		&sv.DepartS, &sv.ArriveS, &day.ServiceDate, &day.DelayS, &day.TripRel, &day.StopTimeRel,
	}, func() error {
		if n := len(out); n == 0 || !out[n-1].same(sv) {
			sv.Days = nil
			out = append(out, sv)
		}
		last := &out[len(out)-1]
		last.Days = append(last.Days, day)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("services for %d legs: %w", len(q.Legs), err)
	}
	return out, nil
}

func (a Service) same(b Service) bool {
	return a.FeedID == b.FeedID && a.RouteID == b.RouteID && a.DirectionID == b.DirectionID &&
		a.OriginStopID == b.OriginStopID && a.OriginDepS == b.OriginDepS
}
