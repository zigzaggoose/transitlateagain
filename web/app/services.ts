import type { Service, Station } from "./api";

// Below this many days a grade is noise: one bad morning would make it an F.
export const MIN_DAYS = 5;

// What a rider at the destination counts as a failure: arriving more than
// 5 minutes late, or the train not coming. Arriving early is fine at the end
// of a ride, so early counts with on time; days the feed never reported are
// left out rather than guessed.
export function record(s: Service): { good: number; bad: number } {
  return {
    good: s.n_on_time + s.n_early,
    bad: s.n_late + s.n_very_late + s.n_cancelled + s.n_skipped,
  };
}

export type Grade = "A" | "B" | "C" | "D" | "F";

// grade is the share of reported days the service got the rider there on
// time; null until there are MIN_DAYS of them.
export function grade(s: Service): Grade | null {
  const { good, bad } = record(s);
  if (good + bad < MIN_DAYS) return null;
  const share = good / (good + bad);
  return share >= 0.95 ? "A" : share >= 0.85 ? "B" : share >= 0.7 ? "C" : share >= 0.5 ? "D" : "F";
}

// typical says the median delay in words: within a minute is on time.
export function typical(delayS: number | null): string {
  if (delayS === null) return "";
  const m = Math.round(Math.abs(delayS) / 60);
  if (m < 1) return "usually on time";
  return `usually ${m} min ${delayS > 0 ? "late" : "early"}`;
}

// verdict is the one line a rider reads: "Late 9 of the last 20 weekdays".
export function verdict(s: Service, days: string): string {
  const { good, bad } = record(s);
  const n = good + bad;
  if (n < MIN_DAYS) return `Only ${n} ${n === 1 ? days.replace(/s$/, "") : days} recorded so far`;
  if (bad === 0) return `On time every one of the last ${n} ${days}`;
  const cancelled = s.n_cancelled + s.n_skipped;
  const late = bad - cancelled;
  const parts = [late && `late ${late}`, cancelled && `cancelled ${cancelled}`].filter(Boolean).join(", ");
  return `${parts[0].toUpperCase()}${parts.slice(1)} of the last ${n} ${days}`;
}

// stationOptions labels each station once, by name, adding the mode only
// where two different stations share a name. The same stop id appears once
// per feed that serves it (Central is in trains and metro), and is one place.
export function stationOptions(stations: Station[], modes: Record<string, string>): { label: string; stop_id: string }[] {
  const byID = new Map<string, Station>();
  for (const s of stations) if (!byID.has(s.stop_id)) byID.set(s.stop_id, s);
  const named = new Map<string, number>();
  for (const s of byID.values()) named.set(s.name, (named.get(s.name) ?? 0) + 1);
  return [...byID.values()]
    .map((s) => ({ label: named.get(s.name)! > 1 ? `${s.name} (${modes[s.feed_id] ?? s.feed_id})` : s.name, stop_id: s.stop_id }))
    .sort((a, b) => a.label.localeCompare(b.label));
}
