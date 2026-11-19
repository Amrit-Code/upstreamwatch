// Package email is the notifier that sends the digest as a plain-text email
// through the operator's SMTP server, using the standard library's net/smtp.
package email

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/netip"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	// defaultTimeout bounds one whole SMTP exchange when Options.Timeout is
	// not set.
	defaultTimeout = 30 * time.Second
	// implicitTLSPort is the SMTPS port, where TLS starts before the greeting.
	implicitTLSPort = 465
	// helloName is the name sent with EHLO. net/smtp uses the same default;
	// it avoids disclosing the machine's hostname.
	helloName = "localhost"
)

// errNoSTARTTLS is returned when a server that is not on loopback does not
// offer STARTTLS, so the exchange would be unencrypted.
var errNoSTARTTLS = errors.New("server does not offer STARTTLS; refusing to continue without TLS")

// Options configures a Notifier.
type Options struct {
	Host     string
	Port     int // 465 means implicit TLS; anything else connects plain and upgrades with STARTTLS
	Username string
	Password model.Secret
	From     string
	To       []string
	Timeout  time.Duration // whole dial-to-quit exchange; default 30s
	// RootCAs, when set, replaces the system roots (tests use it with a
	// self-signed certificate).
	RootCAs *x509.CertPool
}

// Notifier sends each digest as one email to every configured recipient. It
// requires TLS (implicit on port 465, STARTTLS otherwise) unless the host is
// a loopback address or localhost.
type Notifier struct {
	opts    Options
	addr    string
	env     envelope
	timeout time.Duration
	logger  *slog.Logger
	// dial opens the TCP connection; tests replace it to reach a fake
	// server under a name that is not loopback.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// New validates opts and returns a Notifier. It does no I/O. Every problem
// found is returned, joined; no error contains the password.
func New(opts Options, logger *slog.Logger) (*Notifier, error) {
	var errs []error
	if opts.Host == "" {
		errs = append(errs, errors.New("email: host is empty"))
	} else if hasLineBreak(opts.Host) {
		errs = append(errs, errors.New("email: host contains CR or LF"))
	}
	if opts.Port < 1 || opts.Port > 65535 {
		errs = append(errs, fmt.Errorf("email: port %d is outside 1..65535", opts.Port))
	}
	if hasLineBreak(opts.Username) {
		errs = append(errs, errors.New("email: username contains CR or LF"))
	}
	switch {
	case opts.Username != "" && opts.Password == "":
		errs = append(errs, errors.New("email: username is set but password is empty"))
	case opts.Username == "" && opts.Password != "":
		errs = append(errs, errors.New("email: password is set but username is empty"))
	}
	from, err := parseAddress("from", opts.From)
	if err != nil {
		errs = append(errs, err)
	}
	if len(opts.To) == 0 {
		errs = append(errs, errors.New("email: to is empty"))
	}
	to := make([]*mail.Address, 0, len(opts.To))
	for i, raw := range opts.To {
		a, err := parseAddress("to["+strconv.Itoa(i)+"]", raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		to = append(to, a)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	return &Notifier{
		opts:    opts,
		addr:    net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)),
		env:     envelope{from: from, to: to},
		timeout: timeout,
		logger:  logger,
		dial:    dialer.DialContext,
	}, nil
}

// parseAddress parses one configured address, naming field in the error.
func parseAddress(field, raw string) (*mail.Address, error) {
	switch {
	case raw == "":
		return nil, fmt.Errorf("email: %s is empty", field)
	case hasLineBreak(raw):
		return nil, fmt.Errorf("email: %s contains CR or LF", field)
	}
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return nil, fmt.Errorf("email: %s: %w", field, err)
	}
	return a, nil
}

// hasLineBreak reports whether s contains CR or LF.
func hasLineBreak(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}

// Send delivers d as one message to every recipient.
func (n *Notifier) Send(ctx context.Context, d model.Digest) error {
	msg, err := buildMessage(d, n.env)
	if err != nil {
		return fmt.Errorf("email: %w", err)
	}
	err = n.session(ctx, func(ctx context.Context, c *smtp.Client) error {
		if err := c.Mail(n.env.from.Address); err != nil {
			return n.stepError(ctx, "mail", err)
		}
		for _, a := range n.env.to {
			if err := c.Rcpt(a.Address); err != nil {
				return n.stepError(ctx, "rcpt", err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return n.stepError(ctx, "data", err)
		}
		if _, err := w.Write(msg); err != nil {
			_ = w.Close() // best effort: the write error is the one returned
			return n.stepError(ctx, "data", err)
		}
		if err := w.Close(); err != nil {
			return n.stepError(ctx, "data", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	n.logger.Info("email digest sent", "recipients", len(n.env.to), "host", n.opts.Host)
	return nil
}

// Check connects, says EHLO, upgrades to TLS as Send does, authenticates
// when a username is set, and quits without sending a message.
func (n *Notifier) Check(ctx context.Context) error {
	return n.session(ctx, func(context.Context, *smtp.Client) error { return nil })
}

// session runs one SMTP exchange: dial, TLS, EHLO, STARTTLS, AUTH, then fn,
// then QUIT. The whole exchange is bounded by the timeout and ends when ctx
// ends.
func (n *Notifier) session(ctx context.Context, fn func(context.Context, *smtp.Client) error) error {
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()

	conn, err := n.dial(ctx, "tcp", n.addr)
	if err != nil {
		return n.stepError(ctx, "dial", err)
	}
	// net/smtp has no timeouts or context of its own: the deadline bounds
	// every read and write, and closing the connection when ctx ends unblocks
	// whatever call is waiting.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline() // always set: ctx carries the timeout
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close() // best effort: the deadline error is the one returned
		return n.stepError(ctx, "dial", err)
	}

	if n.opts.Port == implicitTLSPort {
		tc := tls.Client(conn, n.tlsConfig())
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close() // best effort: the handshake error is the one returned
			return n.stepError(ctx, "tls", err)
		}
		conn = tc
	}

	c, err := smtp.NewClient(conn, n.opts.Host)
	if err != nil {
		_ = conn.Close() // best effort: the greeting error is the one returned
		return n.stepError(ctx, "greeting", err)
	}
	// After a successful QUIT the connection is already closed and this
	// returns an error that means nothing; on failure it is the cleanup.
	defer func() { _ = c.Close() }()

	if err := c.Hello(helloName); err != nil {
		return n.stepError(ctx, "ehlo", err)
	}
	if n.opts.Port != implicitTLSPort {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(n.tlsConfig()); err != nil {
				return n.stepError(ctx, "starttls", err)
			}
		} else if !isLoopback(n.opts.Host) {
			return n.stepError(ctx, "starttls", errNoSTARTTLS)
		}
	}
	if n.opts.Username != "" {
		auth := smtp.PlainAuth("", n.opts.Username, n.opts.Password.Reveal(), n.opts.Host)
		if err := c.Auth(auth); err != nil {
			return n.stepError(ctx, "auth", err)
		}
	}
	if err := fn(ctx, c); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		return n.stepError(ctx, "quit", err)
	}
	return nil
}

// tlsConfig returns the TLS settings for both implicit TLS and STARTTLS.
func (n *Notifier) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName: n.opts.Host,
		MinVersion: tls.VersionTLS12,
		RootCAs:    n.opts.RootCAs,
	}
}

// isLoopback reports whether host is localhost or a loopback IP address, the
// only hosts that may be reached without TLS.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// stepError describes a failure at one SMTP step, naming host, port and
// step. When ctx has ended, its error replaces the closed-connection error
// that cancellation causes. The text is redacted and never holds the
// password.
func (n *Notifier) stepError(ctx context.Context, step string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	text := "email: " + n.addr + ": " + step + ": " + err.Error()
	if pw := n.opts.Password.Reveal(); pw != "" {
		text = strings.ReplaceAll(text, pw, "[REDACTED]")
	}
	return &smtpError{text: model.Redact(text), err: err}
}

// smtpError is a failed SMTP step whose text has been redacted. It unwraps to
// the cause so callers can still test for context cancellation.
type smtpError struct {
	text string
	err  error
}

func (e *smtpError) Error() string { return e.text }

func (e *smtpError) Unwrap() error { return e.err }
