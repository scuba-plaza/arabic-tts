package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	DefaultRegion     = "eu"
	DefaultLanguage   = "ar-XA"
	DefaultSTTModel   = "chirp_3"
	DefaultVoice      = "ar-XA-Chirp3-HD-Kore"
	DefaultOutputDir  = "out"
	DefaultCredentDir = ".env"

	SampleRateSTT = 16000
	SampleRateTTS = 24000

	MaxSegment    = 55 * time.Second
	MaxSegmentSub = 15 * time.Second

	MaxSynthesisBytes = 4500
)

type Credentials struct {
	Path        string
	ProjectID   string
	ClientEmail string
}

var ErrNoCredentials = errors.New("no service account credentials found")

type serviceAccountMeta struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
}

func ResolveCredentials(explicit string) (Credentials, error) {
	if explicit != "" {
		return inspect(explicit)
	}
	if env := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); env != "" {
		return inspect(env)
	}
	if found, err := discover(DefaultCredentDir); err == nil {
		return found, nil
	}
	return Credentials{}, fmt.Errorf("%w: pass --credentials, set GOOGLE_APPLICATION_CREDENTIALS, or place a service account JSON in %s/", ErrNoCredentials, DefaultCredentDir)
}

func discover(dir string) (Credentials, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(matches) == 0 {
		return Credentials{}, ErrNoCredentials
	}
	sort.Strings(matches)
	for _, m := range matches {
		if c, err := inspect(m); err == nil {
			return c, nil
		}
	}
	return Credentials{}, ErrNoCredentials
}

func inspect(path string) (Credentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("reading credentials %s: %w", path, err)
	}
	var meta serviceAccountMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Credentials{}, fmt.Errorf("credentials %s are not valid JSON", path)
	}
	if meta.Type != "service_account" {
		return Credentials{}, fmt.Errorf("credentials %s are not a service account key", path)
	}
	if meta.ProjectID == "" {
		return Credentials{}, fmt.Errorf("credentials %s have no project_id", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return Credentials{Path: abs, ProjectID: meta.ProjectID, ClientEmail: meta.ClientEmail}, nil
}

func SpeechEndpoint(region string) string {
	return fmt.Sprintf("%s-speech.googleapis.com:443", region)
}

func RecognizerPath(project, region string) string {
	return fmt.Sprintf("projects/%s/locations/%s/recognizers/_", project, region)
}
