"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { API, get, type DayType, type Service, type Services, type Station } from "../api";
import { MIN_DAYS, grade, stationOptions, typical, verdict } from "../services";

const MODES: Record<string, string> = {
  sydneytrains: "Trains",
  metro: "Metro",
  sydneyferries: "Ferries",
  "lightrail-cbdandsoutheast": "Light rail",
  "lightrail-parramatta": "Light rail",
};

const DAY_TYPES: [DayType, string, string][] = [
  ["weekday", "Weekdays", "weekdays"],
  ["saturday", "Saturdays", "Saturdays"],
  ["sunday", "Sundays", "Sundays"],
];

const PAGE = 12;

// minutes reads "HH:MM" as minutes into the evening's service day: TfNSW runs
// a night's last trains past midnight, so before 03:00 sorts after 23:59.
function minutes(hhmm: string): number {
  const [h, m] = hhmm.split(":").map(Number);
  return (h < 3 ? h + 24 : h) * 60 + m;
}

const dayLabel = (iso: string) =>
  new Date(iso + "T12:00:00+10:00").toLocaleDateString("en-AU", { weekday: "short", day: "numeric", month: "short" });

function dayText(d: Service["days"][number]): string {
  switch (d.status) {
    case "cancelled":
      return "cancelled";
    case "skipped":
      return "did not stop";
    case "unknown":
      return "not reported";
  }
  const m = Math.round((d.delay_s ?? 0) / 60);
  return m === 0 ? "on time" : m > 0 ? `${m} min late` : `${-m} min early`;
}

// The URL fragment holds the choice, so a commute can be bookmarked or shared.
function readHash(): { from: string; to: string; day: DayType } {
  const p = new URLSearchParams(location.hash.slice(1));
  const day = p.get("day") as DayType;
  return { from: p.get("from") ?? "", to: p.get("to") ?? "", day: DAY_TYPES.some(([d]) => d === day) ? day : "weekday" };
}

export default function ServicesPage() {
  const [stations, setStations] = useState<Station[]>([]);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [fromText, setFromText] = useState("");
  const [toText, setToText] = useState("");
  const [day, setDay] = useState<DayType>("weekday");
  const [after, setAfter] = useState("");
  const [shown, setShown] = useState(PAGE);
  const [result, setResult] = useState<Services | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const options = useMemo(() => stationOptions(stations, MODES), [stations]);
  const label = (id: string) => options.find((o) => o.stop_id === id)?.label ?? "";

  useEffect(() => {
    get<{ stations: Station[] }>("/v1/stations")
      .then((r) => {
        setStations(r.stations);
        const h = readHash();
        const opts = stationOptions(r.stations, MODES);
        const name = (id: string) => opts.find((o) => o.stop_id === id)?.label ?? "";
        setFrom(h.from);
        setTo(h.to);
        setFromText(name(h.from));
        setToText(name(h.to));
        setDay(h.day);
      })
      .catch((e: Error) => setError(e.message));
    // A quarter of an hour back, so the train just missed is still on screen.
    const t = new Date(Date.now() - 15 * 60e3);
    setAfter(`${String(t.getHours()).padStart(2, "0")}:${String(t.getMinutes()).padStart(2, "0")}`);
  }, []);

  useEffect(() => {
    if (!from || !to || from === to) {
      setResult(null);
      return;
    }
    location.hash = new URLSearchParams({ from, to, day }).toString();
    const ctl = new AbortController();
    setLoading(true);
    get<Services>(`/v1/services?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&day_type=${day}`, ctl.signal)
      .then((r) => {
        setResult(r);
        setError("");
      })
      .catch((e: Error) => e.name !== "AbortError" && setError(e.message))
      .finally(() => setLoading(false));
    return () => ctl.abort();
  }, [from, to, day]);

  useEffect(() => setShown(PAGE), [from, to, day, after]);

  // A typed or picked name becomes a station once it matches one exactly.
  const pickText = (text: string, setText: (s: string) => void, set: (id: string) => void) => {
    setText(text);
    const o = options.find((o) => o.label.toLowerCase() === text.trim().toLowerCase());
    set(o ? o.stop_id : "");
  };
  const swap = () => {
    setFrom(to);
    setTo(from);
    setFromText(label(to));
    setToText(label(from));
  };

  const plural = DAY_TYPES.find(([d]) => d === day)![2];
  // No short name is TfNSW's RTTA_* empty-train runs, as on the live page.
  const later = result
    ? result.services.filter((s) => s.short_name && (!after || minutes(s.departs) >= minutes(after)))
    : [];

  return (
    <main>
      <div className="picker">
        <header>
          <p className="small">
            <Link href="/">← Live running</Link>
          </p>
          <h1>Is my train usually late?</h1>
          <p className="muted">
            Pick where you get on and off. Every train between them gets a report card from the last four weeks.
          </p>
        </header>

        <datalist id="stations">
          {options.map((o) => (
            <option key={o.stop_id} value={o.label} />
          ))}
        </datalist>
        <label htmlFor="from">From</label>
        <input
          id="from"
          list="stations"
          autoComplete="off"
          placeholder="e.g. Hornsby Station"
          value={fromText}
          onChange={(e) => pickText(e.target.value, setFromText, setFrom)}
        />
        <label htmlFor="to">To</label>
        <input
          id="to"
          list="stations"
          autoComplete="off"
          placeholder="e.g. Central Station"
          value={toText}
          onChange={(e) => pickText(e.target.value, setToText, setTo)}
        />
        <div className="controls">
          <button onClick={swap} disabled={!from && !to}>
            ⇄ Swap
          </button>
          {DAY_TYPES.map(([d, name]) => (
            <button key={d} aria-pressed={day === d} onClick={() => setDay(d)}>
              {name}
            </button>
          ))}
          <label className="inline">
            Leaving after{" "}
            <input type="time" value={after} onChange={(e) => setAfter(e.target.value)} />
          </label>
        </div>
      </div>

      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}

      {from && to && from === to && <p className="muted">From and to are the same station.</p>}
      {loading && !result && <p className="muted">Loading…</p>}

      {result && (
        <section aria-labelledby="cards">
          <h2 id="cards">
            {label(from)} to {label(to)} <span className="muted">· {plural}</span>
          </h2>
          {result.services.length === 0 ? (
            <p className="muted">
              No direct service between these with any history on {plural}. Services that need a change are not
              covered.
            </p>
          ) : later.length === 0 ? (
            <p className="muted">Nothing leaves after {after}. Pick an earlier time.</p>
          ) : (
            <>
              <ul className="services">
                {later.slice(0, shown).map((s) => (
                  <ServiceCard key={serviceKey(s)} s={s} from={from} days={plural} />
                ))}
              </ul>
              {later.length > shown && (
                <div className="controls">
                  <button onClick={() => setShown(shown + PAGE)}>Show later trains</button>
                </div>
              )}
            </>
          )}
        </section>
      )}

      <footer className="muted small">
        <p>
          <b>On time</b> means arriving no more than 5 minutes late. Arriving early counts as on time here; a
          cancelled train, or one that did not stop, counts against it. Grades need at least {MIN_DAYS} days: A is on
          time 95 % of days or more, B 85 %, C 70 %, D 50 %, F less. These are Transit Late Again&apos;s own measures,
          from what TfNSW&apos;s realtime feed said, not TfNSW&apos;s official figures.
        </p>
        <p>
          Data: Transport for NSW Open Data. API:{" "}
          <a href={`${API}/v1/stations`}>{API.replace(/^https?:\/\//, "")}</a> ·{" "}
          <a href="https://github.com/zigzaggoose/transitlateagain">source</a>
        </p>
      </footer>
    </main>
  );
}

// serviceKey is the API's service key (migrations/0007): what makes two
// cards different trains.
const serviceKey = (s: Service) => [s.feed_id, s.route_id, s.direction_id, s.origin.stop_id, s.origin_departs].join(" ");

function ServiceCard({ s, from, days }: { s: Service; from: string; days: string }) {
  const g = grade(s);
  const via = s.origin.stop_id !== from && s.origin.name ? ` from ${s.origin.name}` : "";
  return (
    <li className="service">
      <div className={`grade ${g ? "g-" + g : "none"}`} aria-label={g ? `Grade ${g}` : "Not graded yet"}>
        {g ?? "–"}
      </div>
      <div>
        <p className="when">
          <b>{s.departs}</b> → {s.arrives}
          <span className="muted">
            {" "}
            · {s.short_name}
            {s.headsign ? ` to ${s.headsign}` : ""}
            {via}
          </span>
        </p>
        <p>
          {verdict(s, days)}
          {s.typical_delay_s !== null && `, ${typical(s.typical_delay_s)}`}.
        </p>
        <ol className="days" aria-label="Each day, oldest first">
          {s.days.map((d) => (
            <li key={d.service_date} className={d.status} title={`${dayLabel(d.service_date)}: ${dayText(d)}`}>
              <span className="sr">
                {dayLabel(d.service_date)}: {dayText(d)}
              </span>
            </li>
          ))}
        </ol>
      </div>
    </li>
  );
}
