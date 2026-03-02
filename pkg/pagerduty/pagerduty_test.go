package pagerduty

import (
	"testing"
	"time"
)

func TestMergeOnCallRuns(t *testing.T) {
	tests := []struct {
		name    string
		oncalls []OnCall
		want    []OnCallRun
	}{
		{
			name:    "empty input",
			oncalls: nil,
			want:    nil,
		},
		{
			name: "single entry",
			oncalls: []OnCall{
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-01T09:00:00Z", End: "2025-03-02T09:00:00Z"},
			},
			want: []OnCallRun{
				{Schedule: "Primary", Start: mustParse("2025-03-01T09:00:00Z"), End: mustParse("2025-03-02T09:00:00Z")},
			},
		},
		{
			name: "contiguous entries merge",
			oncalls: []OnCall{
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-01T09:00:00Z", End: "2025-03-02T09:00:00Z"},
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-02T09:00:00Z", End: "2025-03-03T09:00:00Z"},
			},
			want: []OnCallRun{
				{Schedule: "Primary", Start: mustParse("2025-03-01T09:00:00Z"), End: mustParse("2025-03-03T09:00:00Z")},
			},
		},
		{
			name: "same day gap merges",
			oncalls: []OnCall{
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-01T09:00:00Z", End: "2025-03-02T09:00:00Z"},
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-02T17:00:00Z", End: "2025-03-03T09:00:00Z"},
			},
			want: []OnCallRun{
				{Schedule: "Primary", Start: mustParse("2025-03-01T09:00:00Z"), End: mustParse("2025-03-03T09:00:00Z")},
			},
		},
		{
			name: "different day gap does not merge",
			oncalls: []OnCall{
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-01T09:00:00Z", End: "2025-03-02T09:00:00Z"},
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-04T09:00:00Z", End: "2025-03-05T09:00:00Z"},
			},
			want: []OnCallRun{
				{Schedule: "Primary", Start: mustParse("2025-03-01T09:00:00Z"), End: mustParse("2025-03-02T09:00:00Z")},
				{Schedule: "Primary", Start: mustParse("2025-03-04T09:00:00Z"), End: mustParse("2025-03-05T09:00:00Z")},
			},
		},
		{
			name: "different schedules stay separate",
			oncalls: []OnCall{
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-01T09:00:00Z", End: "2025-03-02T09:00:00Z"},
				{ScheduleID: "S2", ScheduleName: "Secondary", Start: "2025-03-01T17:00:00Z", End: "2025-03-02T17:00:00Z"},
			},
			want: []OnCallRun{
				{Schedule: "Primary", Start: mustParse("2025-03-01T09:00:00Z"), End: mustParse("2025-03-02T09:00:00Z")},
				{Schedule: "Secondary", Start: mustParse("2025-03-01T17:00:00Z"), End: mustParse("2025-03-02T17:00:00Z")},
			},
		},
		{
			name: "unsorted input is handled",
			oncalls: []OnCall{
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-03T09:00:00Z", End: "2025-03-04T09:00:00Z"},
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-01T09:00:00Z", End: "2025-03-02T09:00:00Z"},
				{ScheduleID: "S1", ScheduleName: "Primary", Start: "2025-03-02T09:00:00Z", End: "2025-03-03T09:00:00Z"},
			},
			want: []OnCallRun{
				{Schedule: "Primary", Start: mustParse("2025-03-01T09:00:00Z"), End: mustParse("2025-03-04T09:00:00Z")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeOnCallRuns(tt.oncalls)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d runs, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Schedule != tt.want[i].Schedule {
					t.Errorf("run[%d].Schedule = %q, want %q", i, got[i].Schedule, tt.want[i].Schedule)
				}
				if !got[i].Start.Equal(tt.want[i].Start) {
					t.Errorf("run[%d].Start = %v, want %v", i, got[i].Start, tt.want[i].Start)
				}
				if !got[i].End.Equal(tt.want[i].End) {
					t.Errorf("run[%d].End = %v, want %v", i, got[i].End, tt.want[i].End)
				}
			}
		})
	}
}

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestSameDay(t *testing.T) {
	utc := time.UTC
	tokyo := time.FixedZone("Asia/Tokyo", 9*60*60)      // UTC+9
	nyc := time.FixedZone("America/New_York", -5*60*60) // UTC-5

	tests := []struct {
		name string
		a, b time.Time
		loc  *time.Location
		want bool
	}{
		{
			name: "same instant is same day",
			a:    mustParse("2025-03-01T12:00:00Z"),
			b:    mustParse("2025-03-01T12:00:00Z"),
			loc:  utc,
			want: true,
		},
		{
			name: "different UTC days",
			a:    mustParse("2025-03-01T23:00:00Z"),
			b:    mustParse("2025-03-02T01:00:00Z"),
			loc:  utc,
			want: false,
		},
		{
			name: "different UTC days but same day in UTC+9",
			a:    mustParse("2025-03-01T23:00:00Z"), // Mar 2 08:00 Tokyo
			b:    mustParse("2025-03-02T01:00:00Z"), // Mar 2 10:00 Tokyo
			loc:  tokyo,
			want: true,
		},
		{
			name: "same UTC day but different days in UTC-5",
			a:    mustParse("2025-03-02T04:00:00Z"), // Mar 1 23:00 NYC
			b:    mustParse("2025-03-02T06:00:00Z"), // Mar 2 01:00 NYC
			loc:  nyc,
			want: false,
		},
		{
			name: "midnight boundary same day",
			a:    mustParse("2025-03-01T00:00:00Z"),
			b:    mustParse("2025-03-01T23:59:59Z"),
			loc:  utc,
			want: true,
		},
		{
			name: "midnight boundary different days",
			a:    mustParse("2025-03-01T23:59:59Z"),
			b:    mustParse("2025-03-02T00:00:00Z"),
			loc:  utc,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sameDay(tt.a, tt.b, tt.loc)
			if got != tt.want {
				t.Errorf("sameDay(%v, %v, %s) = %v, want %v",
					tt.a, tt.b, tt.loc, got, tt.want)
			}
		})
	}
}
