package email_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeConfig selects what the fake SMTP server offers.
type fakeConfig struct {
	// startTLS, when set, is advertised as STARTTLS and used for the upgrade.
	startTLS *tls.Config
	// implicitTLS, when set, wraps every connection in TLS before the
	// greeting, as on port 465.
	implicitTLS *tls.Config
	// auth advertises AUTH PLAIN; rejectAuth answers it with 535.
	auth       bool
	rejectAuth bool
	// hang accepts connections and never answers.
	hang bool
}

// received is what the fake server saw, across all connections.
type received struct {
	// commands is the verb of each command, in order.
	commands []string
	// tlsAtAuth reports whether the connection was encrypted when AUTH
	// arrived.
	tlsAtAuth bool
	// authPayload is the decoded AUTH PLAIN initial response.
	authPayload string
	mailFrom    string
	rcpts       []string
	// data is the message as it arrived after DATA, dot-unstuffed, with its
	// line endings exactly as sent.
	data []byte
}

// fakeSMTP is a minimal ESMTP server on 127.0.0.1 that records what it
// receives.
type fakeSMTP struct {
	ln   net.Listener
	cfg  fakeConfig
	wg   sync.WaitGroup
	once sync.Once

	mu  sync.Mutex
	got received
}

func newFakeSMTP(t *testing.T, cfg fakeConfig) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{ln: ln, cfg: cfg}
	s.wg.Go(s.acceptLoop)
	t.Cleanup(s.close)
	return s
}

// addr is the listener's host:port.
func (s *fakeSMTP) addr() string { return s.ln.Addr().String() }

// port is the listener's port.
func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// close stops accepting and waits for every connection handler to return.
func (s *fakeSMTP) close() {
	s.once.Do(func() {
		_ = s.ln.Close()
		s.wg.Wait()
	})
}

// result closes the server and returns what it received.
func (s *fakeSMTP) result() received {
	s.close()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.got
}

func (s *fakeSMTP) record(f func(r *received)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.got)
}

func (s *fakeSMTP) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Go(func() { s.serve(conn) })
	}
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if s.cfg.hang {
		_, _ = io.Copy(io.Discard, conn) // returns when the client closes
		return
	}
	tlsOn := false
	if s.cfg.implicitTLS != nil {
		tc := tls.Server(conn, s.cfg.implicitTLS)
		if err := tc.Handshake(); err != nil {
			return
		}
		conn, tlsOn = tc, true
	}
	tp := textproto.NewConn(conn)
	reply := func(format string, args ...any) bool { return tp.PrintfLine(format, args...) == nil }
	if !reply("220 fake ESMTP") {
		return
	}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)
		s.record(func(r *received) { r.commands = append(r.commands, verb) })
		switch verb {
		case "EHLO":
			ext := []string{"fake"}
			if s.cfg.startTLS != nil && !tlsOn {
				ext = append(ext, "STARTTLS")
			}
			if s.cfg.auth {
				ext = append(ext, "AUTH PLAIN")
			}
			for i, e := range ext {
				sep := "-"
				if i == len(ext)-1 {
					sep = " "
				}
				if !reply("250%s%s", sep, e) {
					return
				}
			}
		case "STARTTLS":
			if s.cfg.startTLS == nil || tlsOn {
				reply("502 not offered")
				continue
			}
			if !reply("220 ready to start TLS") {
				return
			}
			tc := tls.Server(conn, s.cfg.startTLS)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, tlsOn = tc, true
			tp = textproto.NewConn(conn)
		case "AUTH":
			_, initial, _ := strings.Cut(arg, " ")
			payload, _ := base64.StdEncoding.DecodeString(initial)
			on := tlsOn
			s.record(func(r *received) { r.authPayload, r.tlsAtAuth = string(payload), on })
			if s.cfg.rejectAuth {
				reply("535 5.7.8 authentication credentials invalid")
			} else {
				reply("235 2.7.0 authentication successful")
			}
		case "MAIL":
			s.record(func(r *received) { r.mailFrom = arg })
			reply("250 ok")
		case "RCPT":
			s.record(func(r *received) { r.rcpts = append(r.rcpts, arg) })
			reply("250 ok")
		case "DATA":
			if !reply("354 end with <CRLF>.<CRLF>") {
				return
			}
			var data bytes.Buffer
			for {
				l, err := tp.R.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(strings.TrimPrefix(l, "."))
			}
			s.record(func(r *received) { r.data = data.Bytes() })
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unrecognised command")
		}
	}
}

// selfSignedTLS returns a server TLS config with a fresh self-signed
// certificate for 127.0.0.1, localhost and smtp.invalid-example.test, and a
// pool that trusts it.
func selfSignedTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	// The certificate is checked against the real clock by crypto/tls, so
	// its validity has to cover now.
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake smtp"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:              []string{"localhost", "smtp.invalid-example.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	cfg := &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}
	return cfg, pool
}
