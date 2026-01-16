package cmd

import "testing"

func TestParseRelicsVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    relicsVersion
		wantErr bool
	}{
		{"0.44.0", relicsVersion{0, 44, 0}, false},
		{"1.2.3", relicsVersion{1, 2, 3}, false},
		{"0.44.0-dev", relicsVersion{0, 44, 0}, false},
		{"v0.44.0", relicsVersion{0, 44, 0}, false},
		{"0.44", relicsVersion{0, 44, 0}, false},
		{"10.20.30", relicsVersion{10, 20, 30}, false},
		{"invalid", relicsVersion{}, true},
		{"", relicsVersion{}, true},
		{"a.b.c", relicsVersion{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseRelicsVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRelicsVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseRelicsVersion(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRelicsVersionCompare(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"0.44.0", "0.44.0", 0},
		{"0.44.0", "0.43.0", 1},
		{"0.43.0", "0.44.0", -1},
		{"1.0.0", "0.99.99", 1},
		{"0.44.1", "0.44.0", 1},
		{"0.44.0", "0.44.1", -1},
		{"1.2.3", "1.2.3", 0},
	}

	for _, tt := range tests {
		t.Run(tt.v1+"_vs_"+tt.v2, func(t *testing.T) {
			v1, err := parseRelicsVersion(tt.v1)
			if err != nil {
				t.Fatalf("failed to parse v1 %q: %v", tt.v1, err)
			}
			v2, err := parseRelicsVersion(tt.v2)
			if err != nil {
				t.Fatalf("failed to parse v2 %q: %v", tt.v2, err)
			}

			got := v1.compare(v2)
			if got != tt.want {
				t.Errorf("(%s).compare(%s) = %d, want %d", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}
