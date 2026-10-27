-- One-off, after deploying migrations/0007: write stop_visits for every
-- visit the hourly rollup closed before it existed, by the rollup's own rule
-- (internal/rollup): the last observation of each visit, bucketed by
-- coalesce(scheduled_at, feed_ts). Safe beside the running service: the
-- rollup only writes hours past the watermark, this only hours before it, and
-- ON CONFLICT DO NOTHING makes a second run a no-op.
--
-- On the VM, from /opt/transitlateagain:
--   $C exec -T postgres psql -U headway -v ON_ERROR_STOP=1 < deploy/backfill-stop-visits.sql
-- One partition per statement, so each is one partition's sort, not all of them.

DO $$
DECLARE p regclass;
DECLARE n bigint;
BEGIN
    FOR p IN
        SELECT c.oid::regclass FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
        WHERE i.inhparent = 'observations'::regclass ORDER BY c.relname
    LOOP
        EXECUTE format($q$
            INSERT INTO stop_visits (service_date, feed_id, trip_id, stop_id, scheduled_at, observed_delay_s, trip_rel, stop_time_rel)
            SELECT service_date, feed_id, trip_id, stop_id, scheduled_at, observed_delay_s, trip_rel, stop_time_rel
            FROM (
                SELECT DISTINCT ON (service_date, feed_id, trip_id, stop_id)
                       service_date, feed_id, trip_id, stop_id, scheduled_at, observed_delay_s,
                       trip_rel, stop_time_rel, coalesce(scheduled_at, feed_ts) AS bucket_ts
                FROM %s
                ORDER BY service_date, feed_id, trip_id, stop_id, feed_ts DESC
            ) last_word
            WHERE bucket_ts < (SELECT watermark FROM rollup_state WHERE name = 'hourly')
            ON CONFLICT DO NOTHING$q$, p);
        GET DIAGNOSTICS n = ROW_COUNT;
        RAISE NOTICE '% : % visits', p, n;
    END LOOP;
END $$;
