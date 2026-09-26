package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/scuba-plaza/arabic-tts/arabic"
	"github.com/scuba-plaza/arabic-tts/config"
	"github.com/scuba-plaza/arabic-tts/gcp"
	"github.com/scuba-plaza/arabic-tts/tts"
)

func newTTSCommand() *cobra.Command {
	var (
		inputFile   string
		output      string
		voice       string
		language    string
		rate        float64
		pitch       float64
		volume      float64
		sampleRate  int
		gap         time.Duration
		concurrency int
		useSSML     bool
		useMarkup   bool
	)

	cmd := &cobra.Command{
		Use:   "tts [text]",
		Short: "Synthesize Arabic speech from text",
		Long: "Synthesize Arabic speech from a positional argument, a file, or standard input.\n\n" +
			"Text-to-Speech accepts 5000 bytes per request, which is roughly 2500 Arabic\n" +
			"characters. Longer input is split on sentence boundaries, synthesized\n" +
			"concurrently, and stitched into a single file.",
		Example: "  arabic-tts tts \"مرحبا بك\"\n" +
			"  arabic-tts tts --file script.txt -o out/episode.mp3 --voice ar-XA-Chirp3-HD-Kore\n" +
			"  cat script.txt | arabic-tts tts -o out/episode.wav --rate 0.95",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := readText(cmd, args, inputFile)
			if err != nil {
				return err
			}
			if strings.TrimSpace(text) == "" {
				return usagef("no text given; pass it as an argument, with --file, or on stdin")
			}
			if useSSML && useMarkup {
				return usagef("--ssml and --markup are mutually exclusive")
			}
			if !arabic.HasArabic(text) {
				infof("warning: the input contains no Arabic letters\n")
			}

			if output == "" {
				output = filepath.Join(config.DefaultOutputDir,
					fmt.Sprintf("tts-%s.wav", time.Now().Format("20060102-150405")))
			}

			mode := tts.ModeText
			switch {
			case useSSML:
				mode = tts.ModeSSML
			case useMarkup:
				mode = tts.ModeMarkup
			}

			opts := tts.Options{
				Voice:        voice,
				Language:     language,
				Mode:         mode,
				SpeakingRate: rate,
				Pitch:        pitch,
				PitchSet:     cmd.Flags().Changed("pitch"),
				VolumeGainDB: volume,
				SampleRate:   sampleRate,
				Gap:          gap,
				Concurrency:  concurrency,
				Output:       output,
			}
			if err := opts.Validate(); err != nil {
				return usageError{err}
			}
			opts.Progress = func(done, total int) {
				if total > 1 {
					progressf("\rsynthesizing chunk %d/%d", done, total)
				}
			}

			creds, err := resolve()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := gcp.NewTextToSpeechClient(ctx, creds)
			if err != nil {
				return err
			}
			defer client.Close()

			res, err := tts.Synthesize(ctx, client, text, opts)
			if err != nil {
				return err
			}
			progressf("\n")
			if res.Duration > 0 {
				infof("wrote %s (%s, %d chunk(s), %.1f KB)\n",
					res.Path, res.Duration.Round(time.Millisecond), res.Chunks, float64(res.Bytes)/1024)
			} else {
				infof("wrote %s (%d chunk(s), %.1f KB)\n", res.Path, res.Chunks, float64(res.Bytes)/1024)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&inputFile, "file", "f", "", "read the text from a file instead of the arguments")
	f.StringVarP(&output, "out", "o", "", "output file; extension picks the format (.wav, .mp3, .ogg, .m4a)")
	f.StringVar(&voice, "voice", config.DefaultVoice, "voice name; see 'arabic-tts voices'")
	f.StringVar(&language, "lang", config.DefaultLanguage, "language code")
	f.Float64Var(&rate, "rate", 0, "speaking rate between 0.25 and 2.0")
	f.Float64Var(&pitch, "pitch", 0, "pitch shift in semitones; not supported by Chirp 3 HD voices")
	f.Float64Var(&volume, "volume", 0, "volume gain in dB")
	f.IntVar(&sampleRate, "sample-rate", 0, "output sample rate in Hz (default 24000)")
	f.DurationVar(&gap, "gap", 150*time.Millisecond, "silence inserted between stitched chunks")
	f.IntVar(&concurrency, "concurrency", 0, "parallel synthesis requests (default 4 for Chirp 3, else 8)")
	f.BoolVar(&useSSML, "ssml", false, "treat the input as SSML")
	f.BoolVar(&useMarkup, "markup", false, "treat the input as markup, enabling [pause] tags")
	return cmd
}

func readText(cmd *cobra.Command, args []string, file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", file, err)
		}
		return string(b), nil
	}
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice == 0 {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(b), nil
	}
	return "", nil
}
