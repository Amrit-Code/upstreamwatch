package config

import (
	"reflect"
	"testing"
)

func TestLoad_TrailingEmptyDocumentsLoad(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
	}{
		{name: "trailing ---", content: "data_dir: x\n---\n"},
		{name: "trailing --- and comment", content: "data_dir: x\n---\n# nothing here\n"},
		{name: "document end marker", content: "data_dir: x\n...\n"},
		{name: "explicit ~", content: "data_dir: x\n---\n~\n"},
		{name: "explicit null", content: "data_dir: x\n---\nnull\n"},
		{name: "two empty documents", content: "data_dir: x\n---\n---\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := loadString(t, tt.content, noEnv)
			if err != nil {
				t.Fatalf("Load(%q) error = %v, want nil", tt.content, err)
			}
			if want := wantDefaults("x"); !reflect.DeepEqual(got, want) {
				t.Errorf("Load(%q) = %+v, want %+v", tt.content, got, want)
			}
		})
	}
}

func TestLoad_SecondDocumentWithContentRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
	}{
		{name: "mapping", content: "data_dir: x\n---\ndata_dir: y\n"},
		{name: "after an empty document", content: "data_dir: x\n---\n---\nhttp:\n  listen: 127.0.0.1:9090\n"},
		{name: "scalar", content: "data_dir: x\n---\nhello\n"},
		{name: "empty first document", content: "---\n---\ndata_dir: y\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeConfig(t, tt.content)
			got, err := Load(path, noEnv)
			if err == nil {
				t.Fatalf("Load(%q) = %+v, want an error", tt.content, got)
			}
			if want := "parse " + path + ": more than one YAML document"; err.Error() != want {
				t.Errorf("Load(%q) error = %q, want %q", tt.content, err, want)
			}
			if !reflect.DeepEqual(got, Config{}) {
				t.Errorf("Load(%q) = %+v, want the zero Config", tt.content, got)
			}
		})
	}
}
