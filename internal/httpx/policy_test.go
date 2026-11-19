package httpx

import (
	"errors"
	"net/netip"
	"net/url"
	"testing"
)

func TestBlockedAddr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"0.0.0.0", true},
		{"224.0.0.1", true},
		{"::ffff:10.0.0.1", true},
		{"::", true},
		{"ff02::1", true},
		{"fe80::1%en0", true},
		{"8.8.8.8", false},
		{"100.63.255.255", false},
		{"100.128.0.0", false},
		{"2606:4700:4700::1111", false},
		{"::ffff:8.8.8.8", false},
		// 0.0.0.0/8
		{"0.1.2.3", true},
		{"0.255.255.255", true},
		{"1.0.0.0", false},
		// 192.0.0.0/24
		{"192.0.0.0", true},
		{"192.0.0.255", true},
		{"191.255.255.255", false},
		{"192.0.1.0", false},
		// 198.18.0.0/15
		{"198.18.0.0", true},
		{"198.19.255.255", true},
		{"198.17.255.255", false},
		{"198.20.0.0", false},
		// 240.0.0.0/4; the neighbour below is multicast
		{"240.0.0.0", true},
		{"255.255.255.255", true},
		{"239.255.255.255", true},
		// documentation ranges
		{"192.0.2.0", true},
		{"192.0.2.255", true},
		{"192.0.3.0", false},
		{"198.51.100.0", true},
		{"198.51.100.255", true},
		{"198.51.99.255", false},
		{"198.51.101.0", false},
		{"203.0.113.0", true},
		{"203.0.113.255", true},
		{"203.0.112.255", false},
		{"203.0.114.0", false},
		{"2001:db8::1", true},
		{"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:db9::1", false},
		// NAT64 and 6to4 are judged by the embedded IPv4 address
		{"64:ff9b::a00:1", true},
		{"64:ff9b::7f00:1", true},
		{"64:ff9b::808:808", false},
		{"2002:a00:1::1", true},
		{"2002:c0a8:101::1", true},
		{"2002:808:808::1", false},
		// local-use NAT64 and Teredo are refused outright
		{"64:ff9b:1::808:808", true},
		{"2001::1", true},
		{"2001:0:ffff:ffff::1", true},
		{"2001:1::1", false},
		// deprecated site-local is refused outright
		{"fec0::1", true},
		{"feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		// IPv4-compatible addresses are judged by the embedded IPv4 address
		{"::10.0.0.1", true},
		{"::127.0.0.1", true},
		{"::8.8.8.8", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			got := blockedAddr(netip.MustParseAddr(tt.addr))
			if got != tt.want {
				t.Errorf("blockedAddr(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestRefuseBlocked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		address string
		wantErr bool
	}{
		{"8.8.8.8:443", false},
		{"[2606:4700:4700::1111]:443", false},
		{"127.0.0.1:443", true},
		{"[::ffff:192.168.1.1]:443", true},
		{"not-an-address", true},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			t.Parallel()
			err := refuseBlocked("tcp", tt.address, nil)
			if got := errors.Is(err, ErrBlockedAddress); got != tt.wantErr {
				t.Errorf("refuseBlocked(%q) = %v, want blocked %v", tt.address, err, tt.wantErr)
			}
		})
	}
}

func TestCheckURL(t *testing.T) {
	t.Parallel()
	c := New(nil, Options{AllowPrivateHosts: []string{"Model.Local"}}, nil)
	tests := []struct {
		url  string
		want error
	}{
		{"https://example.com/x", nil},
		{"http://model.local:11434/v1", nil},
		{"HTTP://MODEL.LOCAL/v1", nil},
		{"http://example.com/", ErrSchemeNotAllowed},
		{"http://sub.model.local/", ErrSchemeNotAllowed},
		{"ftp://example.com/", ErrSchemeNotAllowed},
		{"file:///etc/passwd", ErrSchemeNotAllowed},
		{"https://user:pass@example.com/", errUserinfo},
		{"https:///path", errNoHost},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.url, err)
			}
			got := c.checkURL(u)
			if !errors.Is(got, tt.want) || (tt.want == nil && got != nil) {
				t.Errorf("checkURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
