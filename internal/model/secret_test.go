package model_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestSecret_Redaction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  string // the redacted form
	}{
		{name: "empty", value: "", want: ""},
		{name: "eight characters", value: "abcd5678", want: "****"},
		{name: "twelve characters", value: "abcdefgh1234", want: "****1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := model.Secret(tt.value)

			if got := s.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got := fmt.Sprintf("%v", s); got != tt.want {
				t.Errorf("Sprintf(%%v) = %q, want %q", got, tt.want)
			}
			if got := fmt.Sprintf("%s", s); got != tt.want {
				t.Errorf("Sprintf(%%s) = %q, want %q", got, tt.want)
			}
			if got := fmt.Sprintf("%#v", s); got != tt.want {
				t.Errorf("Sprintf(%%#v) = %q, want %q", got, tt.want)
			}

			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			logger.Info("secret", "token", s)
			if tt.value != "" && strings.Contains(buf.String(), tt.value) {
				t.Errorf("slog output %q contains the secret", buf.String())
			}
			if want := "token=" + tt.want; tt.want != "" && !strings.Contains(buf.String(), want) {
				t.Errorf("slog output %q does not contain %q", buf.String(), want)
			}

			b, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if got, want := string(b), `"`+tt.want+`"`; got != want {
				t.Errorf("json.Marshal = %s, want %s", got, want)
			}

			if got := s.Reveal(); got != tt.value {
				t.Errorf("Reveal() = %q, want %q", got, tt.value)
			}
		})
	}
}

func TestSecret_RedactedInsideStruct(t *testing.T) {
	t.Parallel()
	type holder struct {
		Token model.Secret
	}
	const value = "fake-token-abcdefgh1234"
	h := holder{Token: model.Secret(value)}
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		if got := fmt.Sprintf(verb, h); strings.Contains(got, value) {
			t.Errorf("Sprintf(%s) = %q, contains the secret", verb, got)
		}
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if got, want := string(b), `{"Token":"****1234"}`; got != want {
		t.Errorf("json.Marshal = %s, want %s", got, want)
	}
}
