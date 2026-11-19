package gitsrc

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestCheckRemote_PinsAndCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		url   string
		addrs []string
		pin   string
	}{
		{"ipv4", "https://git.example.org/org/repo.git", []string{"8.8.8.8", "8.8.4.4"}, "http.curloptResolve=git.example.org:443:8.8.8.8"},
		{"ipv6 in brackets", "https://git.example.org/org/repo.git", []string{"2606:4700::1111"}, "http.curloptResolve=git.example.org:443:[2606:4700::1111]"},
		{"mapped ipv4 unmapped", "https://git.example.org/x", []string{"::ffff:8.8.8.8"}, "http.curloptResolve=git.example.org:443:8.8.8.8"},
		{"explicit port", "https://git.example.org:8443/x", []string{"8.8.8.8"}, "http.curloptResolve=git.example.org:8443:8.8.8.8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := New(Options{LookupHost: func(context.Context, string) ([]netip.Addr, error) {
				var out []netip.Addr
				for _, a := range tt.addrs {
					out = append(out, netip.MustParseAddr(a))
				}
				return out, nil
			}}, slog.New(slog.DiscardHandler))
			const token = "fake-token-abcdefgh1234"
			rem, err := g.checkRemote(t.Context(), model.TargetRepo{URL: tt.url, Token: token})
			if err != nil {
				t.Fatalf("checkRemote(%q) error = %v", tt.url, err)
			}
			if want := []string{tt.pin}; !reflect.DeepEqual(rem.config, want) {
				t.Errorf("checkRemote(%q) config = %q, want %q", tt.url, rem.config, want)
			}
			host := strings.TrimPrefix(tt.url, "https://")
			host = host[:strings.IndexByte(host, '/')]
			wantEnv := []string{
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http.https://" + host + "/.extraHeader",
				"GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)),
			}
			if !reflect.DeepEqual(rem.env, wantEnv) {
				t.Errorf("checkRemote(%q) env = %q, want %q", tt.url, rem.env, wantEnv)
			}
		})
	}
}

func TestCheckRemote_NoTokenNoHeader(t *testing.T) {
	t.Parallel()
	g := New(Options{LookupHost: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}}, slog.New(slog.DiscardHandler))
	rem, err := g.checkRemote(t.Context(), model.TargetRepo{URL: "https://git.example.org/x"})
	if err != nil {
		t.Fatalf("checkRemote() error = %v", err)
	}
	if rem.env != nil {
		t.Errorf("checkRemote() env = %q, want none without a token", rem.env)
	}
}

func TestCheckRemote_PrivateHostSkipsCheckAndPin(t *testing.T) {
	t.Parallel()
	g := New(Options{LookupHost: func(context.Context, string) ([]netip.Addr, error) {
		t.Error("LookupHost called for an allowed private host")
		return nil, nil
	}}, slog.New(slog.DiscardHandler))
	rem, err := g.checkRemote(t.Context(), model.TargetRepo{URL: "https://git.lan.example/x", AllowPrivateHost: true})
	if err != nil {
		t.Fatalf("checkRemote() error = %v", err)
	}
	if rem.config != nil {
		t.Errorf("checkRemote() config = %q, want no pin", rem.config)
	}
}

func TestStrictPolicyByDefault(t *testing.T) {
	t.Parallel()
	g := New(Options{}, slog.New(slog.DiscardHandler))
	if g.opts.AllowFileURLs {
		t.Error("New(Options{}) allows file URLs")
	}
	if _, err := checkURL("file:///tmp/repo.git", g.opts.AllowFileURLs); err == nil {
		t.Error("checkURL(file URL) with default options = nil error, want a refusal")
	}
	cmd, err := g.command(t.Context(), invocation{args: []string{"--version"}}, &cappedBuffer{limit: 1})
	if err != nil {
		t.Fatalf("command() error = %v", err)
	}
	args := strings.Join(cmd.Args, " ")
	if strings.Contains(args, "protocol.file.allow") {
		t.Errorf("default command allows the file protocol: %s", args)
	}
	for _, want := range baseConfig {
		if !strings.Contains(args, "-c "+want) {
			t.Errorf("default command lacks -c %s: %s", want, args)
		}
	}
}

func TestRunError_RedactsReflectedCredentials(t *testing.T) {
	t.Parallel()
	const token = "fake-token-abcdefgh1234"
	_, secrets := credentialEnv("git.example.org", token)
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	cause := errors.New("exit status 128")

	tests := []struct {
		name   string
		stderr string
	}{
		{"raw token", "fatal: bad token " + token + " for git.example.org"},
		{"basic value", "fatal: rejected header Authorization: Basic " + basic},
		{"basic value alone", "fatal: rejected " + basic},
		{"user and token", "fatal: https://x-access-token:" + token + "@git.example.org/x denied"},
		{"token cut by the cap", strings.Repeat("e", outputLimit-10) + " " + token},
		{"basic cut by the cap", strings.Repeat("e", outputLimit-20) + " " + basic},
		{"other credential", "fatal: Authorization: Bearer some-other-value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stderr := &cappedBuffer{limit: outputLimit}
			stderr.Write([]byte(tt.stderr))
			err := runError(t.Context(), "fetch", cause, stderr, secrets)
			if !errors.Is(err, cause) {
				t.Errorf("runError() = %v, want it to wrap the cause", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, redacted) {
				t.Errorf("runError() = %q, want %q in it", msg, redacted)
			}
			// The cap cases keep only the first few characters of the
			// secret, so even a short prefix must be gone.
			for _, leak := range []string{token, basic, token[:5], basic[:5], "some-other-value"} {
				if strings.Contains(msg, leak) {
					t.Errorf("runError() = %q, contains %q", msg, leak)
				}
			}
			if len(msg) > outputLimit+100 {
				t.Errorf("runError() length = %d, want the stderr bound kept", len(msg))
			}
		})
	}
}

func TestParseSymref(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		out    string
		want   string
		wantOK bool
	}{
		{"main", "ref: refs/heads/main\tHEAD\n0123456789abcdef0123456789abcdef01234567\tHEAD\n", "main", true},
		{"nested", "ref: refs/heads/release/v2\tHEAD\n", "release/v2", true},
		{"no symref", "0123456789abcdef0123456789abcdef01234567\tHEAD\n", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		got, ok := parseSymref(tt.out)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("parseSymref(%s) = %q, %v, want %q, %v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}
