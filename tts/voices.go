package tts

import (
	"context"
	"sort"
	"strings"

	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"

	"github.com/scuba-plaza/arabic-tts/gcp"
)

type VoiceInfo struct {
	Name       string   `json:"name"`
	Tier       string   `json:"tier"`
	Gender     string   `json:"gender"`
	SampleRate int32    `json:"natural_sample_rate_hertz"`
	Languages  []string `json:"language_codes"`
}

var tierRank = map[string]int{"Chirp3-HD": 0, "Neural2": 1, "Wavenet": 2, "Studio": 3, "Standard": 4}

func Tier(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "chirp3-hd"):
		return "Chirp3-HD"
	case strings.Contains(lower, "chirp"):
		return "Chirp"
	case strings.Contains(lower, "neural2"):
		return "Neural2"
	case strings.Contains(lower, "wavenet"):
		return "Wavenet"
	case strings.Contains(lower, "studio"):
		return "Studio"
	case strings.Contains(lower, "standard"):
		return "Standard"
	}
	return "Other"
}

func gender(g texttospeechpb.SsmlVoiceGender) string {
	switch g {
	case texttospeechpb.SsmlVoiceGender_MALE:
		return "Male"
	case texttospeechpb.SsmlVoiceGender_FEMALE:
		return "Female"
	case texttospeechpb.SsmlVoiceGender_NEUTRAL:
		return "Neutral"
	}
	return "Unspecified"
}

func ListVoices(ctx context.Context, client gcp.Synthesizer, language, filter string) ([]VoiceInfo, error) {
	resp, err := client.ListVoices(ctx, &texttospeechpb.ListVoicesRequest{LanguageCode: language})
	if err != nil {
		return nil, err
	}
	out := make([]VoiceInfo, 0, len(resp.Voices))
	needle := strings.ToLower(filter)
	for _, v := range resp.Voices {
		if needle != "" && !strings.Contains(strings.ToLower(v.Name), needle) {
			continue
		}
		out = append(out, VoiceInfo{
			Name:       v.Name,
			Tier:       Tier(v.Name),
			Gender:     gender(v.SsmlGender),
			SampleRate: v.NaturalSampleRateHertz,
			Languages:  v.LanguageCodes,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ri, oki := tierRank[out[i].Tier]
		rj, okj := tierRank[out[j].Tier]
		if !oki {
			ri = 99
		}
		if !okj {
			rj = 99
		}
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
