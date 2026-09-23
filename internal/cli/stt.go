package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"arabic-tts/internal/audio"
	"arabic-tts/internal/config"
	"arabic-tts/internal/gcp"
	"arabic-tts/internal/stt"
)

func newSTTCommand() *cobra.Command {
	var (
		output      string
		format      string
		language    string
		model       string
		segmentMax  time.Duration
		concurrency int
		noiseDB     float64
		minSilence  float64
		noPunct     bool
		normalize   bool
		bom         bool
		noRTL       bool
	)

	cmd := &cobra.Command{
		Use:   "stt <audio-file>",
		Short: "Transcribe an Arabic audio file",
		Long: "Transcribe an Arabic audio file of any format ffmpeg can decode.\n\n" +
			"Audio under a minute goes out as a single request. Longer audio is decoded\n" +
			"to 16 kHz mono, split at silence boundaries, and recognised concurrently,\n" +
			"which finishes far faster than real time and needs no Cloud Storage bucket.",
		Example: "  arabic-tts stt audio/interview.m4a\n" +
			"  arabic-tts stt audio/interview.m4a --format srt -o out/interview.srt\n" +
			"  arabic-tts stt audio/lecture.wav --lang ar-EG --format json --concurrency 8",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			if _, err := os.Stat(source); err != nil {
				return usagef("cannot read %s: %v", source, err)
			}
			if err := audio.Available(); err != nil {
				return usagef("%v; ffmpeg is required to decode audio", err)
			}

			wire, err := stt.ParseFormat(format)
			if err != nil {
				return usageError{err}
			}

			if !cmd.Flags().Changed("segment-max") && wire.IsSubtitle() {
				segmentMax = config.MaxSegmentSub
			}

			creds, err := resolve()
			if err != nil {
				return err
			}

			opts := stt.Options{
				Project:     creds.ProjectID,
				Region:      g.region,
				Language:    language,
				Model:       model,
				SegmentMax:  segmentMax,
				Concurrency: concurrency,
				NoiseDB:     noiseDB,
				MinSilence:  minSilence,
				Punctuation: !noPunct,
				Progress: func(done, total int) {
					progressf("\rtranscribing segment %d/%d", done, total)
				},
				PlanNotice: func(segments []stt.Segment) {
					hard := 0
					for _, s := range segments {
						if s.HardCut {
							hard++
						}
					}
					progressf("plan: %d segment(s), %d forced mid-speech, max %s\n",
						len(segments), hard, segmentMax)
					for _, s := range segments {
						marker := ""
						if s.HardCut {
							marker = " (forced)"
						}
						progressf("  %2d  %7.2fs -> %7.2fs  %5.2fs%s\n",
							s.Index+1, s.Start, s.End, s.Duration(), marker)
					}
				},
			}

			ctx := cmd.Context()
			client, err := gcp.NewSpeechClient(ctx, creds, g.region)
			if err != nil {
				return err
			}
			defer client.Close()

			transcript, err := stt.TranscribeFile(ctx, client, source, opts)
			if err != nil {
				return err
			}
			progressf("\n")

			w := cmd.OutOrStdout()
			if output != "" {
				if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
					return fmt.Errorf("creating output directory: %w", err)
				}
				f, err := os.Create(output)
				if err != nil {
					return fmt.Errorf("creating %s: %w", output, err)
				}
				defer f.Close()
				w = f
			}

			if err := transcript.Write(w, stt.WriteOptions{
				Format:    wire,
				BOM:       bom,
				RTLMark:   wire.IsSubtitle() && !noRTL,
				Normalize: normalize,
			}); err != nil {
				return err
			}
			if output != "" {
				infof("wrote %s (%d segment(s), %.1fs of audio)\n",
					output, len(transcript.Segments), transcript.Duration)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&output, "out", "o", "", "write the transcript to a file instead of stdout")
	f.StringVar(&format, "format", string(stt.FormatText), "output format: txt, json, srt or vtt")
	f.StringVar(&language, "lang", config.DefaultLanguage, "language code, such as ar-XA, ar-EG or ar-SA")
	f.StringVar(&model, "model", config.DefaultSTTModel, "recognition model")
	f.DurationVar(&segmentMax, "segment-max", config.MaxSegment, "longest segment per request (defaults to 15s for subtitles)")
	f.IntVar(&concurrency, "concurrency", 0, "parallel recognition requests (default 6)")
	f.Float64Var(&noiseDB, "silence-threshold", 0, "silence detection threshold in dB (default -35)")
	f.Float64Var(&minSilence, "silence-duration", 0, "shortest silence treated as a cut point, in seconds (default 0.4)")
	f.BoolVar(&noPunct, "no-punctuation", false, "disable automatic punctuation")
	f.BoolVar(&normalize, "normalize", false, "fold Arabic orthographic variants and drop diacritics")
	f.BoolVar(&bom, "bom", false, "prefix the output with a UTF-8 byte order mark")
	f.BoolVar(&noRTL, "no-rtl-mark", false, "omit the right-to-left mark from subtitle cues")
	return cmd
}
