package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/scuba-plaza/arabic-tts/config"
)

const (
	ExitOK    = 0
	ExitUsage = 1
	ExitAPI   = 2
	ExitAudio = 3
)

type globals struct {
	credentials string
	project     string
	region      string
	verbose     bool
	quiet       bool
}

var g globals

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "arabic-tts",
		Short: "Arabic speech synthesis and transcription backed by Google Cloud",
		Long: "arabic-tts synthesises Arabic speech from text and transcribes Arabic audio files.\n\n" +
			"Speech uses Text-to-Speech v1 with ar-XA voices; transcription uses\n" +
			"Speech-to-Text v2 with the chirp_3 model. Files longer than a minute are\n" +
			"split locally on silence and recognised concurrently, so no Cloud Storage\n" +
			"bucket is needed.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&g.credentials, "credentials", "", "path to a service account JSON key (default: $GOOGLE_APPLICATION_CREDENTIALS, then .env/*.json)")
	pf.StringVar(&g.project, "project", "", "Google Cloud project ID (default: the credential's project)")
	pf.StringVar(&g.region, "region", config.DefaultRegion, "Speech-to-Text region; ar-XA is served from eu")
	pf.BoolVarP(&g.verbose, "verbose", "v", false, "report progress and the segment plan on stderr")
	pf.BoolVarP(&g.quiet, "quiet", "q", false, "suppress progress output")

	root.AddCommand(newTTSCommand(), newSTTCommand(), newVoicesCommand(), newDoctorCommand())
	return root
}

func Execute(ctx context.Context) int {
	root := newRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+explain(err))
		return exitCode(err)
	}
	return ExitOK
}

func resolve() (config.Credentials, error) {
	creds, err := config.ResolveCredentials(g.credentials)
	if err != nil {
		return config.Credentials{}, err
	}
	if g.project != "" {
		creds.ProjectID = g.project
	}
	return creds, nil
}

func progressf(format string, args ...any) {
	if g.quiet || !g.verbose {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}

func infof(format string, args ...any) {
	if g.quiet {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}
