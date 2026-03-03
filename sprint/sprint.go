package sprint

import (
	"sort"
	"time"
)

// Anchor is an explicitly defined sprint start date.
type Anchor struct {
	Year      int
	Number    int       // 1–26
	StartDate time.Time // date only (truncated to midnight UTC)
}

// Sprint describes a resolved sprint period.
type Sprint struct {
	Year   int
	Number int
	Start  time.Time
	End    time.Time // exclusive: first day of next sprint
}

// truncate strips a time down to midnight UTC.
func truncate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// sprintAfter returns the (year, number) of the sprint that follows the given one.
func sprintAfter(year, number int) (int, int) {
	number++
	if number > 26 {
		number = 1
		year++
	}
	return year, number
}

// sprintBefore returns the (year, number) of the sprint that precedes the given one.
func sprintBefore(year, number int) (int, int) {
	number--
	if number < 1 {
		number = 26
		year--
	}
	return year, number
}

// Resolve determines which sprint a date falls in, given a set of anchors.
// Returns the sprint info. If no anchors are provided, returns a zero Sprint and false.
func Resolve(anchors []Anchor, date time.Time) (Sprint, bool) {
	if len(anchors) == 0 {
		return Sprint{}, false
	}

	date = truncate(date)

	// Normalize and sort anchors by start date.
	sorted := make([]Anchor, len(anchors))
	copy(sorted, anchors)
	for i := range sorted {
		sorted[i].StartDate = truncate(sorted[i].StartDate)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartDate.Before(sorted[j].StartDate)
	})

	// Case: date is before the first anchor → extrapolate backward.
	if date.Before(sorted[0].StartDate) {
		return extrapolateBackward(sorted[0], date), true
	}

	// Case: date falls between two consecutive anchors.
	for i := 0; i < len(sorted)-1; i++ {
		if !date.Before(sorted[i].StartDate) && date.Before(sorted[i+1].StartDate) {
			return Sprint{
				Year:   sorted[i].Year,
				Number: sorted[i].Number,
				Start:  sorted[i].StartDate,
				End:    sorted[i+1].StartDate,
			}, true
		}
	}

	// Case: date is on or after the last anchor → extrapolate forward.
	last := sorted[len(sorted)-1]
	return extrapolateForward(last, date), true
}

// extrapolateForward finds the sprint for a date on or after the given anchor
// by stepping forward in 14-day increments.
func extrapolateForward(a Anchor, date time.Time) Sprint {
	year, num := a.Year, a.Number
	start := a.StartDate

	for {
		end := start.AddDate(0, 0, 14)
		if date.Before(end) {
			return Sprint{
				Year:   year,
				Number: num,
				Start:  start,
				End:    end,
			}
		}
		start = end
		year, num = sprintAfter(year, num)
	}
}

// extrapolateBackward finds the sprint for a date before the given anchor
// by stepping backward in 14-day increments.
func extrapolateBackward(a Anchor, date time.Time) Sprint {
	year, num := a.Year, a.Number
	end := a.StartDate // exclusive end of the sprint before this one

	for {
		prevYear, prevNum := sprintBefore(year, num)
		prevStart := end.AddDate(0, 0, -14)
		if !date.Before(prevStart) {
			return Sprint{
				Year:   prevYear,
				Number: prevNum,
				Start:  prevStart,
				End:    end,
			}
		}
		end = prevStart
		year, num = prevYear, prevNum
	}
}

// NextStart returns the start date of the next sprint after the given date.
// Returns the date and false if no anchors are available.
func NextStart(anchors []Anchor, date time.Time) (time.Time, bool) {
	current, ok := Resolve(anchors, date)
	if !ok {
		return time.Time{}, false
	}
	return current.End, true
}
