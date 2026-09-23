package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/speech/apiv2/speechpb"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/spf13/cobra"

	"arabic-tts/internal/audio"
	"arabic-tts/internal/config"
	"arabic-tts/internal/gcp"
)

type check struct {
	name   string
	detail string
	err    error
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check credentials, APIs and local tooling",
		Long: "Run every precondition this program depends on and report what is wrong.\n\n" +
			"The API checks are read-only: listing voices is free, and recognition is\n" +
			"sent one second of silence.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			checks := []check{
				checkFFmpeg(ctx),
				checkOutputDir(),
			}

			creds, credErr := resolve()
			if credErr != nil {
				checks = append(checks, check{name: "credentials", err: credErr})
			} else {
				checks = append(checks, check{
					name:   "credentials",
					detail: fmt.Sprintf("%s\n      project %s\n      identity %s", creds.Path, creds.ProjectID, creds.ClientEmail),
				})
				checks = append(checks, checkTTS(ctx, creds), checkSTT(ctx, creds))
			}

			failed := 0
			for _, c := range checks {
				if c.err != nil {
					failed++
					fmt.Fprintf(out, "FAIL  %s\n      %s\n", c.name, indent(explain(c.err)))
					continue
				}
				fmt.Fprintf(out, "ok    %s\n", c.name)
				if c.detail != "" {
					fmt.Fprintf(out, "      %s\n", c.detail)
				}
			}

			fmt.Fprintf(out, "\nBoth Chirp 3 tiers are premium; check current pricing before long runs.\n")
			if failed > 0 {
				return fmt.Errorf("%d of %d checks failed", failed, len(checks))
			}
			return nil
		},
	}
}

func indent(s string) string {
	return strings.Join(strings.Split(s, "\n"), "\n      ")
}

func checkFFmpeg(ctx context.Context) check {
	if err := audio.Available(); err != nil {
		return check{name: "ffmpeg", err: err}
	}
	version, err := audio.Version(ctx, "ffmpeg")
	if err != nil {
		return check{name: "ffmpeg", err: err}
	}
	return check{name: "ffmpeg", detail: version}
}

func checkOutputDir() check {
	if err := os.MkdirAll(config.DefaultOutputDir, 0o755); err != nil {
		return check{name: "output directory", err: err}
	}
	probe := filepath.Join(config.DefaultOutputDir, ".write-probe")
	if err := os.WriteFile(probe, []byte(""), 0o644); err != nil {
		return check{name: "output directory", err: err}
	}
	os.Remove(probe)
	return check{name: "output directory", detail: config.DefaultOutputDir + " is writable"}
}

func checkTTS(ctx context.Context, creds config.Credentials) check {
	client, err := gcp.NewTextToSpeechClient(ctx, creds)
	if err != nil {
		return check{name: "text-to-speech", err: err}
	}
	defer client.Close()
	resp, err := client.ListVoices(ctx, &texttospeechpb.ListVoicesRequest{LanguageCode: config.DefaultLanguage})
	if err != nil {
		return check{name: "text-to-speech", err: err}
	}
	return check{
		name:   "text-to-speech",
		detail: fmt.Sprintf("%d %s voices available", len(resp.Voices), config.DefaultLanguage),
	}
}

func checkSTT(ctx context.Context, creds config.Credentials) check {
	client, err := gcp.NewSpeechClient(ctx, creds, g.region)
	if err != nil {
		return check{name: "speech-to-text", err: err}
	}
	defer client.Close()

	silence := audio.Silence(config.SampleRateSTT, 1, 1.0)
	_, err = client.Recognize(ctx, &speechpb.RecognizeRequest{
		Recognizer: config.RecognizerPath(creds.ProjectID, g.region),
		Config: &speechpb.RecognitionConfig{
			Model:         config.DefaultSTTModel,
			LanguageCodes: []string{config.DefaultLanguage},
			DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{
				ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
					Encoding:          speechpb.ExplicitDecodingConfig_LINEAR16,
					SampleRateHertz:   config.SampleRateSTT,
					AudioChannelCount: 1,
				},
			},
		},
		AudioSource: &speechpb.RecognizeRequest_Content{Content: silence},
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return check{name: "speech-to-text", err: err}
	}
	return check{
		name:   "speech-to-text",
		detail: fmt.Sprintf("%s reachable, model %s, language %s", config.SpeechEndpoint(g.region), config.DefaultSTTModel, config.DefaultLanguage),
	}
}
