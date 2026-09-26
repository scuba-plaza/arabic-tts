package stt

import (
	"testing"

	"github.com/scuba-plaza/arabic-tts/audio"
)

func checkInvariants(t *testing.T, segs []Segment, duration, max float64) {
	t.Helper()
	if len(segs) == 0 {
		t.Fatal("no segments produced")
	}
	if segs[0].Start != 0 {
		t.Errorf("first segment starts at %v, want 0", segs[0].Start)
	}
	last := segs[len(segs)-1]
	if last.End != duration {
		t.Errorf("last segment ends at %v, want %v", last.End, duration)
	}
	for i, s := range segs {
		if s.Index != i {
			t.Errorf("segment %d carries index %d", i, s.Index)
		}
		if s.Duration() <= 0 {
			t.Errorf("segment %d is empty: %+v", i, s)
		}
		if s.Duration() > max+1e-9 {
			t.Errorf("segment %d is %.3fs, over the %.3fs limit", i, s.Duration(), max)
		}
		if i > 0 && segs[i-1].End != s.Start {
			t.Errorf("gap between segment %d and %d: %v vs %v", i-1, i, segs[i-1].End, s.Start)
		}
	}
}

func TestPlanShortAudioIsOneSegment(t *testing.T) {
	segs := PlanSegments(30, nil, 55)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].HardCut {
		t.Error("a whole-file segment should not be marked as a forced cut")
	}
	checkInvariants(t, segs, 30, 55)
}

func TestPlanNoSilenceForcesHardCuts(t *testing.T) {
	segs := PlanSegments(180, nil, 55)
	checkInvariants(t, segs, 180, 55)
	for i, s := range segs[:len(segs)-1] {
		if !s.HardCut {
			t.Errorf("segment %d should be a forced cut when there is no silence", i)
		}
	}
}

func TestPlanCutsAtSilenceMidpoints(t *testing.T) {
	silences := []audio.Interval{
		{Start: 49, End: 51},
		{Start: 99, End: 101},
		{Start: 149, End: 151},
	}
	segs := PlanSegments(200, silences, 55)
	checkInvariants(t, segs, 200, 55)
	want := []float64{50, 100, 150, 200}
	if len(segs) != len(want) {
		t.Fatalf("expected %d segments, got %d", len(want), len(segs))
	}
	for i, s := range segs {
		if s.End != want[i] {
			t.Errorf("segment %d ends at %v, want %v", i, s.End, want[i])
		}
		if i < len(segs)-1 && s.HardCut {
			t.Errorf("segment %d cut at a silence should not be marked forced", i)
		}
	}
}

func TestPlanIgnoresSilenceBeyondReach(t *testing.T) {
	silences := []audio.Interval{{Start: 119, End: 121}}
	segs := PlanSegments(200, silences, 55)
	checkInvariants(t, segs, 200, 55)
	if !segs[0].HardCut {
		t.Error("a silence past the window should not rescue the first cut")
	}
}

func TestPlanSkipsSilenceTooCloseToStart(t *testing.T) {
	silences := []audio.Interval{{Start: 0.1, End: 0.3}}
	segs := PlanSegments(120, silences, 55)
	checkInvariants(t, segs, 120, 55)
	if segs[0].Duration() < minSegment {
		t.Errorf("first segment is %.3fs, shorter than the %.1fs minimum", segs[0].Duration(), minSegment)
	}
}

func TestPlanBoundaryExactlyAtLimit(t *testing.T) {
	silences := []audio.Interval{{Start: 55, End: 55}}
	segs := PlanSegments(110, silences, 55)
	checkInvariants(t, segs, 110, 55)
	if segs[0].End != 55 {
		t.Errorf("a candidate exactly at the limit should be usable, got %v", segs[0].End)
	}
	if segs[0].HardCut {
		t.Error("a cut landing exactly on the limit but at a silence is not forced")
	}
}

func TestPlanDenseSilence(t *testing.T) {
	var silences []audio.Interval
	for at := 5.0; at < 300; at += 5 {
		silences = append(silences, audio.Interval{Start: at, End: at + 0.5})
	}
	segs := PlanSegments(300, silences, 55)
	checkInvariants(t, segs, 300, 55)
	for i, s := range segs {
		if s.HardCut {
			t.Errorf("segment %d was forced despite dense silence", i)
		}
	}
}

func TestPlanZeroDuration(t *testing.T) {
	if segs := PlanSegments(0, nil, 55); segs != nil {
		t.Errorf("zero-length audio should produce no segments, got %+v", segs)
	}
}
