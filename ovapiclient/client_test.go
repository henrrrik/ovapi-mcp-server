package ovapiclient

import "testing"

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		segments []string
		want     string
	}{
		{
			name:     "base only",
			base:     "https://v0.ovapi.nl",
			segments: nil,
			want:     "https://v0.ovapi.nl",
		},
		{
			name:     "single segment",
			base:     "https://v0.ovapi.nl",
			segments: []string{"line"},
			want:     "https://v0.ovapi.nl/line",
		},
		{
			name:     "multiple segments",
			base:     "https://v0.ovapi.nl",
			segments: []string{"tpc", "ut010"},
			want:     "https://v0.ovapi.nl/tpc/ut010",
		},
		{
			name:     "comma-separated values in segment",
			base:     "https://v0.ovapi.nl",
			segments: []string{"tpc", "ut010,ut020"},
			want:     "https://v0.ovapi.nl/tpc/ut010,ut020",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildURL(tt.base, tt.segments...)
			if got != tt.want {
				t.Errorf("BuildURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
}
