package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/speech/apiv2/speechpb"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/scuba-plaza/arabic-tts/audio"
	"github.com/scuba-plaza/arabic-tts/config"
	"github.com/scuba-plaza/arabic-tts/gcp"
)

type Options struct {
	Project     string
	Region      string
	Language    string
	Model       string
	SegmentMax  time.Duration
	Concurrency int
	NoiseDB     float64
	MinSilence  float64
	Punctuation bool
	Progress    func(done, total int)
	PlanNotice  func(segments []Segment)
}

type SegmentResult struct {
	Index      int     `json:"index"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Text       string  `json:"text"`
	Confidence float32 `json:"confidence,omitempty"`
	Language   string  `json:"language,omitempty"`
	HardCut    bool    `json:"hard_cut,omitempty"`
}

type Transcript struct {
	Source   string          `json:"source"`
	Language string          `json:"language"`
	Model    string          `json:"model"`
	Duration float64         `json:"duration"`
	Segments []SegmentResult `json:"segments"`
}

const (
	defaultConcurrency = 6
	maxAttempts        = 4
	defaultNoiseDB     = -35
	defaultMinSilence  = 0.4
)

var ErrNoSpeech = errors.New("no speech recognised in the audio")

func (o *Options) concurrency() int {
	if o.Concurrency > 0 {
		return o.Concurrency
	}
	return defaultConcurrency
}

func (o *Options) noiseDB() float64 {
	if o.NoiseDB != 0 {
		return o.NoiseDB
	}
	return defaultNoiseDB
}

func (o *Options) minSilence() float64 {
	if o.MinSilence > 0 {
		return o.MinSilence
	}
	return defaultMinSilence
}

func (o *Options) segmentMax() float64 {
	if o.SegmentMax > 0 {
		return o.SegmentMax.Seconds()
	}
	return config.MaxSegment.Seconds()
}

func TranscribeFile(ctx context.Context, client gcp.Recognizer, path string, opts Options) (*Transcript, error) {
	info, err := audio.Probe(ctx, path)
	if err != nil {
		return nil, err
	}

	pcm, size, err := audio.DecodePCM(ctx, path, config.SampleRateSTT)
	if err != nil {
		return nil, err
	}
	defer func() {
		pcm.Close()
		os.Remove(pcm.Name())
	}()

	duration := audio.PCMDuration(size, config.SampleRateSTT)
	if info.Duration > 0 {
		duration = info.Duration
	}

	var silences []audio.Interval
	if duration > opts.segmentMax() {
		silences, err = audio.DetectSilence(ctx, path, opts.noiseDB(), opts.minSilence())
		if err != nil {
			return nil, err
		}
	}

	segments := PlanSegments(audio.PCMDuration(size, config.SampleRateSTT), silences, opts.segmentMax())
	if len(segments) == 0 {
		return nil, fmt.Errorf("%s has no audio to transcribe", path)
	}
	if opts.PlanNotice != nil {
		opts.PlanNotice(segments)
	}

	results := make([]SegmentResult, len(segments))
	var done int
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.concurrency())
	for _, seg := range segments {
		seg := seg
		g.Go(func() error {
			chunk, err := readSpan(pcm, size, seg)
			if err != nil {
				return err
			}
			res, err := recognizeChunk(gctx, client, chunk, opts)
			if err != nil {
				return fmt.Errorf("segment %d (%s-%s): %w",
					seg.Index+1, formatClock(seg.Start), formatClock(seg.End), err)
			}
			res.Index = seg.Index
			res.Start = seg.Start
			res.End = seg.End
			res.HardCut = seg.HardCut
			results[seg.Index] = res
			mu.Lock()
			done++
			if opts.Progress != nil {
				opts.Progress(done, len(segments))
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	kept := make([]SegmentResult, 0, len(results))
	for _, r := range results {
		if r.Text != "" {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil, ErrNoSpeech
	}

	return &Transcript{
		Source:   path,
		Language: opts.Language,
		Model:    opts.Model,
		Duration: duration,
		Segments: kept,
	}, nil
}

func readSpan(f *os.File, size int64, seg Segment) ([]byte, error) {
	start := audio.PCMOffset(seg.Start, config.SampleRateSTT)
	end := audio.PCMOffset(seg.End, config.SampleRateSTT)
	if end > size {
		end = size
	}
	if start >= end {
		return nil, nil
	}
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading decoded audio: %w", err)
	}
	return buf, nil
}

func recognizeChunk(ctx context.Context, client gcp.Recognizer, pcm []byte, opts Options) (SegmentResult, error) {
	if len(pcm) == 0 {
		return SegmentResult{}, nil
	}
	req := &speechpb.RecognizeRequest{
		Recognizer: config.RecognizerPath(opts.Project, opts.Region),
		Config: &speechpb.RecognitionConfig{
			Model:         opts.Model,
			LanguageCodes: []string{opts.Language},
			DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{
				ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
					Encoding:          speechpb.ExplicitDecodingConfig_LINEAR16,
					SampleRateHertz:   config.SampleRateSTT,
					AudioChannelCount: 1,
				},
			},
			Features: &speechpb.RecognitionFeatures{
				EnableAutomaticPunctuation: opts.Punctuation,
			},
		},
		AudioSource: &speechpb.RecognizeRequest_Content{Content: pcm},
	}

	resp, err := withRetry(ctx, func() (*speechpb.RecognizeResponse, error) {
		return client.Recognize(ctx, req)
	})
	if err != nil {
		return SegmentResult{}, err
	}

	var out SegmentResult
	var text string
	for _, r := range resp.Results {
		if len(r.Alternatives) == 0 {
			continue
		}
		alt := r.Alternatives[0]
		if alt.Transcript == "" {
			continue
		}
		if text != "" {
			text += " "
		}
		text += alt.Transcript
		if alt.Confidence > out.Confidence {
			out.Confidence = alt.Confidence
		}
		if r.LanguageCode != "" {
			out.Language = r.LanguageCode
		}
	}
	out.Text = text
	return out, nil
}

func withRetry(ctx context.Context, call func() (*speechpb.RecognizeResponse, error)) (*speechpb.RecognizeResponse, error) {
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := call()
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff + jitter):
		}
		backoff *= 2
	}
	if status.Code(lastErr) == codes.ResourceExhausted {
		return nil, fmt.Errorf("%w (quota exhausted after %d attempts; try a lower --concurrency)", lastErr, maxAttempts)
	}
	return nil, lastErr
}

func retryable(err error) bool {
	switch status.Code(err) {
	case codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.Internal:
		return true
	}
	return false
}
