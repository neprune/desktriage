package sprint

import (
	"testing"
	"time"
)

func d(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestResolveSingleAnchorWithinFirst(t *testing.T) {
	anchors := []Anchor{
		{Year: 2025, Number: 1, StartDate: d(2025, 1, 6)},
	}
	s, ok := Resolve(anchors, d(2025, 1, 10))
	if !ok {
		t.Fatal("expected ok")
	}
	if s.Year != 2025 || s.Number != 1 {
		t.Errorf("got sprint %d/%d, want 2025/1", s.Year, s.Number)
	}
	if !s.Start.Equal(d(2025, 1, 6)) {
		t.Errorf("start = %v, want 2025-01-06", s.Start)
	}
	if !s.End.Equal(d(2025, 1, 20)) {
		t.Errorf("end = %v, want 2025-01-20", s.End)
	}
}

func TestResolveSingleAnchorExtrapolateForward(t *testing.T) {
	anchors := []Anchor{
		{Year: 2025, Number: 1, StartDate: d(2025, 1, 6)},
	}
	// 14 days later = sprint 2 start: Jan 20
	// 28 days later = sprint 3 start: Feb 3
	// Test a date in sprint 2: Jan 25
	s, ok := Resolve(anchors, d(2025, 1, 25))
	if !ok {
		t.Fatal("expected ok")
	}
	if s.Year != 2025 || s.Number != 2 {
		t.Errorf("got sprint %d/%d, want 2025/2", s.Year, s.Number)
	}
	if !s.Start.Equal(d(2025, 1, 20)) {
		t.Errorf("start = %v, want 2025-01-20", s.Start)
	}
	if !s.End.Equal(d(2025, 2, 3)) {
		t.Errorf("end = %v, want 2025-02-03", s.End)
	}
}

func TestResolveSingleAnchorExtrapolateBackward(t *testing.T) {
	anchors := []Anchor{
		{Year: 2025, Number: 3, StartDate: d(2025, 2, 3)},
	}
	// Backward: sprint 2 starts Jan 20, sprint 1 starts Jan 6
	// Jan 10 is in sprint 1 (Jan 6 – Jan 20)
	s, ok := Resolve(anchors, d(2025, 1, 10))
	if !ok {
		t.Fatal("expected ok")
	}
	if s.Year != 2025 || s.Number != 1 {
		t.Errorf("got sprint %d/%d, want 2025/1", s.Year, s.Number)
	}
	if !s.Start.Equal(d(2025, 1, 6)) {
		t.Errorf("start = %v, want 2025-01-06", s.Start)
	}
	if !s.End.Equal(d(2025, 1, 20)) {
		t.Errorf("end = %v, want 2025-01-20", s.End)
	}

	// Also verify sprint 2: Jan 25 should be sprint 2
	s2, ok := Resolve(anchors, d(2025, 1, 25))
	if !ok {
		t.Fatal("expected ok")
	}
	if s2.Year != 2025 || s2.Number != 2 {
		t.Errorf("got sprint %d/%d, want 2025/2", s2.Year, s2.Number)
	}
	if !s2.Start.Equal(d(2025, 1, 20)) {
		t.Errorf("start = %v, want 2025-01-20", s2.Start)
	}
	if !s2.End.Equal(d(2025, 2, 3)) {
		t.Errorf("end = %v, want 2025-02-03", s2.End)
	}
}

func TestResolveTwoAnchorsThreeWeekSprint(t *testing.T) {
	anchors := []Anchor{
		{Year: 2025, Number: 3, StartDate: d(2025, 2, 1)},
		{Year: 2025, Number: 4, StartDate: d(2025, 2, 22)},
	}
	// Feb 20 should be in sprint 3 (3-week sprint: Feb 1 – Feb 22)
	s, ok := Resolve(anchors, d(2025, 2, 20))
	if !ok {
		t.Fatal("expected ok")
	}
	if s.Year != 2025 || s.Number != 3 {
		t.Errorf("got sprint %d/%d, want 2025/3", s.Year, s.Number)
	}
	if !s.Start.Equal(d(2025, 2, 1)) {
		t.Errorf("start = %v, want 2025-02-01", s.Start)
	}
	if !s.End.Equal(d(2025, 2, 22)) {
		t.Errorf("end = %v, want 2025-02-22", s.End)
	}
}

func TestResolveTwoAnchorsOneWeekSprint(t *testing.T) {
	anchors := []Anchor{
		{Year: 2025, Number: 4, StartDate: d(2025, 2, 22)},
		{Year: 2025, Number: 5, StartDate: d(2025, 3, 1)},
	}
	// Feb 25 should be in sprint 4 (1-week sprint: Feb 22 – Mar 1)
	s, ok := Resolve(anchors, d(2025, 2, 25))
	if !ok {
		t.Fatal("expected ok")
	}
	if s.Year != 2025 || s.Number != 4 {
		t.Errorf("got sprint %d/%d, want 2025/4", s.Year, s.Number)
	}
	if !s.Start.Equal(d(2025, 2, 22)) {
		t.Errorf("start = %v, want 2025-02-22", s.Start)
	}
	if !s.End.Equal(d(2025, 3, 1)) {
		t.Errorf("end = %v, want 2025-03-01", s.End)
	}
}

func TestResolveYearBoundarySprint26(t *testing.T) {
	// Sprint 26 of 2024 starts Dec 23 and bleeds into January 2025.
	// With a single anchor we extrapolate: sprint 26 runs Dec 23 – Jan 6.
	anchors := []Anchor{
		{Year: 2024, Number: 26, StartDate: d(2024, 12, 23)},
	}
	s, ok := Resolve(anchors, d(2025, 1, 2))
	if !ok {
		t.Fatal("expected ok")
	}
	if s.Year != 2024 || s.Number != 26 {
		t.Errorf("got sprint %d/%d, want 2024/26", s.Year, s.Number)
	}
	if !s.Start.Equal(d(2024, 12, 23)) {
		t.Errorf("start = %v, want 2024-12-23", s.Start)
	}
	if !s.End.Equal(d(2025, 1, 6)) {
		t.Errorf("end = %v, want 2025-01-06", s.End)
	}

	// Jan 6 should be sprint 1 of 2025
	s2, ok := Resolve(anchors, d(2025, 1, 6))
	if !ok {
		t.Fatal("expected ok")
	}
	if s2.Year != 2025 || s2.Number != 1 {
		t.Errorf("got sprint %d/%d, want 2025/1", s2.Year, s2.Number)
	}
}

func TestNextStartBasic(t *testing.T) {
	anchors := []Anchor{
		{Year: 2025, Number: 1, StartDate: d(2025, 1, 6)},
	}
	next, ok := NextStart(anchors, d(2025, 1, 10))
	if !ok {
		t.Fatal("expected ok")
	}
	if !next.Equal(d(2025, 1, 20)) {
		t.Errorf("next = %v, want 2025-01-20", next)
	}
}

func TestNextStartOnBoundary(t *testing.T) {
	anchors := []Anchor{
		{Year: 2025, Number: 1, StartDate: d(2025, 1, 6)},
	}
	// Date exactly on sprint 2 start (Jan 20): should return sprint 3 start (Feb 3)
	next, ok := NextStart(anchors, d(2025, 1, 20))
	if !ok {
		t.Fatal("expected ok")
	}
	if !next.Equal(d(2025, 2, 3)) {
		t.Errorf("next = %v, want 2025-02-03", next)
	}
}

func TestResolveNoAnchors(t *testing.T) {
	_, ok := Resolve(nil, d(2025, 1, 10))
	if ok {
		t.Error("expected ok=false for empty anchors")
	}

	_, ok = NextStart(nil, d(2025, 1, 10))
	if ok {
		t.Error("expected ok=false for empty anchors in NextStart")
	}
}
