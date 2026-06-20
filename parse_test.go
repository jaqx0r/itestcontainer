package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/jaqx0r/itestcontainer/internal/runtime"
)

func TestParsePorts(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantExposedCount int
		wantBindingKey   runtime.Port
		wantHostPort     string
		wantErr          bool
	}{
		{
			name:             "empty string",
			input:            "",
			wantExposedCount: 0,
		},
		{
			name:             "single port mapping",
			input:            "8080:80/tcp",
			wantExposedCount: 1,
			wantBindingKey:   runtime.Port("80/tcp"),
			wantHostPort:     "8080",
		},
		{
			name:             "two port mappings",
			input:            "8080:80/tcp,9090:90/tcp",
			wantExposedCount: 2,
		},
		{
			name:             "malformed no colon skipped",
			input:            "8080",
			wantExposedCount: 0,
		},
		{
			name:    "invalid container port",
			input:   "8080:notaport",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exposed, bindings, err := parsePorts(tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(exposed) != tc.wantExposedCount {
				t.Errorf("exposed ports: got %d, want %d", len(exposed), tc.wantExposedCount)
			}
			if tc.wantHostPort != "" {
				binding, ok := bindings[tc.wantBindingKey]
				if !ok {
					t.Fatalf("binding for %v not found", tc.wantBindingKey)
				}
				if len(binding) == 0 || binding[0].HostPort != tc.wantHostPort {
					t.Errorf("host port: got %v, want %s", binding, tc.wantHostPort)
				}
			}
		})
	}
}

func TestParseEnvironment(t *testing.T) {
	lookup := func(vars map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			v, ok := vars[k]
			return v, ok
		}
	}

	tests := []struct {
		name    string
		input   string
		env     map[string]string
		wantMap map[string]string
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			env:     map[string]string{},
			wantMap: map[string]string{},
		},
		{
			name:    "single var set",
			input:   "FOO",
			env:     map[string]string{"FOO": "bar"},
			wantMap: map[string]string{"FOO": "bar"},
		},
		{
			name:    "var not set",
			input:   "MISSING",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name:    "multiple vars one missing",
			input:   "FOO,MISSING",
			env:     map[string]string{"FOO": "bar"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseEnvironment(tc.input, lookup(tc.env))
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, v := range tc.wantMap {
				if got[k] != v {
					t.Errorf("env[%s]: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestVolumeSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "non-empty target",
			input: "//some:target",
			want: func() string {
				h := sha256.New()
				h.Write([]byte("//some:target"))
				return hex.EncodeToString(h.Sum(nil))
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := volumeSuffix(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if tc.input != "" && len(got) != 64 {
				t.Errorf("expected 64-char hex, got len=%d", len(got))
			}
		})
	}
}

func TestParseVolumes(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		suffix      string
		wantCount   int
		wantNames   []string
		wantTargets []string
	}{
		{
			name:      "empty string",
			input:     "",
			wantCount: 0,
		},
		{
			name:        "single volume no suffix",
			input:       "data:/app/data",
			suffix:      "",
			wantCount:   1,
			wantNames:   []string{"bazel-itest-data"},
			wantTargets: []string{"/app/data"},
		},
		{
			name:        "single volume with suffix",
			input:       "data:/app/data",
			suffix:      "abc123",
			wantCount:   1,
			wantNames:   []string{"bazel-itest-data-abc123"},
			wantTargets: []string{"/app/data"},
		},
		{
			name:        "multiple volumes",
			input:       "data:/app/data,logs:/app/logs",
			suffix:      "",
			wantCount:   2,
			wantNames:   []string{"bazel-itest-data", "bazel-itest-logs"},
			wantTargets: []string{"/app/data", "/app/logs"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseVolumes(tc.input, tc.suffix)
			if len(got) != tc.wantCount {
				t.Fatalf("mount count: got %d, want %d", len(got), tc.wantCount)
			}
			for i, m := range got {
				if m.Type != "volume" {
					t.Errorf("mount[%d] type: got %q, want %q", i, m.Type, "volume")
				}
				if m.Source != tc.wantNames[i] {
					t.Errorf("mount[%d] source: got %q, want %q", i, m.Source, tc.wantNames[i])
				}
				if m.Target != tc.wantTargets[i] {
					t.Errorf("mount[%d] target: got %q, want %q", i, m.Target, tc.wantTargets[i])
				}
			}
		})
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMap map[string]string
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			wantMap: map[string]string{},
		},
		{
			name:    "single label",
			input:   "key=val",
			wantMap: map[string]string{"key": "val"},
		},
		{
			name:    "two labels",
			input:   "key=val,key2=val2",
			wantMap: map[string]string{"key": "val", "key2": "val2"},
		},
		{
			name:    "no equals sign",
			input:   "noequals",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLabels(tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, v := range tc.wantMap {
				if got[k] != v {
					t.Errorf("label[%s]: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
