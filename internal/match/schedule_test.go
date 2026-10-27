package match

import (
	"slices"
	"testing"
)

func TestSchedule_Legs_RidesFromOneStationToAnother(t *testing.T) {
	s := NewSchedule(1, "trains")
	for id, name := range map[string]string{"HBY": "Hornsby", "HBY1": "Hornsby Platform 1", "HBY2": "Hornsby Platform 2", "CEN": "Central", "CEN1": "Central Platform 1", "MID": "Midway"} {
		s.AddStop(id, name)
	}
	s.SetParent("HBY1", "HBY")
	s.SetParent("HBY2", "HBY")
	s.SetParent("CEN1", "CEN")
	add := func(trip string, calls ...StopTime) {
		s.AddTrip(trip, Trip{RouteID: "R"})
		for _, c := range calls {
			s.AddStopTime(trip, c)
		}
	}
	add("down", StopTime{1, "HBY1", 100, 110}, StopTime{2, "MID", 200, 210}, StopTime{3, "CEN1", 300, NoTime})
	add("other-platform", StopTime{1, "HBY2", 400, 400}, StopTime{2, "CEN1", 500, 500})
	add("up", StopTime{1, "CEN1", 100, 100}, StopTime{2, "HBY1", 200, 200})
	add("short", StopTime{1, "HBY1", 100, 100}, StopTime{2, "MID", 200, 200})
	add("untimed-end", StopTime{1, "HBY1", 100, 100}, StopTime{2, "CEN1", NoTime, NoTime})

	legs, toStops := s.Legs("HBY", "CEN")

	slices.SortFunc(legs, func(a, b Leg) int { return int(a.DepartS - b.DepartS) })
	t.Run("trips calling at any platform of from and then of to, departure then arrival", func(t *testing.T) {
		want := []Leg{{"down", 110, 300}, {"other-platform", 400, 500}}
		if !slices.Equal(legs, want) {
			t.Errorf("legs = %+v, want %+v; not the trip the other way, one that stops short, or one with no time at to", legs, want)
		}
	})
	t.Run("to's stops include the station and its platforms", func(t *testing.T) {
		slices.Sort(toStops)
		if !slices.Equal(toStops, []string{"CEN", "CEN1"}) {
			t.Errorf("to stops = %v", toStops)
		}
	})
	t.Run("a station the timetable lacks has no legs", func(t *testing.T) {
		if legs, to := s.Legs("HBY", "NOWHERE"); legs != nil || to != nil {
			t.Errorf("legs %v to %v", legs, to)
		}
	})
}
