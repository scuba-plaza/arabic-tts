package stt

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"cloud.google.com/go/speech/apiv2/speechpb"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/scuba-plaza/arabic-tts/audio"
	"github.com/scuba-plaza/arabic-tts/config"
)

type fakeRecognizer struct {
	mu       sync.Mutex
	requests []*speechpb.RecognizeRequest
	inFlight atomic.Int32
	peak     atomic.Int32
	failures atomic.Int32
	failWith codes.Code
	release  chan struct{}
}

func (f *fakeRecognizer) Recognize(_ context.Context, req *speechpb.RecognizeRequest, _ ...gax.CallOption) (*speechpb.RecognizeResponse, error) {
	if n := f.failures.Load(); n > 0 {
		f.failures.Add(-1)
		return nil, status.Error(f.failWith, "injected failure")
	}
	cur := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		peak := f.peak.Load()
		if cur <= peak || f.peak.CompareAndSwap(peak, cur) {
			break
		}
	}
	if f.release != nil {
		<-f.release
	}

	f.mu.Lock()
	f.requests = append(f.requests, req)
	index := len(f.requests)
	f.mu.Unlock()

	return &speechpb.RecognizeResponse{
		Results: []*speechpb.SpeechRecognitionResult{{
			Alternatives: []*speechpb.SpeechRecognitionAlternative{{
				Transcript: fmt.Sprintf("مقطع %d", index),
				Confidence: 0.9,
			}},
			LanguageCode: "ar-XA",
		}},
	}, nil
}

func writeTestWAV(t *testing.T, path string, spans []struct {
	Seconds float64
	Tone    bool
}) {
	t.Helper()
	const rate = config.SampleRateSTT
	var pcm []byte
	phase := 0.0
	for _, span := range spans {
		n := int(span.Seconds * rate)
		for i := 0; i < n; i++ {
			var sample int16
			if span.Tone {
				sample = int16(12000 * math.Sin(phase))
				phase += 2 * math.Pi * 220 / rate
			}
			pcm = append(pcm, byte(sample), byte(sample>>8))
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := audio.WriteWAV(f, pcm, rate, 1); err != nil {
		t.Fatal(err)
	}
}

type span = struct {
	Seconds float64
	Tone    bool
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if err := audio.Available(); err != nil {
		t.Skipf("ffmpeg is required for this test: %v", err)
	}
}

func testOptions() Options {
	return Options{
		Project:     "test-project",
		Region:      "eu",
		Language:    "ar-XA",
		Model:       "chirp_3",
		Punctuation: true,
	}
}

func TestTranscribeShortFileIsOneRequest(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "short.wav")
	writeTestWAV(t, path, []span{{2, true}})

	fake := &fakeRecognizer{}
	tr, err := TranscribeFile(context.Background(), fake, path, testOptions())
	if err != nil {
		t.Fatalf("TranscribeFile: %v", err)
	}
	if len(fake.requests) != 1 {
		t.Errorf("short audio should be a single request, got %d", len(fake.requests))
	}
	if len(tr.Segments) != 1 || tr.Segments[0].Text == "" {
		t.Errorf("unexpected transcript: %+v", tr.Segments)
	}
	if tr.Model != "chirp_3" || tr.Language != "ar-XA" {
		t.Errorf("transcript metadata is wrong: %+v", tr)
	}
}

func TestTranscribeSplitsLongFileOnSilence(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "long.wav")
	writeTestWAV(t, path, []span{{3, true}, {1, false}, {3, true}, {1, false}, {4, true}})

	fake := &fakeRecognizer{}
	opts := testOptions()
	opts.SegmentMax = 5_000_000_000

	var plan []Segment
	opts.PlanNotice = func(s []Segment) { plan = s }

	tr, err := TranscribeFile(context.Background(), fake, path, opts)
	if err != nil {
		t.Fatalf("TranscribeFile: %v", err)
	}
	if len(plan) < 2 {
		t.Fatalf("expected the file to be segmented, got %d segment(s)", len(plan))
	}
	for i, s := range plan {
		if s.HardCut {
			t.Errorf("segment %d was forced even though the file has clear silences", i)
		}
		if s.Duration() > 5.0001 {
			t.Errorf("segment %d is %.2fs, over the 5s limit", i, s.Duration())
		}
	}
	if len(fake.requests) != len(plan) {
		t.Errorf("made %d requests for %d segments", len(fake.requests), len(plan))
	}
	if len(tr.Segments) != len(plan) {
		t.Errorf("returned %d results for %d segments", len(tr.Segments), len(plan))
	}
}

func TestTranscribeOrdersResultsByTimeline(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "order.wav")
	writeTestWAV(t, path, []span{{3, true}, {1, false}, {3, true}, {1, false}, {3, true}, {1, false}, {3, true}})

	fake := &fakeRecognizer{}
	opts := testOptions()
	opts.SegmentMax = 4_000_000_000
	opts.Concurrency = 4

	tr, err := TranscribeFile(context.Background(), fake, path, opts)
	if err != nil {
		t.Fatalf("TranscribeFile: %v", err)
	}
	for i, s := range tr.Segments {
		if s.Index != i {
			t.Errorf("result %d carries index %d", i, s.Index)
		}
		if i > 0 && s.Start < tr.Segments[i-1].Start {
			t.Errorf("segment %d starts before its predecessor: %v < %v", i, s.Start, tr.Segments[i-1].Start)
		}
	}
}

func TestTranscribeSendsCorrectRecognitionConfig(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "config.wav")
	writeTestWAV(t, path, []span{{2, true}})

	fake := &fakeRecognizer{}
	if _, err := TranscribeFile(context.Background(), fake, path, testOptions()); err != nil {
		t.Fatal(err)
	}
	req := fake.requests[0]
	if want := "projects/test-project/locations/eu/recognizers/_"; req.Recognizer != want {
		t.Errorf("recognizer is %q, want %q", req.Recognizer, want)
	}
	if req.Config.Model != "chirp_3" {
		t.Errorf("model is %q", req.Config.Model)
	}
	dec, ok := req.Config.DecodingConfig.(*speechpb.RecognitionConfig_ExplicitDecodingConfig)
	if !ok {
		t.Fatalf("expected an explicit decoding config, got %T", req.Config.DecodingConfig)
	}
	if dec.ExplicitDecodingConfig.SampleRateHertz != config.SampleRateSTT {
		t.Errorf("sample rate is %d, want %d", dec.ExplicitDecodingConfig.SampleRateHertz, config.SampleRateSTT)
	}
	if dec.ExplicitDecodingConfig.AudioChannelCount != 1 {
		t.Errorf("channel count is %d, want 1", dec.ExplicitDecodingConfig.AudioChannelCount)
	}

	if req.Config.Features.EnableWordTimeOffsets {
		t.Error("word time offsets must not be enabled for chirp_3")
	}
	if !req.Config.Features.EnableAutomaticPunctuation {
		t.Error("automatic punctuation should follow the option")
	}
}

func TestTranscribeRespectsConcurrencyLimit(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "concurrent.wav")
	var spans []span
	for i := 0; i < 8; i++ {
		spans = append(spans, span{3, true}, span{1, false})
	}
	writeTestWAV(t, path, spans)

	fake := &fakeRecognizer{release: make(chan struct{})}
	close(fake.release)
	opts := testOptions()
	opts.SegmentMax = 4_000_000_000
	opts.Concurrency = 2

	if _, err := TranscribeFile(context.Background(), fake, path, opts); err != nil {
		t.Fatal(err)
	}
	if peak := fake.peak.Load(); peak > 2 {
		t.Errorf("ran %d requests at once, limit was 2", peak)
	}
}

func TestRetryRecoversFromTransientFailures(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "retry.wav")
	writeTestWAV(t, path, []span{{2, true}})

	fake := &fakeRecognizer{failWith: codes.Unavailable}
	fake.failures.Store(2)

	tr, err := TranscribeFile(context.Background(), fake, path, testOptions())
	if err != nil {
		t.Fatalf("transient failures should be retried: %v", err)
	}
	if len(tr.Segments) != 1 {
		t.Errorf("expected a transcript after retrying, got %+v", tr.Segments)
	}
}

func TestPermanentFailureIsNotRetried(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "denied.wav")
	writeTestWAV(t, path, []span{{2, true}})

	fake := &fakeRecognizer{failWith: codes.PermissionDenied}
	fake.failures.Store(100)

	_, err := TranscribeFile(context.Background(), fake, path, testOptions())
	if err == nil {
		t.Fatal("expected the call to fail")
	}
	if status.Code(errors.Unwrap(err)) != codes.PermissionDenied && status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected a PermissionDenied status, got %v", err)
	}
	if remaining := fake.failures.Load(); remaining < 99 {
		t.Errorf("a permanent failure was retried %d times", 100-remaining)
	}
}

func TestQuotaExhaustionSuggestsLowerConcurrency(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "quota.wav")
	writeTestWAV(t, path, []span{{2, true}})

	fake := &fakeRecognizer{failWith: codes.ResourceExhausted}
	fake.failures.Store(100)

	_, err := TranscribeFile(context.Background(), fake, path, testOptions())
	if err == nil {
		t.Fatal("expected the call to fail")
	}
	if got := err.Error(); !strings.Contains(got, "concurrency") {
		t.Errorf("error should suggest lowering concurrency, got %v", got)
	}
}

func TestSilentAudioReportsNoSpeech(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "silent.wav")
	writeTestWAV(t, path, []span{{2, false}})

	fake := &fakeRecognizer{}
	emptyFake := &emptyRecognizer{fakeRecognizer: fake}
	_, err := TranscribeFile(context.Background(), emptyFake, path, testOptions())
	if !errors.Is(err, ErrNoSpeech) {
		t.Errorf("expected ErrNoSpeech, got %v", err)
	}
}

type emptyRecognizer struct{ *fakeRecognizer }

func (e *emptyRecognizer) Recognize(context.Context, *speechpb.RecognizeRequest, ...gax.CallOption) (*speechpb.RecognizeResponse, error) {
	return &speechpb.RecognizeResponse{}, nil
}
