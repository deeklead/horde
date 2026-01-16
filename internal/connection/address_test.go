package connection

import (
	"testing"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Address
		wantErr bool
	}{
		{
			name:  "warband/raider",
			input: "horde/rictus",
			want:  &Address{Warband: "horde", Raider: "rictus"},
		},
		{
			name:  "warband/ broadcast",
			input: "horde/",
			want:  &Address{Warband: "horde"},
		},
		{
			name:  "machine:warband/raider",
			input: "vm:horde/rictus",
			want:  &Address{Machine: "vm", Warband: "horde", Raider: "rictus"},
		},
		{
			name:  "machine:warband/ broadcast",
			input: "vm:horde/",
			want:  &Address{Machine: "vm", Warband: "horde"},
		},
		{
			name:  "warband only (no slash)",
			input: "horde",
			want:  &Address{Warband: "horde"},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "empty machine",
			input:   ":horde/rictus",
			wantErr: true,
		},
		{
			name:    "empty warband",
			input:   "vm:/rictus",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseAddress(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseAddress(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Machine != tt.want.Machine {
				t.Errorf("Machine = %q, want %q", got.Machine, tt.want.Machine)
			}
			if got.Warband != tt.want.Warband {
				t.Errorf("Warband = %q, want %q", got.Warband, tt.want.Warband)
			}
			if got.Raider != tt.want.Raider {
				t.Errorf("Raider = %q, want %q", got.Raider, tt.want.Raider)
			}
		})
	}
}

func TestAddressString(t *testing.T) {
	tests := []struct {
		addr *Address
		want string
	}{
		{
			addr: &Address{Warband: "horde", Raider: "rictus"},
			want: "horde/rictus",
		},
		{
			addr: &Address{Warband: "horde"},
			want: "horde/",
		},
		{
			addr: &Address{Machine: "vm", Warband: "horde", Raider: "rictus"},
			want: "vm:horde/rictus",
		},
		{
			addr: &Address{Machine: "vm", Warband: "horde"},
			want: "vm:horde/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.addr.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddressIsLocal(t *testing.T) {
	tests := []struct {
		addr *Address
		want bool
	}{
		{&Address{Warband: "horde"}, true},
		{&Address{Machine: "", Warband: "horde"}, true},
		{&Address{Machine: "local", Warband: "horde"}, true},
		{&Address{Machine: "vm", Warband: "horde"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.addr.String(), func(t *testing.T) {
			if got := tt.addr.IsLocal(); got != tt.want {
				t.Errorf("IsLocal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddressIsBroadcast(t *testing.T) {
	tests := []struct {
		addr *Address
		want bool
	}{
		{&Address{Warband: "horde"}, true},
		{&Address{Warband: "horde", Raider: ""}, true},
		{&Address{Warband: "horde", Raider: "rictus"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.addr.String(), func(t *testing.T) {
			if got := tt.addr.IsBroadcast(); got != tt.want {
				t.Errorf("IsBroadcast() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddressEqual(t *testing.T) {
	tests := []struct {
		a, b *Address
		want bool
	}{
		{
			&Address{Warband: "horde", Raider: "rictus"},
			&Address{Warband: "horde", Raider: "rictus"},
			true,
		},
		{
			&Address{Machine: "", Warband: "horde"},
			&Address{Machine: "local", Warband: "horde"},
			true,
		},
		{
			&Address{Warband: "horde", Raider: "rictus"},
			&Address{Warband: "horde", Raider: "nux"},
			false,
		},
		{
			&Address{Warband: "horde"},
			nil,
			false,
		},
	}

	for _, tt := range tests {
		name := "equal"
		if !tt.want {
			name = "not equal"
		}
		t.Run(name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAddress_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Address
		wantErr bool
	}{
		// Malformed: empty/whitespace variations
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			want:    &Address{Warband: "   "},
			wantErr: false, // whitespace-only warband is technically parsed
		},
		{
			name:    "just slash",
			input:   "/",
			wantErr: true,
		},
		{
			name:    "double slash",
			input:   "//",
			wantErr: true,
		},
		{
			name:    "triple slash",
			input:   "///",
			wantErr: true,
		},

		// Malformed: leading/trailing issues
		{
			name:    "leading slash",
			input:   "/raider",
			wantErr: true,
		},
		{
			name:    "leading slash with warband",
			input:   "/warband/raider",
			wantErr: true,
		},
		{
			name:  "trailing slash is broadcast",
			input: "warband/",
			want:  &Address{Warband: "warband"},
		},

		// Machine prefix edge cases
		{
			name:    "colon only",
			input:   ":",
			wantErr: true,
		},
		{
			name:    "colon with trailing slash",
			input:   ":/",
			wantErr: true,
		},
		{
			name:    "empty machine with colon",
			input:   ":warband/raider",
			wantErr: true,
		},
		{
			name:  "multiple colons in machine",
			input: "host:8080:warband/raider",
			want:  &Address{Machine: "host", Warband: "8080:warband", Raider: "raider"},
		},
		{
			name:  "colon in warband name",
			input: "machine:warband:port/raider",
			want:  &Address{Machine: "machine", Warband: "warband:port", Raider: "raider"},
		},

		// Multiple slash handling (SplitN behavior)
		{
			name:  "extra slashes in raider",
			input: "warband/pole/cat/extra",
			want:  &Address{Warband: "warband", Raider: "pole/cat/extra"},
		},
		{
			name:  "many path components",
			input: "a/b/c/d/e",
			want:  &Address{Warband: "a", Raider: "b/c/d/e"},
		},

		// Unicode handling
		{
			name:  "unicode warband name",
			input: "日本語/raider",
			want:  &Address{Warband: "日本語", Raider: "raider"},
		},
		{
			name:  "unicode raider name",
			input: "warband/工作者",
			want:  &Address{Warband: "warband", Raider: "工作者"},
		},
		{
			name:  "emoji in address",
			input: "🔧/🐱",
			want:  &Address{Warband: "🔧", Raider: "🐱"},
		},
		{
			name:  "unicode machine name",
			input: "マシン:warband/raider",
			want:  &Address{Machine: "マシン", Warband: "warband", Raider: "raider"},
		},

		// Long addresses
		{
			name:  "very long warband name",
			input: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789/raider",
			want:  &Address{Warband: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789", Raider: "raider"},
		},
		{
			name:  "very long raider name",
			input: "warband/abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789",
			want:  &Address{Warband: "warband", Raider: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"},
		},

		// Special characters
		{
			name:  "hyphen in names",
			input: "my-warband/my-raider",
			want:  &Address{Warband: "my-warband", Raider: "my-raider"},
		},
		{
			name:  "underscore in names",
			input: "my_rig/my_raider",
			want:  &Address{Warband: "my_rig", Raider: "my_raider"},
		},
		{
			name:  "dots in names",
			input: "my.warband/my.raider",
			want:  &Address{Warband: "my.warband", Raider: "my.raider"},
		},
		{
			name:  "mixed special chars",
			input: "warband-1_v2.0/raider-alpha_1.0",
			want:  &Address{Warband: "warband-1_v2.0", Raider: "raider-alpha_1.0"},
		},

		// Whitespace in components
		{
			name:  "space in warband name",
			input: "my warband/raider",
			want:  &Address{Warband: "my warband", Raider: "raider"},
		},
		{
			name:  "space in raider name",
			input: "warband/my raider",
			want:  &Address{Warband: "warband", Raider: "my raider"},
		},
		{
			name:  "leading space in warband",
			input: " warband/raider",
			want:  &Address{Warband: " warband", Raider: "raider"},
		},
		{
			name:  "trailing space in raider",
			input: "warband/raider ",
			want:  &Address{Warband: "warband", Raider: "raider "},
		},

		// Edge case: machine with no warband after colon
		{
			name:    "machine colon nothing",
			input:   "machine:",
			wantErr: true,
		},
		{
			name:    "machine colon slash",
			input:   "machine:/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseAddress(%q) expected error, got %+v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseAddress(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Machine != tt.want.Machine {
				t.Errorf("Machine = %q, want %q", got.Machine, tt.want.Machine)
			}
			if got.Warband != tt.want.Warband {
				t.Errorf("Warband = %q, want %q", got.Warband, tt.want.Warband)
			}
			if got.Raider != tt.want.Raider {
				t.Errorf("Raider = %q, want %q", got.Raider, tt.want.Raider)
			}
		})
	}
}

func TestMustParseAddress_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseAddress with empty string should panic")
		}
	}()
	MustParseAddress("")
}

func TestMustParseAddress_Valid(t *testing.T) {
	// Should not panic
	addr := MustParseAddress("warband/raider")
	if addr.Warband != "warband" || addr.Raider != "raider" {
		t.Errorf("MustParseAddress returned wrong address: %+v", addr)
	}
}

func TestAddressRigPath(t *testing.T) {
	tests := []struct {
		addr *Address
		want string
	}{
		{
			addr: &Address{Warband: "horde", Raider: "rictus"},
			want: "horde/rictus",
		},
		{
			addr: &Address{Warband: "horde"},
			want: "horde/",
		},
		{
			addr: &Address{Machine: "vm", Warband: "horde", Raider: "rictus"},
			want: "horde/rictus",
		},
		{
			addr: &Address{Warband: "a", Raider: "b/c/d"},
			want: "a/b/c/d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.addr.RigPath()
			if got != tt.want {
				t.Errorf("RigPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
