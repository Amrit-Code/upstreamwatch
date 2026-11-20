package osv

import (
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestToQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		purl, version string
		wantName      string
		wantEco       string
		wantVersion   string
		wantOK        bool
	}{
		{"pkg:golang/github.com/gin-gonic/gin", "v1.9.1", "github.com/gin-gonic/gin", "Go", "1.9.1", true},
		{"pkg:golang/github.com/gin-gonic/gin", "1.9.1", "github.com/gin-gonic/gin", "Go", "1.9.1", true},
		{"pkg:golang/example.com/m", "vv1.0.0", "example.com/m", "Go", "v1.0.0", true},
		{"pkg:golang/toolchain", "1.26", "stdlib", "Go", "1.26.0", true},
		{"pkg:golang/toolchain", "go1.26.3", "stdlib", "Go", "1.26.3", true},
		{"pkg:golang/toolchain", "go1.26", "stdlib", "Go", "1.26.0", true},
		{"pkg:golang/toolchain", "1.26.3", "stdlib", "Go", "1.26.3", true},
		{"pkg:golang/toolchain", "1.26rc1", "stdlib", "Go", "1.26rc1", true},
		{"pkg:pypi/pyyaml", "6.0.1", "pyyaml", "PyPI", "6.0.1", true},
		{"pkg:pypi/ruamel.yaml", "0.17.0", "ruamel-yaml", "PyPI", "0.17.0", true},
		{"pkg:pypi/Zope_Interface", "5.0", "zope-interface", "PyPI", "5.0", true},
		{"pkg:githubactions/actions/checkout", "v4.1.1", "actions/checkout", "GitHub Actions", "v4.1.1", true},
		{"pkg:npm/%40scope/name", "1.0.0", "@scope/name", "npm", "1.0.0", true},
		{"pkg:cargo/serde", "1.0.200", "serde", "crates.io", "1.0.200", true},
		{"pkg:maven/org.apache.tomcat/tomcat-coyote", "9.0.80", "org.apache.tomcat:tomcat-coyote", "Maven", "9.0.80", true},
		{"pkg:maven/com.example/a%2Db", "1.0", "com.example:a-b", "Maven", "1.0", true},
		{"pkg:maven/tomcat-coyote", "9.0.80", "", "", "", false},
		{"pkg:maven/org.apache.tomcat/", "9.0.80", "", "", "", false},
		{"pkg:docker/library/golang", "1.26", "", "", "", false},
		{"pkg:generic/thing", "1.0", "", "", "", false},
		{"pkg:golang/example.com/m", "", "", "", "", false},
		{"pkg:golang/toolchain", "go", "", "", "", false},
		{"pkg:npm/%zz", "1.0.0", "", "", "", false},
		{"not-a-purl", "1.0.0", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.purl+"@"+tt.version, func(t *testing.T) {
			t.Parallel()
			got, ok := toQuery(model.PackageVersion{PURL: tt.purl, Version: tt.version})
			if ok != tt.wantOK {
				t.Fatalf("toQuery(%s, %q) ok = %v, want %v", tt.purl, tt.version, ok, tt.wantOK)
			}
			want := osvQuery{Package: osvPackage{Name: tt.wantName, Ecosystem: tt.wantEco}, Version: tt.wantVersion}
			if ok && got != want {
				t.Errorf("toQuery(%s, %q) = %+v, want %+v", tt.purl, tt.version, got, want)
			}
		})
	}
}

func TestCompleteMinor(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"1.26", "1.26.0"},
		{"1.26.3", "1.26.3"},
		{"1", "1"},
		{"1.x", "1.x"},
		{".26", ".26"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := completeMinor(tt.in); got != tt.want {
			t.Errorf("completeMinor(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
