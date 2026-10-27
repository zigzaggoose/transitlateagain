-- TfNSW's published service alerts (§15 2026-10-27), so a service page can
-- say a late day had a published disruption. The feed carries only what is
-- current, so each alert is kept as last seen, keyed by its entity id.
--
-- cause and effect are raw GTFS-realtime integers, as the schedule
-- relationships are (§15 2026-09-21). active is the alert's active periods;
-- an alert with none is always active, '{(,)}'.
CREATE TABLE service_alerts (
    alert_id      text            PRIMARY KEY,
    first_seen_ts timestamptz     NOT NULL,   -- feed header timestamps, never our clock
    last_seen_ts  timestamptz     NOT NULL,
    cause         smallint        NOT NULL,
    effect        smallint        NOT NULL,
    header        text            NOT NULL,
    description   text,
    url           text,
    active        tstzmultirange  NOT NULL,
    route_ids     text[]          NOT NULL,
    stop_ids      text[]          NOT NULL,
    trip_ids      text[]          NOT NULL
);

-- The service pages ask "alerts on these routes or trips during these days".
CREATE INDEX service_alerts_active ON service_alerts USING gist (active);
