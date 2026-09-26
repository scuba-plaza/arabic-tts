package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/scuba-plaza/arabic-tts/config"
	"github.com/scuba-plaza/arabic-tts/gcp"
	"github.com/scuba-plaza/arabic-tts/tts"
)

func newVoicesCommand() *cobra.Command {
	var (
		language string
		filter   string
		asJSON   bool
	)

	cmd := &cobra.Command{
		Use:     "voices",
		Short:   "List the available Arabic voices",
		Example: "  arabic-tts voices\n  arabic-tts voices --filter Chirp3\n  arabic-tts voices --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			voices, err := tts.ListVoices(ctx, client, language, filter)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(voices)
			}
			if len(voices) == 0 {
				fmt.Fprintf(out, "no voices matched %q for %s\n", filter, language)
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "VOICE\tTIER\tGENDER\tHZ")
			for _, v := range voices {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", v.Name, v.Tier, v.Gender, v.SampleRate)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			infof("\n%d voice(s)\n", len(voices))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&language, "lang", config.DefaultLanguage, "language code to list voices for")
	f.StringVar(&filter, "filter", "", "case-insensitive substring of the voice name")
	f.BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}
