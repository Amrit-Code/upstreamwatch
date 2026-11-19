package main

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/discord"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/email"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/slack"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/stdout"
)

// validEmail returns an email channel that email.New accepts.
func validEmail() config.Channel {
	return config.Channel{
		Type: "email", Host: "smtp.example.invalid", Port: 587, Username: "upw",
		Password: fakeSMTPPassword, From: "upw@example.invalid", To: []string{"ops@example.invalid"},
	}
}

func testClient() *httpx.Client {
	return httpx.New(nil, httpx.Options{}, slog.New(slog.DiscardHandler))
}

func TestChannelNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		types []string
		want  []string
	}{
		{name: "none", types: nil, want: []string{}},
		{name: "one of each", types: []string{"email", "stdout", "ntfy"}, want: []string{"email", "stdout", "ntfy"}},
		{name: "repeats numbered per type", types: []string{"stdout", "ntfy", "stdout", "ntfy", "stdout"},
			want: []string{"stdout", "ntfy", "stdout#2", "ntfy#2", "stdout#3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chs := make([]config.Channel, 0, len(tt.types))
			for _, typ := range tt.types {
				chs = append(chs, config.Channel{Type: typ})
			}
			if got := channelNames(chs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("channelNames(%q) = %q, want %q", tt.types, got, tt.want)
			}
		})
	}
}

func TestChannels_Names(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	chs := []config.Channel{{Type: "stdout"}, {Type: "stdout"}, validEmail()}
	got, err := buildChannels(chs, testClient(), &out, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("buildChannels() error = %v", err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	if want := []string{"stdout", "stdout#2", "email"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("channel names = %q, want %q", names, want)
	}
	if _, ok := got[0].Notifier.(*stdout.Notifier); !ok {
		t.Errorf("channel stdout notifier is %T, want *stdout.Notifier", got[0].Notifier)
	}
	if _, ok := got[2].Notifier.(*email.Notifier); !ok {
		t.Errorf("channel email notifier is %T, want *email.Notifier", got[2].Notifier)
	}
	// The stdout channel writes to the writer it was given.
	if err := got[0].Notifier.Send(t.Context(), model.Digest{Heartbeat: true}); err != nil {
		t.Fatalf("stdout Send() error = %v", err)
	}
	if out.Len() == 0 {
		t.Error("stdout channel wrote nothing to the injected writer")
	}
}

func TestChannels_AdapterTypes(t *testing.T) {
	t.Parallel()
	chs := []config.Channel{
		{Type: "discord_webhook", URL: "https://discord.example.invalid/api/webhooks/1/abcd"},
		{Type: "slack_webhook", URL: "https://hooks.example.invalid/services/abcd"},
		{Type: "ntfy", URL: "https://ntfy.example.invalid/upw-topic"},
	}
	got, err := buildChannels(chs, testClient(), &bytes.Buffer{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("buildChannels() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("buildChannels() returned %d channels, want 3", len(got))
	}
	if _, ok := got[0].Notifier.(*discord.Notifier); !ok || got[0].Name != "discord_webhook" {
		t.Errorf("channel 0 = %s %T, want discord_webhook *discord.Notifier", got[0].Name, got[0].Notifier)
	}
	if _, ok := got[1].Notifier.(*slack.Notifier); !ok || got[1].Name != "slack_webhook" {
		t.Errorf("channel 1 = %s %T, want slack_webhook *slack.Notifier", got[1].Name, got[1].Notifier)
	}
	if _, ok := got[2].Notifier.(*ntfy.Notifier); !ok || got[2].Name != "ntfy" {
		t.Errorf("channel 2 = %s %T, want ntfy *ntfy.Notifier", got[2].Name, got[2].Notifier)
	}
}

func TestChannels_SkipUnsetSecret(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	unsetEmail := validEmail()
	unsetEmail.Password = ""
	chs := []config.Channel{
		{Type: "discord_webhook"},
		unsetEmail,
		{Type: "stdout"},
		{Type: "discord_webhook", URL: "https://discord.example.invalid/api/webhooks/1/abcd"},
	}
	got, err := buildChannels(chs, testClient(), &bytes.Buffer{}, logger)
	if err != nil {
		t.Fatalf("buildChannels() error = %v, want unset secrets skipped", err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	// The second webhook keeps its config-position name although the first
	// is skipped.
	if want := []string{"stdout", "discord_webhook#2"}; !reflect.DeepEqual(names, want) {
		t.Errorf("channel names = %q, want %q", names, want)
	}
	for _, name := range []string{"channel=discord_webhook\n", "channel=email\n"} {
		if !strings.Contains(logs.String(), `level=WARN msg="notify channel skipped, secret not set" `+name) {
			t.Errorf("logs do not warn about %q:\n%s", name, logs.String())
		}
	}
}

func TestChannels_EmailInvalid(t *testing.T) {
	t.Parallel()
	bad := validEmail()
	bad.From = "not an address"
	chs := []config.Channel{{Type: "stdout"}, bad}
	got, err := buildChannels(chs, testClient(), &bytes.Buffer{}, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatalf("buildChannels() = %v, nil; want an error", got)
	}
	if got != nil {
		t.Errorf("buildChannels() channels = %v with an error, want nil", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, "notify channel email:") {
		t.Errorf("error = %q, want it to name channel email", msg)
	}
	if strings.Contains(msg, fakeSMTPPassword) {
		t.Errorf("error = %q, contains the password", msg)
	}
}

func TestChannels_WebhookInvalid(t *testing.T) {
	t.Parallel()
	chs := []config.Channel{
		{Type: "ntfy", URL: "https://ntfy.example.invalid/topic"},
		{Type: "ntfy", URL: "http://ntfy.example.invalid/secret-topic"},
	}
	_, err := buildChannels(chs, testClient(), &bytes.Buffer{}, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("buildChannels() error = nil, want the plain-http URL refused")
	}
	if msg := err.Error(); !strings.Contains(msg, "notify channel ntfy#2:") || strings.Contains(msg, "secret-topic") {
		t.Errorf("error = %q, want it to name ntfy#2 and not the topic", msg)
	}
}
