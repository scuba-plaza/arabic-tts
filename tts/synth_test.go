package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/scuba-plaza/arabic-tts/audio"
)

type fakeSynthesizer struct {
	mu        sync.Mutex
	requests  []*texttospeechpb.SynthesizeSpeechRequest
	voices    []*texttospeechpb.Voice
	err       error
	transient []error
	calls     int
}

func (f *fakeSynthesizer) SynthesizeSpeech(_ context.Context, req *texttospeechpb.SynthesizeSpeechRequest, _ ...gax.CallOption) (*texttospeechpb.SynthesizeSpeechResponse, error) {
	f.mu.Lock()
	f.calls++
	if len(f.transient) > 0 {
		err := f.transient[0]
		f.transient = f.transient[1:]
		f.mu.Unlock()
		return nil, err
	}
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()

	pcm := make([]byte, 480)
	for i := 0; i+1 < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], uint16(i))
	}
	if req.AudioConfig.AudioEncoding != texttospeechpb.AudioEncoding_LINEAR16 {
		return &texttospeechpb.SynthesizeSpeechResponse{AudioContent: append([]byte("ID3COMPRESSED"), pcm...)}, nil
	}
	var buf bytes.Buffer
	if err := audio.WriteWAV(&buf, pcm, int(req.AudioConfig.SampleRateHertz), 1); err != nil {
		return nil, err
	}
	return &texttospeechpb.SynthesizeSpeechResponse{AudioContent: buf.Bytes()}, nil
}

func (f *fakeSynthesizer) ListVoices(_ context.Context, _ *texttospeechpb.ListVoicesRequest, _ ...gax.CallOption) (*texttospeechpb.ListVoicesResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &texttospeechpb.ListVoicesResponse{Voices: f.voices}, nil
}

func baseOptions(out string) Options {
	return Options{
		Voice:      "ar-XA-Chirp3-HD-Kore",
		Language:   "ar-XA",
		SampleRate: 24000,
		Output:     out,
	}
}

func TestSynthesizeShortTextTakesTheDirectPath(t *testing.T) {
	fake := &fakeSynthesizer{}
	out := filepath.Join(t.TempDir(), "hello.mp3")
	res, err := Synthesize(context.Background(), fake, "مرحبا بك", baseOptions(out))
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if res.Chunks != 1 {
		t.Errorf("expected a single chunk, got %d", res.Chunks)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(fake.requests))
	}
	if got := fake.requests[0].AudioConfig.AudioEncoding; got != texttospeechpb.AudioEncoding_MP3 {
		t.Errorf("short text to .mp3 should request MP3 directly, got %v", got)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output file missing: %v", err)
	}
}

func TestSynthesizeLongTextIsChunkedAndStitched(t *testing.T) {
	fake := &fakeSynthesizer{}
	out := filepath.Join(t.TempDir(), "long.wav")
	text := strings.Repeat("السلام عليكم ورحمة الله وبركاته. ", 400)

	res, err := Synthesize(context.Background(), fake, text, baseOptions(out))
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if res.Chunks < 2 {
		t.Fatalf("expected the text to be chunked, got %d chunk(s)", res.Chunks)
	}
	if len(fake.requests) != res.Chunks {
		t.Errorf("made %d requests for %d chunks", len(fake.requests), res.Chunks)
	}
	for i, req := range fake.requests {
		if got := req.AudioConfig.AudioEncoding; got != texttospeechpb.AudioEncoding_LINEAR16 {
			t.Errorf("chunk %d requested %v; stitching needs LINEAR16", i, got)
		}
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	if n := bytes.Count(written, []byte("RIFF")); n != 1 {
		t.Errorf("output holds %d RIFF headers, want exactly 1", n)
	}
	if !bytes.HasPrefix(written, []byte("RIFF")) {
		t.Error("output does not start with a RIFF header")
	}
	if _, err := audio.StripWAVHeader(written); err != nil {
		t.Errorf("stitched output is not a readable WAV: %v", err)
	}
}

func TestSynthesizeInsertsGapBetweenChunks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "gap.wav")
	text := strings.Repeat("السلام عليكم ورحمة الله وبركاته. ", 400)

	opts := baseOptions(out)
	withoutGap, err := Synthesize(context.Background(), &fakeSynthesizer{}, text, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Gap = 500_000_000
	withGap, err := Synthesize(context.Background(), &fakeSynthesizer{}, text, opts)
	if err != nil {
		t.Fatal(err)
	}
	if withGap.Bytes <= withoutGap.Bytes {
		t.Errorf("gap did not lengthen the output: %d vs %d", withGap.Bytes, withoutGap.Bytes)
	}
}

func TestSynthesizeCarriesInputMode(t *testing.T) {
	for _, tc := range []struct {
		mode  InputMode
		check func(*texttospeechpb.SynthesisInput) bool
	}{
		{ModeText, func(i *texttospeechpb.SynthesisInput) bool {
			_, ok := i.InputSource.(*texttospeechpb.SynthesisInput_Text)
			return ok
		}},
		{ModeSSML, func(i *texttospeechpb.SynthesisInput) bool {
			_, ok := i.InputSource.(*texttospeechpb.SynthesisInput_Ssml)
			return ok
		}},
		{ModeMarkup, func(i *texttospeechpb.SynthesisInput) bool {
			_, ok := i.InputSource.(*texttospeechpb.SynthesisInput_Markup)
			return ok
		}},
	} {
		fake := &fakeSynthesizer{}
		opts := baseOptions(filepath.Join(t.TempDir(), "mode.wav"))
		opts.Mode = tc.mode
		if _, err := Synthesize(context.Background(), fake, "مرحبا", opts); err != nil {
			t.Fatalf("mode %v: %v", tc.mode, err)
		}
		if !tc.check(fake.requests[0].Input) {
			t.Errorf("mode %v routed to the wrong input field", tc.mode)
		}
	}
}

func TestValidateRejectsPitchOnChirp3(t *testing.T) {
	opts := baseOptions("out.wav")
	opts.PitchSet = true
	opts.Pitch = 2
	err := opts.Validate()
	if err == nil {
		t.Fatal("expected pitch on a Chirp 3 voice to be rejected")
	}
	if !strings.Contains(err.Error(), "pitch") {
		t.Errorf("error should mention pitch, got %v", err)
	}

	opts.Voice = "ar-XA-Wavenet-B"
	if err := opts.Validate(); err != nil {
		t.Errorf("Wavenet voices accept pitch, got %v", err)
	}
}

func TestValidateRejectsOutOfRangeRate(t *testing.T) {
	for _, rate := range []float64{0.1, 2.5} {
		opts := baseOptions("out.wav")
		opts.SpeakingRate = rate
		if err := opts.Validate(); err == nil {
			t.Errorf("rate %v should be rejected", rate)
		}
	}
	opts := baseOptions("out.wav")
	opts.SpeakingRate = 0.95
	if err := opts.Validate(); err != nil {
		t.Errorf("rate 0.95 is valid, got %v", err)
	}
}

func TestSynthesizeRejectsUnknownExtension(t *testing.T) {
	opts := baseOptions(filepath.Join(t.TempDir(), "speech.flac"))
	_, err := Synthesize(context.Background(), &fakeSynthesizer{}, "مرحبا", opts)
	if err == nil || !strings.Contains(err.Error(), "unsupported output extension") {
		t.Errorf("expected an unsupported-extension error, got %v", err)
	}
}

func TestSynthesizeRejectsEmptyInput(t *testing.T) {
	opts := baseOptions(filepath.Join(t.TempDir(), "x.wav"))
	if _, err := Synthesize(context.Background(), &fakeSynthesizer{}, "   ", opts); err == nil {
		t.Error("expected empty input to be rejected")
	}
}

func TestConcurrencyIsLowerForChirp3(t *testing.T) {
	chirp := Options{Voice: "ar-XA-Chirp3-HD-Kore"}
	wavenet := Options{Voice: "ar-XA-Wavenet-B"}
	if chirp.concurrency() >= wavenet.concurrency() {
		t.Errorf("Chirp 3 has a tighter quota and should use fewer workers: %d vs %d",
			chirp.concurrency(), wavenet.concurrency())
	}
	explicit := Options{Voice: "ar-XA-Chirp3-HD-Kore", Concurrency: 3}
	if explicit.concurrency() != 3 {
		t.Errorf("an explicit concurrency should win, got %d", explicit.concurrency())
	}
}

func TestListVoicesSortsAndFilters(t *testing.T) {
	fake := &fakeSynthesizer{voices: []*texttospeechpb.Voice{
		{Name: "ar-XA-Standard-A", SsmlGender: texttospeechpb.SsmlVoiceGender_FEMALE},
		{Name: "ar-XA-Chirp3-HD-Puck", SsmlGender: texttospeechpb.SsmlVoiceGender_MALE},
		{Name: "ar-XA-Wavenet-B", SsmlGender: texttospeechpb.SsmlVoiceGender_MALE},
		{Name: "ar-XA-Chirp3-HD-Kore", SsmlGender: texttospeechpb.SsmlVoiceGender_FEMALE},
	}}
	all, err := ListVoices(context.Background(), fake, "ar-XA", "")
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Name != "ar-XA-Chirp3-HD-Kore" || all[1].Name != "ar-XA-Chirp3-HD-Puck" {
		t.Errorf("Chirp 3 voices should sort first and alphabetically, got %v", []VoiceInfo{all[0], all[1]})
	}
	if all[0].Tier != "Chirp3-HD" || all[0].Gender != "Female" {
		t.Errorf("unexpected voice metadata: %+v", all[0])
	}

	filtered, err := ListVoices(context.Background(), fake, "ar-XA", "wavenet")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "ar-XA-Wavenet-B" {
		t.Errorf("case-insensitive filter failed: %+v", filtered)
	}
}

func fastRetries(t *testing.T) {
	t.Helper()
	saved := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = saved })
}

func TestSynthesizeRetriesQuotaErrors(t *testing.T) {
	fastRetries(t)
	fake := &fakeSynthesizer{transient: []error{
		status.Error(codes.ResourceExhausted, "Resource has been exhausted (e.g. check quota)."),
		status.Error(codes.Unavailable, "try again"),
	}}
	out := filepath.Join(t.TempDir(), "retry.mp3")
	if _, err := Synthesize(context.Background(), fake, "مرحبا بك", baseOptions(out)); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if fake.calls != 3 || len(fake.requests) != 1 {
		t.Errorf("calls = %d, successful requests = %d", fake.calls, len(fake.requests))
	}
}

func TestSynthesizeGivesUpAfterMaxAttempts(t *testing.T) {
	fastRetries(t)
	var quota []error
	for range maxAttempts + 3 {
		quota = append(quota, status.Error(codes.ResourceExhausted, "quota"))
	}
	fake := &fakeSynthesizer{transient: quota}
	_, err := Synthesize(context.Background(), fake, "مرحبا بك", baseOptions(filepath.Join(t.TempDir(), "x.mp3")))
	if status.Code(errors.Unwrap(err)) != codes.ResourceExhausted || !strings.Contains(err.Error(), "--concurrency") {
		t.Fatalf("err = %v", err)
	}
	if fake.calls != maxAttempts {
		t.Errorf("calls = %d, want %d", fake.calls, maxAttempts)
	}
}

func TestSynthesizeDoesNotRetryPermanentErrors(t *testing.T) {
	fastRetries(t)
	fake := &fakeSynthesizer{err: status.Error(codes.PermissionDenied, "API not enabled")}
	_, err := Synthesize(context.Background(), fake, "مرحبا بك", baseOptions(filepath.Join(t.TempDir(), "x.mp3")))
	if status.Code(err) != codes.PermissionDenied || fake.calls != 1 {
		t.Fatalf("err = %v after %d calls", err, fake.calls)
	}
}
