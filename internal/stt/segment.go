package stt

import (
	"sort"

	"arabic-tts/internal/audio"
)

type Segment struct {
	Index   int
	Start   float64
	End     float64
	HardCut bool
}

func (s Segment) Duration() float64 { return s.End - s.Start }

const minSegment = 1.0

func PlanSegments(duration float64, silences []audio.Interval, maxSegment float64) []Segment {
	if duration <= 0 {
		return nil
	}
	if maxSegment <= 0 {
		maxSegment = duration
	}
	if duration <= maxSegment {
		return []Segment{{Index: 0, Start: 0, End: duration}}
	}

	candidates := make([]float64, 0, len(silences))
	for _, s := range silences {
		mid := (s.Start + s.End) / 2
		if mid > 0 && mid < duration {
			candidates = append(candidates, mid)
		}
	}
	sort.Float64s(candidates)

	var segments []Segment
	pos := 0.0
	for duration-pos > maxSegment {
		limit := pos + maxSegment
		cut := bestCut(candidates, pos, limit)
		hard := false
		if cut <= pos {
			cut = limit
			hard = true
		}
		segments = append(segments, Segment{Index: len(segments), Start: pos, End: cut, HardCut: hard})
		pos = cut
	}
	if duration-pos > 0 {
		segments = append(segments, Segment{Index: len(segments), Start: pos, End: duration})
	}
	return segments
}

func bestCut(candidates []float64, pos, limit float64) float64 {
	idx := sort.Search(len(candidates), func(i int) bool { return candidates[i] > limit })
	if idx == 0 {
		return 0
	}
	c := candidates[idx-1]
	if c <= pos || c-pos < minSegment {
		return 0
	}
	return c
}
