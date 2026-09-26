package gcp

import (
	"context"
	"fmt"

	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"

	"github.com/scuba-plaza/arabic-tts/config"
)

type Recognizer interface {
	Recognize(ctx context.Context, req *speechpb.RecognizeRequest, opts ...gax.CallOption) (*speechpb.RecognizeResponse, error)
}

type Synthesizer interface {
	SynthesizeSpeech(ctx context.Context, req *texttospeechpb.SynthesizeSpeechRequest, opts ...gax.CallOption) (*texttospeechpb.SynthesizeSpeechResponse, error)
	ListVoices(ctx context.Context, req *texttospeechpb.ListVoicesRequest, opts ...gax.CallOption) (*texttospeechpb.ListVoicesResponse, error)
}

func NewSpeechClient(ctx context.Context, creds config.Credentials, region string) (*speech.Client, error) {
	c, err := speech.NewClient(ctx,
		option.WithCredentialsFile(creds.Path),
		option.WithEndpoint(config.SpeechEndpoint(region)),
	)
	if err != nil {
		return nil, fmt.Errorf("speech client for region %s: %w", region, err)
	}
	return c, nil
}

func NewTextToSpeechClient(ctx context.Context, creds config.Credentials) (*texttospeech.Client, error) {
	c, err := texttospeech.NewClient(ctx, option.WithCredentialsFile(creds.Path))
	if err != nil {
		return nil, fmt.Errorf("text-to-speech client: %w", err)
	}
	return c, nil
}
