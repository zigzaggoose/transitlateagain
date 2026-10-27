package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/zigzaggoose/transitlateagain/internal/gtfsrt"
)

// UpsertAlerts records one alerts poll. An alert seen before keeps its
// first_seen_ts and takes the rest from this poll, which is the producer's
// latest word; feed_ts is the header's, so a replayed body changes nothing.
func (s *Store) UpsertAlerts(ctx context.Context, f gtfsrt.AlertFeed) error {
	b := &pgx.Batch{}
	for _, a := range f.Alerts {
		active := pgtype.Multirange[pgtype.Range[pgtype.Timestamptz]]{}
		for _, p := range a.Periods {
			active = append(active, bounds(p))
		}
		if len(active) == 0 {
			active = append(active, bounds(gtfsrt.Period{})) // always, per the specification
		}
		b.Queue(`
			INSERT INTO service_alerts (alert_id, first_seen_ts, last_seen_ts, cause, effect, header, description, url, active, route_ids, stop_ids, trip_ids)
			VALUES ($1, $2, $2, $3, $4, $5, nullif($6, ''), nullif($7, ''), $8::tstzmultirange, $9, $10, $11)
			ON CONFLICT (alert_id) DO UPDATE SET
				last_seen_ts = greatest(service_alerts.last_seen_ts, EXCLUDED.last_seen_ts),
				cause = EXCLUDED.cause, effect = EXCLUDED.effect, header = EXCLUDED.header,
				description = EXCLUDED.description, url = EXCLUDED.url, active = EXCLUDED.active,
				route_ids = EXCLUDED.route_ids, stop_ids = EXCLUDED.stop_ids, trip_ids = EXCLUDED.trip_ids
			WHERE EXCLUDED.last_seen_ts >= service_alerts.last_seen_ts`,
			a.ID, f.HeaderTS, int16(a.Cause), int16(a.Effect), a.Header, a.Description, a.URL, active,
			nonNil(a.RouteIDs), nonNil(a.StopIDs), nonNil(a.TripIDs))
	}
	if err := s.pool.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("upsert %d alerts (feed_ts %s): %w", len(f.Alerts), f.HeaderTS.Format(time.RFC3339), err)
	}
	return nil
}

func bounds(p gtfsrt.Period) pgtype.Range[pgtype.Timestamptz] {
	r := pgtype.Range[pgtype.Timestamptz]{LowerType: pgtype.Unbounded, UpperType: pgtype.Unbounded, Valid: true}
	if p.Start != nil {
		r.Lower, r.LowerType = pgtype.Timestamptz{Time: *p.Start, Valid: true}, pgtype.Inclusive
	}
	if p.End != nil {
		r.Upper, r.UpperType = pgtype.Timestamptz{Time: *p.End, Valid: true}, pgtype.Exclusive
	}
	return r
}

// nonNil keeps an empty list an empty array: the columns are NOT NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
