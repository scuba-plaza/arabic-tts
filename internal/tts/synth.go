package tts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"golang.org/x/sync/errgroup"

	"arabic-tts/internal/audio"
	"arabic-tts/internal/config"
	"arabic-tts/internal/gcp"
)

type InputMode int

const (
	ModeText InputMode = iota

	ModeSSML

	ModeMarkup
)

type Options struct {
	Voice        string
	Language     string
	Mode         InputMode
	SpeakingRate float64
	Pitch        float64
	PitchSet     bool
	VolumeGainDB float64
	SampleRate   int
	Gap          time.Duration
	Concurrency  int
	Output       string
	Progress     func(done, total int)
}

type Result struct {
	Path     string
	Chunks   int
	Bytes    int
	Duration time.Duration
	Encoded  bool
}

const (
	chirpConcurrency   = 4
	defaultConcurrency = 8
)

func IsChirp3(voice string) bool {
	return strings.Contains(strings.ToLower(voice), "chirp3")
}

func (o *Options) Validate() error {
	if o.SpeakingRate != 0 && (o.SpeakingRate < 0.25 || o.SpeakingRate > 2.0) {
		return fmt.Errorf("speaking rate %.2f is out of range; must be between 0.25 and 2.0", o.SpeakingRate)
	}
	if o.PitchSet && IsChirp3(o.Voice) {
		return fmt.Errorf("voice %s is a Chirp 3 HD voice and does not support pitch; drop --pitch or pick an ar-XA-Wavenet/Standard voice", o.Voice)
	}
	if o.Output != "" {
		if _, known := encodingFor(o.Output); !known {
			return fmt.Errorf("unsupported output extension %q; use .wav, .mp3, .ogg or .m4a", filepath.Ext(o.Output))
		}
	}
	return nil
}

func (o *Options) concurrency() int {
	if o.Concurrency > 0 {
		return o.Concurrency
	}
	if IsChirp3(o.Voice) {
		return chirpConcurrency
	}
	return defaultConcurrency
}

func (o *Options) sampleRate() int {
	if o.SampleRate > 0 {
		return o.SampleRate
	}
	return config.SampleRateTTS
}

func encodingFor(path string) (texttospeechpb.AudioEncoding, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return texttospeechpb.AudioEncoding_LINEAR16, true
	case ".mp3":
		return texttospeechpb.AudioEncoding_MP3, true
	case ".ogg", ".opus":
		return texttospeechpb.AudioEncoding_OGG_OPUS, true
	case ".m4a":
		return texttospeechpb.AudioEncoding_M4A, true
	}
	return texttospeechpb.AudioEncoding_LINEAR16, false
}

func (o *Options) input(text string) *texttospeechpb.SynthesisInput {
	switch o.Mode {
	case ModeSSML:
		return &texttospeechpb.SynthesisInput{InputSource: &texttospeechpb.SynthesisInput_Ssml{Ssml: text}}
	case ModeMarkup:
		return &texttospeechpb.SynthesisInput{InputSource: &texttospeechpb.SynthesisInput_Markup{Markup: text}}
	default:
		return &texttospeechpb.SynthesisInput{InputSource: &texttospeechpb.SynthesisInput_Text{Text: text}}
	}
}

func (o *Options) request(text string, enc texttospeechpb.AudioEncoding) *texttospeechpb.SynthesizeSpeechRequest {
	cfg := &texttospeechpb.AudioConfig{
		AudioEncoding:   enc,
		SampleRateHertz: int32(o.sampleRate()),
	}
	if o.SpeakingRate != 0 {
		cfg.SpeakingRate = o.SpeakingRate
	}
	if o.PitchSet {
		cfg.Pitch = o.Pitch
	}
	if o.VolumeGainDB != 0 {
		cfg.VolumeGainDb = o.VolumeGainDB
	}
	return &texttospeechpb.SynthesizeSpeechRequest{
		Input:       o.input(text),
		Voice:       &texttospeechpb.VoiceSelectionParams{LanguageCode: o.Language, Name: o.Voice},
		AudioConfig: cfg,
	}
}

func Synthesize(ctx context.Context, client gcp.Synthesizer, text string, opts Options) (Result, error) {
	if err := opts.Validate(); err != nil {
		return Result{}, err
	}
	chunks := Split(text, config.MaxSynthesisBytes)
	if len(chunks) == 0 {
		return Result{}, fmt.Errorf("nothing to synthesize: input is empty")
	}

	targetEnc, known := encodingFor(opts.Output)
	if !known {
		return Result{}, fmt.Errorf("unsupported output extension %q; use .wav, .mp3, .ogg or .m4a", filepath.Ext(opts.Output))
	}

	if err := os.MkdirAll(filepath.Dir(opts.Output), 0o755); err != nil {
		return Result{}, fmt.Errorf("creating output directory: %w", err)
	}

	if len(chunks) == 1 {
		resp, err := client.SynthesizeSpeech(ctx, opts.request(chunks[0], targetEnc))
		if err != nil {
			return Result{}, err
		}
		if opts.Progress != nil {
			opts.Progress(1, 1)
		}
		if err := os.WriteFile(opts.Output, resp.AudioContent, 0o644); err != nil {
			return Result{}, fmt.Errorf("writing %s: %w", opts.Output, err)
		}
		dur := time.Duration(0)
		if targetEnc == texttospeechpb.AudioEncoding_LINEAR16 {
			if pcm, err := audio.StripWAVHeader(resp.AudioContent); err == nil {
				dur = pcmDuration(len(pcm), opts.sampleRate())
			}
		}
		return Result{Path: opts.Output, Chunks: 1, Bytes: len(resp.AudioContent), Duration: dur}, nil
	}

	parts := make([][]byte, len(chunks))
	var done int
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.concurrency())
	for i, chunk := range chunks {
		i, chunk := i, chunk
		g.Go(func() error {
			resp, err := client.SynthesizeSpeech(gctx, opts.request(chunk, texttospeechpb.AudioEncoding_LINEAR16))
			if err != nil {
				return fmt.Errorf("chunk %d of %d: %w", i+1, len(chunks), err)
			}
			pcm, err := audio.StripWAVHeader(resp.AudioContent)
			if err != nil {
				return fmt.Errorf("chunk %d of %d: %w", i+1, len(chunks), err)
			}
			parts[i] = pcm
			mu.Lock()
			done++
			if opts.Progress != nil {
				opts.Progress(done, len(chunks))
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Result{}, err
	}

	gap := audio.Silence(opts.sampleRate(), 1, opts.Gap.Seconds())
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	total += gap2Len(gap, len(parts))

	stitched := make([]byte, 0, total)
	for i, p := range parts {
		if i > 0 {
			stitched = append(stitched, gap...)
		}
		stitched = append(stitched, p...)
	}

	wavPath := opts.Output
	encoded := false
	if targetEnc != texttospeechpb.AudioEncoding_LINEAR16 {
		wavPath = opts.Output + ".stitch.wav"
		encoded = true
	}

	if err := writeWAVFile(wavPath, stitched, opts.sampleRate()); err != nil {
		return Result{}, err
	}
	if encoded {
		defer os.Remove(wavPath)
		if err := audio.Encode(ctx, wavPath, opts.Output); err != nil {
			return Result{}, err
		}
	}

	size := len(stitched) + 44
	if encoded {
		if st, err := os.Stat(opts.Output); err == nil {
			size = int(st.Size())
		}
	}
	return Result{
		Path:     opts.Output,
		Chunks:   len(chunks),
		Bytes:    size,
		Duration: pcmDuration(len(stitched), opts.sampleRate()),
		Encoded:  encoded,
	}, nil
}

func gap2Len(gap []byte, parts int) int {
	if parts < 2 {
		return 0
	}
	return len(gap) * (parts - 1)
}

func writeWAVFile(path string, pcm []byte, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()
	if err := audio.WriteWAV(f, pcm, sampleRate, 1); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func pcmDuration(n, sampleRate int) time.Duration {
	if sampleRate == 0 {
		return 0
	}
	return time.Duration(audio.PCMDuration(int64(n), sampleRate) * float64(time.Second))
}
