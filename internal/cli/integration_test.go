//go:build integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"arabic-tts/internal/arabic"
	"arabic-tts/internal/config"
	"arabic-tts/internal/gcp"
	"arabic-tts/internal/stt"
	"arabic-tts/internal/tts"
)

func requireLive(t *testing.T) config.Credentials {
	t.Helper()
	if os.Getenv("ARABIC_TTS_INTEGRATION") != "1" {
		t.Skip("set ARABIC_TTS_INTEGRATION=1 to run billable live tests")
	}

	t.Chdir(moduleRoot(t))
	creds, err := config.ResolveCredentials("")
	if err != nil {
		t.Skipf("no credentials: %v", err)
	}
	return creds
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the module root")
		}
		dir = parent
	}
}

func TestLiveRoundTrip(t *testing.T) {
	creds := requireLive(t)
	ctx := context.Background()
	const phrase = "السلام عليكم ورحمة الله وبركاته هذا اختبار للنظام"

	ttsClient, err := gcp.NewTextToSpeechClient(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer ttsClient.Close()

	out := filepath.Join(t.TempDir(), "roundtrip.wav")
	if _, err := tts.Synthesize(ctx, ttsClient, phrase, tts.Options{
		Voice:    config.DefaultVoice,
		Language: config.DefaultLanguage,
		Output:   out,
	}); err != nil {
		t.Fatalf("synthesis: %v", err)
	}

	sttClient, err := gcp.NewSpeechClient(ctx, creds, config.DefaultRegion)
	if err != nil {
		t.Fatal(err)
	}
	defer sttClient.Close()

	transcript, err := stt.TranscribeFile(ctx, sttClient, out, stt.Options{
		Project:     creds.ProjectID,
		Region:      config.DefaultRegion,
		Language:    config.DefaultLanguage,
		Model:       config.DefaultSTTModel,
		Punctuation: true,
	})
	if err != nil {
		t.Fatalf("transcription: %v", err)
	}

	got := arabic.Normalize(transcript.Text())
	want := arabic.Normalize(phrase)
	if got != want {
		t.Errorf("round trip did not survive\n got: %s\nwant: %s", got, want)
	}
}

func TestLiveVoicesIncludeChirp3(t *testing.T) {
	creds := requireLive(t)
	ctx := context.Background()
	client, err := gcp.NewTextToSpeechClient(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	voices, err := tts.ListVoices(ctx, client, config.DefaultLanguage, "Chirp3")
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) == 0 {
		t.Error("expected ar-XA Chirp 3 HD voices to be available")
	}
}
