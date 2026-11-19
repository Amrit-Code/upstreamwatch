package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/discord"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/email"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/slack"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/stdout"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
)

// notifyStdout is where a stdout notify channel writes the digest. Tests in
// this package replace it; nothing else changes it.
var notifyStdout io.Writer = os.Stdout

// channelNotifier is one configured notify channel built into its adapter.
type channelNotifier struct {
	name     string
	notifier report.Notifier
	// checker probes the channel for upw doctor; nil for stdout, which has
	// nothing to probe.
	checker Checker
	// success is the doctor detail printed when the probe passes.
	success string
}

// channelNames returns the name of each channel, in config order: its type,
// with "#2", "#3" and so on appended for the second and later channels of
// the same type. Every configured channel counts, including one skipped for
// an unset secret, so a name does not change with the environment.
func channelNames(chs []config.Channel) []string {
	seen := make(map[string]int, len(chs))
	names := make([]string, 0, len(chs))
	for _, ch := range chs {
		seen[ch.Type]++
		name := ch.Type
		if n := seen[ch.Type]; n > 1 {
			name += "#" + strconv.Itoa(n)
		}
		names = append(names, name)
	}
	return names
}

// secretUnset reports whether the channel takes a secret and its
// environment variable was unset or empty when the config was loaded.
func secretUnset(ch config.Channel) bool {
	switch ch.Type {
	case "email":
		return ch.Password == ""
	case "discord_webhook", "slack_webhook", "ntfy":
		return ch.URL == ""
	default:
		return false
	}
}

// buildChannels builds one digest channel per configured notify channel, in
// config order, named as channelNames names them. A channel whose secret is
// unset is skipped with a warning. Every channel that cannot be built is
// returned in the error, which names the channel and never its secret.
func buildChannels(chs []config.Channel, hc *httpx.Client, w io.Writer, logger *slog.Logger) ([]report.Channel, error) {
	var channels []report.Channel
	var errs []error
	for i, name := range channelNames(chs) {
		if secretUnset(chs[i]) {
			logger.Warn("notify channel skipped, secret not set", "channel", name)
			continue
		}
		n, err := newChannelNotifier(name, chs[i], hc, w, logger)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		channels = append(channels, report.Channel{Name: n.name, Notifier: n.notifier})
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return channels, nil
}

// newChannelNotifier builds the adapter for one channel. stdout writes to w;
// the webhook and ntfy adapters send through hc. The adapters' errors name
// at most the URL's host and never the email password; the error returned
// adds the channel's name.
func newChannelNotifier(name string, ch config.Channel, hc *httpx.Client, w io.Writer, logger *slog.Logger) (channelNotifier, error) {
	cn := channelNotifier{name: name}
	var err error
	switch ch.Type {
	case "email":
		var n *email.Notifier
		n, err = email.New(email.Options{
			Host:     ch.Host,
			Port:     ch.Port,
			Username: ch.Username,
			Password: ch.Password,
			From:     ch.From,
			To:       ch.To,
		}, logger)
		cn.notifier, cn.checker, cn.success = n, n, "login accepted"
	case "stdout":
		cn.notifier, cn.success = stdout.New(w), "writes to standard output"
	case "discord_webhook":
		var n *discord.Notifier
		n, err = discord.New(hc, ch.URL, logger)
		cn.notifier, cn.checker, cn.success = n, n, "webhook reachable"
	case "slack_webhook":
		var n *slack.Notifier
		n, err = slack.New(hc, ch.URL, logger)
		cn.notifier, cn.checker, cn.success = n, n, "configured (not probed)"
	case "ntfy":
		var n *ntfy.Notifier
		n, err = ntfy.New(hc, ch.URL, logger)
		cn.notifier, cn.checker, cn.success = n, n, "server healthy"
	default:
		// Config validation rejects any other type before this runs.
		err = fmt.Errorf("unknown type %q", ch.Type)
	}
	if err != nil {
		return channelNotifier{}, fmt.Errorf("notify channel %s: %w", name, err)
	}
	return cn, nil
}
