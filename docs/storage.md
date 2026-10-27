# Storage

`PROJECT.md` §13's storage rows, measured on the VM on **2026-10-27 at 22:20**
(BinaryLane 1 GB / 20 GB, five feeds, `RETENTION_DAYS=7`), by
`deploy/release.sh` straight after deploying `08c5f46`. Row counts are
Postgres's estimates (`pg_class.reltuples`), not `count(*)`.

## Raw observations

The seven complete service days in the retention window:

| Partition | Size | Rows | |
|---|---:|---:|---|
| `observations_2026_10_20` | 521 MB | 1,953,022 | Tue |
| `observations_2026_10_21` | 535 MB | 2,004,186 | Wed |
| `observations_2026_10_22` | 491 MB | 1,820,121 | Thu |
| `observations_2026_10_23` | 552 MB | 2,026,411 | Fri |
| `observations_2026_10_24` | 416 MB | 1,430,414 | Sat |
| `observations_2026_10_25` | 448 MB | 1,629,976 | Sun |
| `observations_2026_10_26` | 715 MB | 2,564,691 | Mon, the first T1 North Shore trackwork night |
| **Seven days** | **3,678 MB** | **13,428,821** | |

- **~287 bytes a row**, heap and indexes. The laptop's one evening in September
  gave 266–277; the difference is index growth at full size.
- **~525 MB a day** on average: ~563 MB a weekday, ~432 MB a weekend day. The
  September projection of ~270 MB a day was trains alone, off-peak.
- **~3.7 GB** at `RETENTION_DAYS=7`.

## Rollups

| Table | Size | Rows |
|---|---:|---:|
| `otp_stop_hourly` | 241 MB | 1,206,727 |
| `otp_route_hourly` | 10 MB | 54,559 |

251 MB for the ~33 days since the first rollup on the VM (2026-09-24), so
**~7.6 MB a day**. That is an average over the whole history, not one day's
delta measured on its own. Against ~525 MB of raw rows a day, the rollups keep
**~98.6 %** less (`1 − 7.6 / 525`).

## Per-service history (migrations/0007)

| Table | Size | Rows |
|---|---:|---:|
| `stop_visits` | 108 MB | 552,942 (backfilled, 8 days) |
| `trip_services` | 31 MB | 157,145 |

- **~205 bytes a visit**, against 209 measured on 1.7M synthetic rows.
- ~77,000 visits a weekday and ~54,000 a weekend day, all five feeds: what the
  feeds reported, below the 96,542 the timetable schedules on a weekday.
- So **~15 MB a weekday, ~95 MB a week, ~2.4 GB** at `VISIT_RETENTION_DAYS=180`.
- `trip_services` growth is not measured yet: 157,145 rows on the first day.

## Whole database and disk

- `pg_database_size`: **6,062 MB**, under the §13 target of 10 GB. By
  subtraction, ~1.6 GB of it is none of the tables above (the seven days,
  today's partial 410 MB, the rollups and the two new tables): most likely the
  stored timetable versions, 212 MB per trains version when measured in
  September. Not measured directly.
- Disk: **16 GB of 20 GB used, 2.8 GB free (86 %)**. The database is 6 GB of
  that 16 GB. `docker system df` the same evening: build cache 3.63 GB (3.58
  reclaimable, 81 entries from a month of `up --build`), images 1.48 GB
  (117 MB reclaimable), the database volume 6.65 GB; the OS and the 2 GB
  swapfile make up the rest. `deploy/release.sh` now prunes build cache older
  than a week after each deploy. That is already past the 15 % free of
  `TransitLateAgainDiskLow` (§10.4), and `stop_visits` growing by ~2.4 GB over
  its first 180 days would nearly fill what is left. See §16 q13.

## Re-measuring

`sh deploy/release.sh` prints all of the above after every deploy. On its own,
on the VM:

```sql
SELECT c.relname, pg_size_pretty(pg_total_relation_size(c.oid)), c.reltuples::bigint
FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
WHERE i.inhparent IN ('observations'::regclass, 'stop_visits'::regclass) ORDER BY 1;
SELECT pg_size_pretty(pg_database_size(current_database()));
```
