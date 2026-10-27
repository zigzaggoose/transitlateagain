# HANDOFF.md

Session state as of **2026-10-27, evening**. This session's work is three commits on
`main` after `5af5f5b`, one per step below, **not yet pushed or deployed**. Rewrite this
file when it stops being true. Permanent rules live in `CLAUDE.md`; design in
`PROJECT.md` (§12 milestones, §15 decisions, §16 open questions). Every
deployment step ever run is in `deploy/vm-bootstrap.md`.

## This session: service report cards (committed, not deployed)

Five steps the user asked for, from a review of how to make the site worth
visiting. §15 has a row for each, dated 2026-10-27.

1. **Storage over a full day** — *not done*: reads on the VM were not
   permitted from this session. Run the queries in "Due next" below.
2. **Trip id stability** — measured on the two stored train bundles: 92 % of
   trip ids carry over a day, 99.9 % of service keys do. Hence (3).
3. `migrations/0007`: `trip_services` (trip id → service key, filled by the
   loader) and `stop_visits` (each visit's final word, written by the rollup,
   monthly partitions, `VISIT_RETENTION_DAYS=180`).
4. `GET /v1/stations`, `GET /v1/services`, and the `/services` page: a grade
   and a square a day for every direct train between two stations.
5. `migrations/0008` + an alerts poller (`ALERTS_URL`, 5 min): TfNSW's service
   alerts, shown on the days they covered.

All checks green on 2026-10-27: `make lint`, `make test`, `make
test-integration` (0 skips), `make cross`, `npm run build`, and
`node --experimental-strip-types --test app/*.test.ts` in `web/` (Node 22
needs the flag). Exercised end to end locally against a copy of the dev
database with 1.7M synthetic visits; the dev database itself was not touched.

**Deploying it** (after `git push`): `git pull && $C up -d --build` on the VM,
as usual. Startup applies 0007 and 0008 before the pollers start; 0007 fills
`trip_services` from the stored timetables, ~9 s locally with nine versions,
so expect a pause in ingest of that order. Then, beside the running service:

```sh
$C exec -T postgres psql -U headway -v ON_ERROR_STOP=1 < deploy/backfill-stop-visits.sql
```

which gives the service pages the last `RETENTION_DAYS` of history at once.

**`deploy/Untitled-1.yaml` holds a live Grafana token** and is untracked but
not ignored. Delete it; rotate the token if it went anywhere else.

## Start here

```sh
open -a Docker && docker start headway-dev-pg        # the daemon does not survive a reboot
make lint && make test && make test-integration && make cross
curl -s https://api.transitlateagain.dev/readyz        # production, from outside
ssh root@119.42.55.16 'curl -s localhost:8081/v1/admin/stats'   # production, from inside
```

**Check exit codes, never piped output** (`657d87b` shipped a failing test that
way). zsh does not word-split `$var`; a heredoc whose body contains its own
terminator line ends early; BSD `sed` has no `\b` (use `perl -pi -e`).

## Where the project is

| Stage | State |
|---|---|
| 1 — MVP | **Complete.** Live since 2026-09-24 11:53. |
| 2 — Matching, history, CI | **Complete.** |
| 3 — All modes, scale, load test | Complete except **storage over a full day** (due 2026-09-26, below). |
| 4 — Observability, front end | **Complete.** |

The project was renamed from **Headway** to **Transit Late Again** on
2026-09-24 (`b7b1059`). The old name survives on purpose in: the Postgres role
and database (`headway`), the Compose volume (`headway_pgdata`), the VM's
hostname, and every §15 row written before the rename.

## What runs where

| | Address | Notes |
|---|---|---|
| Site | https://transitlateagain.dev | `web/`, Next.js 16 static export on **Cloudflare Pages** (project `transitlateagain`: root `web`, `npm run build`, output `out`, `NODE_VERSION=22`). Rebuilt on every push to `main`. |
| API | https://api.transitlateagain.dev | The VM behind Cloudflare in Full (strict). CORS allows exactly `https://transitlateagain.dev`. 10 req/s per visitor, keyed on `CF-Connecting-IP`. |
| VM | `ssh root@119.42.55.16` | BinaryLane Standard 1 GB, Sydney, Ubuntu 24.04, no backups. Key-only SSH (`~/.ssh/id_ed25519`); the emailed root password works only in BinaryLane's web console. Only SSH is open to the world; 443 is open to Cloudflare's ranges only. |
| Admin, `/metrics` | `localhost:8081` on the VM | Never public. Alloy scrapes it over the Compose network. |
| Metrics | Grafana Cloud stack, region `prod-au-southeast-1` | Dashboard imported from `deploy/grafana-dashboard.json`; alert `TransitLateAgainIngestStopped` emails the account owner; runbook `docs/runbook.md`. |
| DNS, TLS | Cloudflare (nameservers set at Namecheap) | Records: `api` A → 119.42.55.16 (proxied); the root is a Pages custom domain; MX/TXT are Namecheap email forwarding. Origin cert in `/opt/transitlateagain-tls` on the VM, expires 2041-09-20; its key never left the VM. |

**Every Compose command on the VM needs both files**, from `/opt/transitlateagain`:

```sh
C="docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.vm.yml"
git pull && $C up -d --build        # deploy
```

Memory with all of it running: service ~230 MB, Postgres ~142 MB, Alloy
~79 MB, ~230 MB available, 2 GB swap barely used.

## Secrets: where they are, never in git

| Secret | Local `.env` (gitignored, 0600) | VM `/opt/transitlateagain/.env` (0600) |
|---|---|---|
| `TFNSW_API_KEY` | yes | yes |
| `POSTGRES_PASSWORD` | — (dev DB is `headway`/`headway`) | yes, generated on the VM |
| `GRAFANA_REMOTE_WRITE_URL`, `GRAFANA_USERNAME`, `GRAFANA_TOKEN` | yes, since 2026-09-24 | yes, read by Alloy |
| Origin TLS key | — | `/opt/transitlateagain-tls/origin.key`, owner 65532, 0400 |

The Grafana token belongs to the access policy `transitlateagain-alloy-write`,
scope **metrics:write** only, so it cannot query (and a read-only token answers
remote write with `401 invalid scope requested`). Test it without printing it:

```sh
set -a; . ./.env; set +a
curl -s -o /dev/null -w '%{http_code}\n' -u "$GRAFANA_USERNAME:$GRAFANA_TOKEN" -X POST "$GRAFANA_REMOTE_WRITE_URL"   # 400 = authenticated
```

**Pasted into session transcripts, so rotate if one is ever shared:** the TfNSW
API key (an early session) and the Grafana write token (this one). Rotating
either means editing both `.env` files, then `$C up -d` on the VM.

## Due next

1. **Storage over a full day, the last unticked box in the plan**, now a
   month overdue and also what sizes `VISIT_RETENTION_DAYS` (§16 q13). On the
   VM (`cd /opt/transitlateagain && $C exec postgres psql -U headway`):
   ```sql
   SELECT c.relname, pg_size_pretty(pg_total_relation_size(c.oid)), c.reltuples::bigint
   FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
   WHERE i.inhparent = 'observations'::regclass ORDER BY 1;
   SELECT pg_size_pretty(pg_total_relation_size('otp_stop_hourly') + pg_total_relation_size('otp_route_hourly'));
   SELECT pg_size_pretty(pg_database_size(current_database()));
   ```
   Fill in `docs/storage.md` and the two "baseline TBD" rows of §13, and tick
   the Stage 3 box. After a month of the new tables, measure `stop_visits`,
   `trip_services` and `service_alerts` too (§16 q13, q14).
2. **Ride-check the numbers**: take two or three trains you know, and compare
   what `/services` says about those days with what happened.
3. **§13 freshness p50** is unmeasured, and the p95 (17.7 s) is a six-minute
   first window. The dashboard's freshness panel over a full day gives both.
4. **The `DOCKER-USER` allowlist** in `deploy/firewall.sh` is Cloudflare's IPv4
   ranges as of 2026-09-24. If Cloudflare changes them
   (https://www.cloudflare.com/ips-v4), update the list and rerun the script.

## Found and fixed on 2026-09-24

- **§9.1 case 18:** the producers keep finished trips and served stops, so
  `/now` called 21 % of trips active with a next stop hours past. The cache
  drops a stop 5 minutes after its prediction, against the feed timestamp.
- **Schedule refreshes failed after the first load:** TfNSW answers an
  unchanged `If-Modified-Since` with 502, not 304. Downloads are unconditional.
- **Grafana 12.2+ calls classic dashboard JSON "old format"**; the dashboard
  file is a V1 resource (`dashboard.grafana.app/v1`).
- **Adding a site to Cloudflare imports the registrar's parking records**; the
  first symptom was a 522 with nothing reaching the VM.

## Things that are true and easy to forget

- **Trains never send `stop_sequence`, `start_date`, `direction_id` or a vehicle
  id**, and 275 of 354 train updates carry delays only. So trains match by
  stop name (order 2) and the other feeds by sequence (order 1).
- **Schedule relationships stay raw integers.** `REPLACEMENT` (5) is ~15–18 % of
  train updates.
- **Parramatta light rail published an empty feed** (a 15-byte header) on the
  morning of 2026-09-24 and was back by ~14:30. Nothing on our side.
- **"Empty Train" is a real TfNSW headsign** for out-of-service runs.
- **The dev database's `public` schema has real data.** Catalogue tests scope
  to their own schema; load tests go in a throwaway database.
- **`web/AGENTS.md` and `web/CLAUDE.md` are generated by `next dev`** and say to
  read `web/node_modules/next/dist/docs/` before writing Next.js code: version
  16 differs from older training data.
- **Nothing is left running on the laptop** except the dev Postgres container.
