package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakePrivateKey = "-----BEGIN PRIVATE KEY-----\nSUPERSECRETKEYMATERIAL\n-----END PRIVATE KEY-----\n"

func writeKey(t *testing.T, dir, name, kind, project string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := `{"type":"` + kind + `","project_id":"` + project +
		`","client_email":"svc@` + project + `.iam.gserviceaccount.com","private_key":"` +
		strings.ReplaceAll(fakePrivateKey, "\n", `\n`) + `"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveExplicitPathWins(t *testing.T) {
	dir := t.TempDir()
	path := writeKey(t, dir, "key.json", "service_account", "explicit-project")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeKey(t, dir, "env.json", "service_account", "env-project"))

	creds, err := ResolveCredentials(path)
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.ProjectID != "explicit-project" {
		t.Errorf("project is %q, want explicit-project", creds.ProjectID)
	}
	if creds.ClientEmail == "" {
		t.Error("client email should be reported")
	}
}

func TestResolveFallsBackToEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeKey(t, dir, "env.json", "service_account", "env-project"))
	creds, err := ResolveCredentials("")
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.ProjectID != "env-project" {
		t.Errorf("project is %q, want env-project", creds.ProjectID)
	}
}

func TestResolveDiscoversKeyInEnvDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Chdir(dir)
	if err := os.Mkdir(DefaultCredentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeKey(t, DefaultCredentDir, "not-a-key.json", "authorized_user", "ignored")
	writeKey(t, DefaultCredentDir, "service.json", "service_account", "discovered-project")

	creds, err := ResolveCredentials("")
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.ProjectID != "discovered-project" {
		t.Errorf("project is %q, want discovered-project", creds.ProjectID)
	}
	if !filepath.IsAbs(creds.Path) {
		t.Errorf("credential path %q should be absolute", creds.Path)
	}
}

func TestResolveReportsMissingCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Chdir(t.TempDir())
	_, err := ResolveCredentials("")
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

func TestErrorsNeverLeakKeyMaterial(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"not json":        "this is not json at all",
		"wrong type":      `{"type":"authorized_user","private_key":"` + strings.ReplaceAll(fakePrivateKey, "\n", `\n`) + `"}`,
		"missing project": `{"type":"service_account","private_key":"` + strings.ReplaceAll(fakePrivateKey, "\n", `\n`) + `"}`,
	}
	for name, body := range cases {
		path := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ResolveCredentials(path)
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if strings.Contains(err.Error(), "SUPERSECRETKEYMATERIAL") || strings.Contains(err.Error(), "BEGIN PRIVATE KEY") {
			t.Errorf("%s: error leaked key material: %v", name, err)
		}
	}
}

func TestSpeechEndpointAndRecognizerPathAgree(t *testing.T) {
	if got := SpeechEndpoint("eu"); got != "eu-speech.googleapis.com:443" {
		t.Errorf("SpeechEndpoint(eu) = %q", got)
	}
	if got := RecognizerPath("proj", "eu"); got != "projects/proj/locations/eu/recognizers/_" {
		t.Errorf("RecognizerPath = %q", got)
	}
}
