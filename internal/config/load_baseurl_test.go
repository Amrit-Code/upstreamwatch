package config

import (
	"strings"
	"testing"
)

func TestLoad_BaseURLNeedsHostname(t *testing.T) {
	t.Parallel()
	tests := []struct {
		baseURL string
		wantErr bool
	}{
		{baseURL: "https://upw.example.ts.net", wantErr: false},
		{baseURL: "https://upw.example.net:8443/upw/", wantErr: false},
		{baseURL: "http://127.0.0.1:8080", wantErr: false},
		{baseURL: "https://:443", wantErr: true},
		{baseURL: "http://:8080/upw", wantErr: true},
		{baseURL: "https://", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			t.Parallel()
			got, err := loadString(t, "http:\n  base_url: \""+tt.baseURL+"\"\n", noEnv)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Load(base_url %q) error = %v, want nil", tt.baseURL, err)
				}
				if got.HTTP.BaseURL != tt.baseURL {
					t.Errorf("Load(base_url %q).HTTP.BaseURL = %q", tt.baseURL, got.HTTP.BaseURL)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "http.base_url:") {
				t.Errorf("Load(base_url %q) error = %v, want an http.base_url error", tt.baseURL, err)
			}
		})
	}
}
