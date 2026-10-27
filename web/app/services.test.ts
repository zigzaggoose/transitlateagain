// node --test app/services.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import type { Service } from "./api.ts";
import { grade, stationOptions, typical, verdict } from "./services.ts";

const service = (n: Partial<Service>): Service => ({
  feed_id: "sydneytrains",
  route_id: "R",
  direction_id: 0,
  origin: { stop_id: "O" },
  origin_departs: "07:30",
  departs: "07:42",
  arrives: "08:21",
  n_days: 0,
  n_early: 0,
  n_on_time: 0,
  n_late: 0,
  n_very_late: 0,
  n_cancelled: 0,
  n_skipped: 0,
  n_unknown: 0,
  n_alert_days: 0,
  typical_delay_s: null,
  days: [],
  ...n,
});

test("no grade below five reported days, and unreported days do not count", () => {
  assert.equal(grade(service({ n_on_time: 4, n_unknown: 10 })), null);
  assert.equal(grade(service({ n_on_time: 5 })), "A");
});

test("arriving early is a good day; cancelled and skipped are bad ones", () => {
  assert.equal(grade(service({ n_early: 19, n_cancelled: 1 })), "A"); // 95 %
  assert.equal(grade(service({ n_on_time: 9, n_skipped: 1 })), "B"); // 90 %
  assert.equal(grade(service({ n_on_time: 1, n_very_late: 4 })), "F");
});

test("the verdict names late and cancelled days out of the days counted", () => {
  assert.equal(verdict(service({ n_on_time: 11, n_late: 7, n_very_late: 2 }), "weekdays"), "Late 9 of the last 20 weekdays");
  assert.equal(verdict(service({ n_on_time: 17, n_late: 2, n_cancelled: 1 }), "weekdays"), "Late 2, cancelled 1 of the last 20 weekdays");
  assert.equal(verdict(service({ n_on_time: 6 }), "Saturdays"), "On time every one of the last 6 Saturdays");
  assert.equal(verdict(service({ n_on_time: 1 }), "Sundays"), "Only 1 Sunday recorded so far");
});

test("typical delay in words", () => {
  assert.equal(typical(null), "");
  assert.equal(typical(25), "usually on time");
  assert.equal(typical(250), "usually 4 min late");
  assert.equal(typical(-130), "usually 2 min early");
});

test("one option per place, with the mode only where names clash", () => {
  const got = stationOptions(
    [
      { stop_id: "200060", name: "Central Station", feed_id: "sydneytrains" },
      { stop_id: "200060", name: "Central Station", feed_id: "metro" },
      { stop_id: "X1", name: "Wynyard", feed_id: "sydneytrains" },
      { stop_id: "X2", name: "Wynyard", feed_id: "lightrail-cbdandsoutheast" },
    ],
    { sydneytrains: "Trains", "lightrail-cbdandsoutheast": "Light rail" },
  );
  assert.deepEqual(got, [
    { label: "Central Station", stop_id: "200060" },
    { label: "Wynyard (Light rail)", stop_id: "X2" },
    { label: "Wynyard (Trains)", stop_id: "X1" },
  ]);
});
