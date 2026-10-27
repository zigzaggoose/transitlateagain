-- Per-service history (§6.2, §15 2026-10-27). The hourly rollups answer "how
-- did this stop do between 07:00 and 08:00" but not "how does the 07:42 from
-- Hornsby do", and the raw rows that could answer it are dropped after
-- RETENTION_DAYS. Two tables close the gap.

-- What a trip id means, kept after its timetable version is pruned. A
-- service is the train a commuter means by "the 07:42": route, direction,
-- origin station and scheduled origin departure. Trip ids are not that: one
-- weekday service carries a different trip id per calendar pattern, and 8 %
-- of train trip ids change from one day's bundle to the next, while 99.9 %
-- of service keys survive it (measured 2026-10-27, versions of 2026-09-23 and
-- 2026-09-24). No trip id shared by those versions changed its key (0 of
-- 61,246), so the first version to name a trip id is trusted for good.
--
-- origin_stop_id is the parent station where the timetable has one: one
-- service can leave from different platforms on different days, which split
-- 11,544 services into 12,008 when keyed by platform. origin_dep_s is
-- rounded to the nearest minute, the clock a rider reads: TfNSW times some
-- trips a second off the minute (NSN_2k from Hornsby at 21:42:00 and at
-- 21:42:01, the same train), and no route runs two trains one way from one
-- station inside a minute.
CREATE TABLE trip_services (
    feed_id          text     NOT NULL,
    trip_id          text     NOT NULL,
    route_id         text     NOT NULL,
    direction_id     smallint NOT NULL DEFAULT -1,   -- -1 means unknown, as in otp_*_hourly
    origin_stop_id   text     NOT NULL,
    origin_dep_s     integer  NOT NULL,              -- seconds from the start of the service day, to the minute
    headsign         text,
    -- No foreign key: the row must outlive the version that introduced it.
    first_version_id bigint   NOT NULL,
    PRIMARY KEY (feed_id, trip_id)
);

-- "Every trip id this service has ever had."
CREATE INDEX trip_services_by_service
    ON trip_services (feed_id, route_id, direction_id, origin_stop_id, origin_dep_s);

-- Newest version first, so a trip id in several stored versions takes the key
-- the loader would give it today. The first call with a time is the origin:
-- GTFS requires times there, and a row without one cannot be keyed.
INSERT INTO trip_services (feed_id, trip_id, route_id, direction_id, origin_stop_id, origin_dep_s, headsign, first_version_id)
SELECT DISTINCT ON (v.feed_id, st.trip_id)
       v.feed_id, st.trip_id, t.route_id, coalesce(t.direction_id, -1),
       coalesce(s.parent_station, st.stop_id), (coalesce(st.departure_s, st.arrival_s) + 30) / 60 * 60,
       t.headsign, v.id
FROM stop_times st
JOIN schedule_versions v ON v.id = st.version_id
JOIN trips t ON t.version_id = st.version_id AND t.trip_id = st.trip_id
JOIN stops s ON s.version_id = st.version_id AND s.stop_id = st.stop_id
WHERE coalesce(st.departure_s, st.arrival_s) IS NOT NULL
ORDER BY v.feed_id, st.trip_id, v.id DESC, st.stop_sequence;

-- One row per stop visit: the last thing the feed said about it, which is
-- what the rollup counts. Written by the rollup for each hour it closes, so
-- the hourly tables can be rebuilt from it after a bug fix rather than being
-- wrong for good. route_id and direction_id are not repeated here: they are
-- the trip's, in trip_services.
--
-- Monthly partitions, not daily like observations: rows are ~25x fewer per
-- day and kept ~25x longer, and a year is 12 tables instead of 365.
-- Retention is DROP TABLE of a whole month (§9.3).
CREATE TABLE stop_visits (
    service_date     date        NOT NULL,
    feed_id          text        NOT NULL,
    trip_id          text        NOT NULL,
    stop_id          text        NOT NULL,
    scheduled_at     timestamptz,            -- NULL when unmatched
    observed_delay_s integer,
    trip_rel         smallint    NOT NULL,
    stop_time_rel    smallint    NOT NULL,
    -- Trip first: the service pages read "these trip ids at these stops, on
    -- these dates", equalities then a range. Date first would scan every
    -- visit of every trip in the range; date before stop read 400 entries a
    -- trip to keep 20, and Strathfield to Central took 484 ms against 325 ms
    -- (2M visits, measured 2026-10-27). service_date must be in the key;
    -- where is free.
    PRIMARY KEY (feed_id, trip_id, stop_id, service_date)
) PARTITION BY RANGE (service_date);

CREATE TABLE stop_visits_default PARTITION OF stop_visits DEFAULT;

-- The months the backfill below writes into, plus the current and next one,
-- so it lands in real partitions rather than the default.
DO $$
DECLARE m date;
BEGIN
    FOR m IN
        SELECT generate_series(
            date_trunc('month', least(coalesce((SELECT min(service_date) FROM observations), current_date), current_date)),
            date_trunc('month', current_date) + interval '1 month',
            interval '1 month')::date
    LOOP
        EXECUTE format('CREATE TABLE %I PARTITION OF stop_visits FOR VALUES FROM (%L) TO (%L)',
                       'stop_visits_' || to_char(m, 'YYYY_MM'), m, (m + interval '1 month')::date);
    END LOOP;
END $$;

-- The visits the rollup closed before this migration are backfilled by
-- deploy/backfill-stop-visits.sql, run by hand beside the live service, not
-- here: migrations run before the pollers start, and a DISTINCT ON over every
-- raw row (millions on the VM) would stop ingest for minutes, losing realtime
-- data nobody archives.
