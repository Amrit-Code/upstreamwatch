package model

import "testing"

func TestPyPIName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "dot", in: "ruamel.yaml", want: "ruamel-yaml"},
		{name: "underscore and capitals", in: "Zope_Interface", want: "zope-interface"},
		{name: "repeated dash", in: "Foo--Bar", want: "foo-bar"},
		{name: "mixed run", in: "a._-b", want: "a-b"},
		{name: "already canonical", in: "example-lib", want: "example-lib"},
		{name: "upper case only", in: "PyYAML", want: "pyyaml"},
		{name: "leading and trailing runs kept as one dash", in: "_lead..", want: "-lead-"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PyPIName(tt.in); got != tt.want {
				t.Errorf("PyPIName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
