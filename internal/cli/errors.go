package cli

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/scuba-plaza/arabic-tts/audio"
	"github.com/scuba-plaza/arabic-tts/config"
	"github.com/scuba-plaza/arabic-tts/stt"
)

type usageError struct{ error }

func usagef(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

func explain(err error) string {
	var toolErr *audio.ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Error()
	}

	st, ok := status.FromError(rootCause(err))
	if !ok {
		return err.Error()
	}

	switch st.Code() {
	case codes.PermissionDenied:
		return fmt.Sprintf("%v\n\n"+
			"The service account may lack access, or the API may not be enabled on the project.\n"+
			"Enable them in the Cloud Console, or run:\n"+
			"  gcloud services enable texttospeech.googleapis.com --project %s\n"+
			"  gcloud services enable speech.googleapis.com       --project %s",
			err, projectHint(), projectHint())
	case codes.Unauthenticated:
		return fmt.Sprintf("%v\n\nCredentials were rejected. Checked: %s", err, credentialHint())
	case codes.ResourceExhausted:
		return fmt.Sprintf("%v\n\nQuota exceeded. Lower --concurrency and retry.", err)
	case codes.InvalidArgument:
		msg := st.Message()
		if strings.Contains(strings.ToLower(msg), "language") || strings.Contains(strings.ToLower(msg), "model") {
			return fmt.Sprintf("%v\n\n"+
				"ar-XA is served only by the chirp_3 model in the eu region.\n"+
				"Check --lang, --model and --region agree with each other.", err)
		}
		return err.Error()
	case codes.NotFound:
		return fmt.Sprintf("%v\n\nCheck --project and --region; the recognizer path is built from both.", err)
	}
	return err.Error()
}

func rootCause(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
			return err
		}
		err = unwrapped
	}
}

func projectHint() string {
	if g.project != "" {
		return g.project
	}
	if creds, err := config.ResolveCredentials(g.credentials); err == nil {
		return creds.ProjectID
	}
	return "YOUR_PROJECT"
}

func credentialHint() string {
	if creds, err := config.ResolveCredentials(g.credentials); err == nil {
		return creds.Path
	}
	return "no credential file found"
}

func exitCode(err error) int {
	var usageErr usageError
	if errors.As(err, &usageErr) {
		return ExitUsage
	}
	var toolErr *audio.ToolError
	if errors.As(err, &toolErr) {
		return ExitAudio
	}
	if errors.Is(err, config.ErrNoCredentials) {
		return ExitUsage
	}
	if errors.Is(err, stt.ErrNoSpeech) {
		return ExitAPI
	}
	if _, ok := status.FromError(rootCause(err)); ok && status.Code(rootCause(err)) != codes.Unknown {
		return ExitAPI
	}
	return ExitAPI
}
