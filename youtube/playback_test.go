package youtube

import (
	"encoding/json"
	"testing"
)

func TestParseLoudnessPrefersPerceptualWhenIntegratedLoudnessIsZero(t *testing.T) {
	raw := json.RawMessage(`{"playerConfig":{"audioConfig":{"loudnessDb":0,"perceptualLoudnessDb":-12.5}}}`)
	got, err := parseLoudness(raw)
	if err != nil || got.LoudnessDB != -12.5 {
		t.Fatalf("parseLoudness() = %#v, %v; want -12.5, nil", got, err)
	}
}

func TestParseLoudnessRejectsUnavailableOrMalformedResponses(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"playabilityStatus":{"status":"UNPLAYABLE"}}`),
		json.RawMessage(`{"playerConfig":{"audioConfig":{}}}`),
		json.RawMessage(`{"streamingData":{"formats":[{"audioTrack":{"loudnessDb":9}}]}}`),
		json.RawMessage(`not json`),
	} {
		if got, err := parseLoudness(raw); err == nil || got != (Loudness{}) {
			t.Errorf("parseLoudness(%s) = %#v, %v; want empty and an error", raw, got, err)
		}
	}
}
