//go:build integration

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cleanHDEnv returns os.Environ() with all HD_* variables removed.
// This ensures tests don't inherit stale role environment from CI or previous tests.
func cleanHDEnv() []string {
	var clean []string
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "HD_") {
			clean = append(clean, env)
		}
	}
	return clean
}

// TestRoleHomeE2E validates that hd role home returns correct paths
// for all role types after a full hd install.
func TestRoleHomeE2E(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "warchief",
			args:     []string{"role", "home", "warchief"},
			expected: filepath.Join(hqPath, "warchief"),
		},
		{
			name:     "shaman",
			args:     []string{"role", "home", "shaman"},
			expected: filepath.Join(hqPath, "shaman"),
		},
		{
			name:     "witness",
			args:     []string{"role", "home", "witness", "--warband", rigName},
			expected: filepath.Join(hqPath, rigName, "witness"),
		},
		{
			name:     "forge",
			args:     []string{"role", "home", "forge", "--warband", rigName},
			expected: filepath.Join(hqPath, rigName, "forge", "warband"),
		},
		{
			name:     "raider",
			args:     []string{"role", "home", "raider", "--warband", rigName, "--raider", "Toast"},
			expected: filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
		},
		{
			name:     "clan",
			args:     []string{"role", "home", "clan", "--warband", rigName, "--raider", "worker1"},
			expected: filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, tt.args...)
			cmd.Dir = hqPath
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			// Use Output() to only capture stdout (warnings go to stderr)
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("hd %v failed: %v\nOutput: %s", tt.args, err, output)
			}

			got := strings.TrimSpace(string(output))
			if got != tt.expected {
				t.Errorf("hd %v = %q, want %q", tt.args, got, tt.expected)
			}
		})
	}
}

// TestRoleHomeMissingFlags validates that hd role home fails when required flags are missing.
func TestRoleHomeMissingFlags(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "witness without --warband",
			args: []string{"role", "home", "witness"},
		},
		{
			name: "forge without --warband",
			args: []string{"role", "home", "forge"},
		},
		{
			name: "raider without --warband",
			args: []string{"role", "home", "raider", "--raider", "Toast"},
		},
		{
			name: "raider without --raider",
			args: []string{"role", "home", "raider", "--warband", "testrig"},
		},
		{
			name: "clan without --warband",
			args: []string{"role", "home", "clan", "--raider", "worker1"},
		},
		{
			name: "clan without --raider",
			args: []string{"role", "home", "clan", "--warband", "testrig"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, tt.args...)
			cmd.Dir = hqPath
			// Use cleanHDEnv to ensure no stale HD_* vars affect the test
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Errorf("hd %v should have failed but succeeded with output: %s", tt.args, output)
			}
		})
	}
}


// TestRoleHomeCwdDetection validates hd role home without arguments detects role from cwd.
func TestRoleHomeCwdDetection(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create warband directory structure for cwd detection
	dirs := []string{
		filepath.Join(hqPath, rigName, "witness"),
		filepath.Join(hqPath, rigName, "forge", "warband"),
		filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
		filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name     string
		cwd      string
		expected string
	}{
		{
			name:     "warchief from warchief dir",
			cwd:      filepath.Join(hqPath, "warchief"),
			expected: filepath.Join(hqPath, "warchief"),
		},
		{
			name:     "shaman from shaman dir",
			cwd:      filepath.Join(hqPath, "shaman"),
			expected: filepath.Join(hqPath, "shaman"),
		},
		{
			name:     "witness from witness dir",
			cwd:      filepath.Join(hqPath, rigName, "witness"),
			expected: filepath.Join(hqPath, rigName, "witness"),
		},
		{
			name:     "forge from forge/warband dir",
			cwd:      filepath.Join(hqPath, rigName, "forge", "warband"),
			expected: filepath.Join(hqPath, rigName, "forge", "warband"),
		},
		{
			name:     "raider from raiders/Toast/warband dir",
			cwd:      filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			expected: filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
		},
		{
			name:     "clan from clan/worker1/warband dir",
			cwd:      filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
			expected: filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "home")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hd role home failed: %v\nOutput: %s", err, output)
			}

			got := strings.TrimSpace(string(output))
			if got != tt.expected {
				t.Errorf("hd role home = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestRoleEnvCwdDetection validates hd role env without arguments detects role from cwd.
func TestRoleEnvCwdDetection(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create warband directory structure for cwd detection
	dirs := []string{
		filepath.Join(hqPath, rigName, "witness"),
		filepath.Join(hqPath, rigName, "forge", "warband"),
		filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
		filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name string
		cwd  string
		want []string
	}{
		{
			name: "warchief from warchief dir",
			cwd:  filepath.Join(hqPath, "warchief"),
			want: []string{
				"export HD_ROLE=warchief",
				"export HD_ROLE_HOME=" + filepath.Join(hqPath, "warchief"),
			},
		},
		{
			name: "shaman from shaman dir",
			cwd:  filepath.Join(hqPath, "shaman"),
			want: []string{
				"export HD_ROLE=shaman",
				"export HD_ROLE_HOME=" + filepath.Join(hqPath, "shaman"),
			},
		},
		{
			name: "witness from witness dir",
			cwd:  filepath.Join(hqPath, rigName, "witness"),
			want: []string{
				"export HD_ROLE=witness",
				"export HD_WARBAND=" + rigName,
				"export BD_ACTOR=" + rigName + "/witness",
				"export HD_ROLE_HOME=" + filepath.Join(hqPath, rigName, "witness"),
			},
		},
		{
			name: "forge from forge/warband dir",
			cwd:  filepath.Join(hqPath, rigName, "forge", "warband"),
			want: []string{
				"export HD_ROLE=forge",
				"export HD_WARBAND=" + rigName,
				"export BD_ACTOR=" + rigName + "/forge",
				"export HD_ROLE_HOME=" + filepath.Join(hqPath, rigName, "forge", "warband"),
			},
		},
		{
			name: "raider from raiders/Toast/warband dir",
			cwd:  filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			want: []string{
				"export HD_ROLE=raider",
				"export HD_WARBAND=" + rigName,
				"export HD_RAIDER=Toast",
				"export BD_ACTOR=" + rigName + "/raiders/Toast",
				"export HD_ROLE_HOME=" + filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			},
		},
		{
			name: "clan from clan/worker1/warband dir",
			cwd:  filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
			want: []string{
				"export HD_ROLE=clan",
				"export HD_WARBAND=" + rigName,
				"export HD_CLAN=worker1",
				"export BD_ACTOR=" + rigName + "/clan/worker1",
				"export HD_ROLE_HOME=" + filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "env")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hd role env failed: %v\nOutput: %s", err, output)
			}

			got := string(output)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("output missing %q\ngot: %s", w, got)
				}
			}
		})
	}
}

// TestRoleListE2E validates hd role list shows all roles.
func TestRoleListE2E(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command(gtBinary, "role", "list")
	cmd.Dir = hqPath
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd role list failed: %v\nOutput: %s", err, output)
	}

	got := string(output)

	// Check header
	if !strings.Contains(got, "Available roles:") {
		t.Errorf("output missing 'Available roles:' header\ngot: %s", got)
	}

	// Check all roles are listed
	roles := []string{"warchief", "shaman", "witness", "forge", "raider", "clan"}
	for _, role := range roles {
		if !strings.Contains(got, role) {
			t.Errorf("output missing role %q\ngot: %s", role, got)
		}
	}
}

// TestRoleShowE2E validates hd role show displays correct role info.
func TestRoleShowE2E(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create warband directory structure for cwd detection
	dirs := []string{
		filepath.Join(hqPath, rigName, "witness"),
		filepath.Join(hqPath, rigName, "forge", "warband"),
		filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
		filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name       string
		cwd        string
		wantRole   string
		wantSource string
		wantHome   string
		wantRig    string
		wantWorker string
	}{
		{
			name:       "warchief from warchief dir",
			cwd:        filepath.Join(hqPath, "warchief"),
			wantRole:   "warchief",
			wantSource: "cwd",
			wantHome:   filepath.Join(hqPath, "warchief"),
		},
		{
			name:       "shaman from shaman dir",
			cwd:        filepath.Join(hqPath, "shaman"),
			wantRole:   "shaman",
			wantSource: "cwd",
			wantHome:   filepath.Join(hqPath, "shaman"),
		},
		{
			name:       "witness from witness dir",
			cwd:        filepath.Join(hqPath, rigName, "witness"),
			wantRole:   "witness",
			wantSource: "cwd",
			wantHome:   filepath.Join(hqPath, rigName, "witness"),
			wantRig:    rigName,
		},
		{
			name:       "raider from raiders/Toast/warband dir",
			cwd:        filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			wantRole:   "raider",
			wantSource: "cwd",
			wantHome:   filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			wantRig:    rigName,
			wantWorker: "Toast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "show")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hd role show failed: %v\nOutput: %s", err, output)
			}

			got := string(output)

			if !strings.Contains(got, tt.wantRole) {
				t.Errorf("output missing role %q\ngot: %s", tt.wantRole, got)
			}

			if !strings.Contains(got, "Source: "+tt.wantSource) {
				t.Errorf("output missing 'Source: %s'\ngot: %s", tt.wantSource, got)
			}

			if !strings.Contains(got, "Home: "+tt.wantHome) {
				t.Errorf("output missing 'Home: %s'\ngot: %s", tt.wantHome, got)
			}

			if tt.wantRig != "" {
				if !strings.Contains(got, "Warband: "+tt.wantRig) {
					t.Errorf("output missing 'Warband: %s'\ngot: %s", tt.wantRig, got)
				}
			}

			if tt.wantWorker != "" {
				if !strings.Contains(got, "Worker: "+tt.wantWorker) {
					t.Errorf("output missing 'Worker: %s'\ngot: %s", tt.wantWorker, got)
				}
			}
		})
	}
}

// TestRoleShowMismatch validates hd role show displays mismatch warning.
func TestRoleShowMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	// Run from warchief dir but set HD_ROLE to shaman
	cmd = exec.Command(gtBinary, "role", "show")
	cmd.Dir = filepath.Join(hqPath, "warchief")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir, "HD_ROLE=shaman")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd role show failed: %v\nOutput: %s", err, output)
	}

	got := string(output)

	// HD_ROLE takes precedence, so role should be shaman
	if !strings.Contains(got, "shaman") {
		t.Errorf("should show 'shaman' from HD_ROLE, got: %s", got)
	}

	// Source should be env
	if !strings.Contains(got, "Source: env") {
		t.Errorf("source should be 'env', got: %s", got)
	}

	// Should show mismatch warning
	if !strings.Contains(got, "ROLE MISMATCH") {
		t.Errorf("should show ROLE MISMATCH warning\ngot: %s", got)
	}

	// Should show both the env value and cwd suggestion
	if !strings.Contains(got, "HD_ROLE=shaman") {
		t.Errorf("should show HD_ROLE value\ngot: %s", got)
	}

	if !strings.Contains(got, "cwd suggests: warchief") {
		t.Errorf("should show cwd suggestion\ngot: %s", got)
	}
}

// TestRoleDetectE2E validates hd role detect uses cwd and ignores HD_ROLE.
func TestRoleDetectE2E(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create warband directory structure for cwd detection
	dirs := []string{
		filepath.Join(hqPath, rigName, "witness"),
		filepath.Join(hqPath, rigName, "forge", "warband"),
		filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
		filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name        string
		cwd         string
		wantRole    string
		wantRig     string
		wantWorker  string
	}{
		{
			name:     "warchief from warchief dir",
			cwd:      filepath.Join(hqPath, "warchief"),
			wantRole: "warchief",
		},
		{
			name:     "shaman from shaman dir",
			cwd:      filepath.Join(hqPath, "shaman"),
			wantRole: "shaman",
		},
		{
			name:     "witness from witness dir",
			cwd:      filepath.Join(hqPath, rigName, "witness"),
			wantRole: "witness",
			wantRig:  rigName,
		},
		{
			name:     "forge from forge/warband dir",
			cwd:      filepath.Join(hqPath, rigName, "forge", "warband"),
			wantRole: "forge",
			wantRig:  rigName,
		},
		{
			name:       "raider from raiders/Toast/warband dir",
			cwd:        filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			wantRole:   "raider",
			wantRig:    rigName,
			wantWorker: "Toast",
		},
		{
			name:       "clan from clan/worker1/warband dir",
			cwd:        filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
			wantRole:   "clan",
			wantRig:    rigName,
			wantWorker: "worker1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "detect")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hd role detect failed: %v\nOutput: %s", err, output)
			}

			got := string(output)

			// Check role is detected
			if !strings.Contains(got, tt.wantRole) {
				t.Errorf("output missing role %q\ngot: %s", tt.wantRole, got)
			}

			// Check "(from cwd)" marker
			if !strings.Contains(got, "(from cwd)") {
				t.Errorf("output missing '(from cwd)' marker\ngot: %s", got)
			}

			// Check warband if expected
			if tt.wantRig != "" {
				if !strings.Contains(got, "Warband: "+tt.wantRig) {
					t.Errorf("output missing 'Warband: %s'\ngot: %s", tt.wantRig, got)
				}
			}

			// Check worker if expected
			if tt.wantWorker != "" {
				if !strings.Contains(got, "Worker: "+tt.wantWorker) {
					t.Errorf("output missing 'Worker: %s'\ngot: %s", tt.wantWorker, got)
				}
			}
		})
	}
}

// TestRoleDetectIgnoresGTRole validates hd role detect ignores HD_ROLE env var.
func TestRoleDetectIgnoresGTRole(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	// Run from warchief dir but set HD_ROLE to shaman
	cmd = exec.Command(gtBinary, "role", "detect")
	cmd.Dir = filepath.Join(hqPath, "warchief")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir, "HD_ROLE=shaman")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd role detect failed: %v\nOutput: %s", err, output)
	}

	got := string(output)

	// Should detect warchief from cwd, not shaman from env
	if !strings.Contains(got, "warchief") {
		t.Errorf("should detect 'warchief' from cwd, got: %s", got)
	}

	// Should show mismatch warning
	if !strings.Contains(got, "Mismatch") {
		t.Errorf("should show mismatch warning when HD_ROLE disagrees\ngot: %s", got)
	}

	if !strings.Contains(got, "HD_ROLE=shaman") {
		t.Errorf("should show conflicting HD_ROLE value\ngot: %s", got)
	}
}

// TestRoleDetectInvalidPaths validates detection behavior for incomplete/invalid paths.
func TestRoleDetectInvalidPaths(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create incomplete directory structures
	dirs := []string{
		filepath.Join(hqPath, rigName),                        // warband root
		filepath.Join(hqPath, rigName, "raiders"),            // raiders without name
		filepath.Join(hqPath, rigName, "clan"),                // clan without name
		filepath.Join(hqPath, rigName, "forge"),            // forge without /warband
		filepath.Join(hqPath, rigName, "witness"),             // witness (valid - no /warband needed)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name     string
		cwd      string
		wantRole string
	}{
		{
			name:     "warband root returns unknown",
			cwd:      filepath.Join(hqPath, rigName),
			wantRole: "unknown",
		},
		{
			name:     "raiders without name returns unknown",
			cwd:      filepath.Join(hqPath, rigName, "raiders"),
			wantRole: "unknown",
		},
		{
			name:     "clan without name returns unknown",
			cwd:      filepath.Join(hqPath, rigName, "clan"),
			wantRole: "unknown",
		},
		{
			name:     "forge without /warband still detects forge",
			cwd:      filepath.Join(hqPath, rigName, "forge"),
			wantRole: "forge",
		},
		{
			name:     "witness without /warband detects witness",
			cwd:      filepath.Join(hqPath, rigName, "witness"),
			wantRole: "witness",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "detect")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hd role detect failed: %v\nOutput: %s", err, output)
			}

			got := string(output)
			if !strings.Contains(got, tt.wantRole) {
				t.Errorf("expected role %q\ngot: %s", tt.wantRole, got)
			}
		})
	}
}

// TestRoleEnvIncompleteEnvVars validates hd role env fills gaps from cwd with warning.
func TestRoleEnvIncompleteEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create warband directory structure for cwd detection
	dirs := []string{
		filepath.Join(hqPath, rigName, "witness"),
		filepath.Join(hqPath, rigName, "forge", "warband"),
		filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
		filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name       string
		cwd        string
		envVars    []string
		wantExport []string // Expected exports in stdout
		wantStderr string   // Expected warning in stderr
	}{
		{
			name: "HD_ROLE=witness without HD_WARBAND, filled from cwd",
			cwd:  filepath.Join(hqPath, rigName, "witness"),
			envVars: []string{"HD_ROLE=witness"},
			wantExport: []string{
				"export HD_ROLE=witness",
				"export HD_WARBAND=" + rigName,
				"export BD_ACTOR=" + rigName + "/witness",
			},
			wantStderr: "env vars incomplete",
		},
		{
			name: "HD_ROLE=forge without HD_WARBAND, filled from cwd",
			cwd:  filepath.Join(hqPath, rigName, "forge", "warband"),
			envVars: []string{"HD_ROLE=forge"},
			wantExport: []string{
				"export HD_ROLE=forge",
				"export HD_WARBAND=" + rigName,
				"export BD_ACTOR=" + rigName + "/forge",
			},
			wantStderr: "env vars incomplete",
		},
		{
			name: "HD_ROLE=raider without HD_WARBAND or HD_RAIDER, filled from cwd",
			cwd:  filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			envVars: []string{"HD_ROLE=raider"},
			wantExport: []string{
				"export HD_ROLE=raider",
				"export HD_WARBAND=" + rigName,
				"export HD_RAIDER=Toast",
				"export BD_ACTOR=" + rigName + "/raiders/Toast",
			},
			wantStderr: "env vars incomplete",
		},
		{
			name: "HD_ROLE=raider with HD_WARBAND but no HD_RAIDER, filled from cwd",
			cwd:  filepath.Join(hqPath, rigName, "raiders", "Toast", "warband"),
			envVars: []string{"HD_ROLE=raider", "HD_WARBAND=" + rigName},
			wantExport: []string{
				"export HD_ROLE=raider",
				"export HD_WARBAND=" + rigName,
				"export HD_RAIDER=Toast",
				"export BD_ACTOR=" + rigName + "/raiders/Toast",
			},
			wantStderr: "env vars incomplete",
		},
		{
			name: "HD_ROLE=clan without HD_WARBAND or HD_CLAN, filled from cwd",
			cwd:  filepath.Join(hqPath, rigName, "clan", "worker1", "warband"),
			envVars: []string{"HD_ROLE=clan"},
			wantExport: []string{
				"export HD_ROLE=clan",
				"export HD_WARBAND=" + rigName,
				"export HD_CLAN=worker1",
				"export BD_ACTOR=" + rigName + "/clan/worker1",
			},
			wantStderr: "env vars incomplete",
		},
		{
			name: "Complete env vars - no warning",
			cwd:  filepath.Join(hqPath, rigName, "witness"),
			envVars: []string{"HD_ROLE=witness", "HD_WARBAND=" + rigName},
			wantExport: []string{
				"export HD_ROLE=witness",
				"export HD_WARBAND=" + rigName,
				"export BD_ACTOR=" + rigName + "/witness",
			},
			wantStderr: "", // No warning expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "env")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
			cmd.Env = append(cmd.Env, tt.envVars...)

			// Use CombinedOutput to see stderr for debugging, but separate stdout/stderr
			stdout, _ := cmd.Output() // Only stdout
			// Re-run to get stderr
			cmd2 := exec.Command(gtBinary, "role", "env")
			cmd2.Dir = tt.cwd
			cmd2.Env = append(cleanHDEnv(), "HOME="+tmpDir)
			cmd2.Env = append(cmd2.Env, tt.envVars...)
			combined, _ := cmd2.CombinedOutput()
			stderr := strings.TrimPrefix(string(combined), string(stdout))

			// Check expected exports in stdout
			gotStdout := string(stdout)
			for _, w := range tt.wantExport {
				if !strings.Contains(gotStdout, w) {
					t.Errorf("stdout missing %q\ngot: %s", w, gotStdout)
				}
			}

			// Check expected warning in stderr
			if tt.wantStderr != "" {
				if !strings.Contains(stderr, tt.wantStderr) {
					t.Errorf("stderr should contain %q\ngot: %s\ncombined: %s", tt.wantStderr, stderr, combined)
				}
			} else {
				if strings.Contains(stderr, "incomplete") {
					t.Errorf("stderr should not contain 'incomplete' warning\ngot: %s", stderr)
				}
			}
		})
	}
}

// TestRoleEnvCwdMismatchFromIncompleteDir validates warnings when in incomplete directories.
func TestRoleEnvCwdMismatchFromIncompleteDir(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create incomplete directory structures (missing /warband)
	dirs := []string{
		filepath.Join(hqPath, rigName, "forge"),            // forge without /warband
		filepath.Join(hqPath, rigName, "raiders", "Toast"),   // raider without /warband
		filepath.Join(hqPath, rigName, "clan", "worker1"),     // clan without /warband
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name       string
		cwd        string
		envVars    []string
		wantStderr string // Expected warning about cwd mismatch
	}{
		{
			name: "forge without /warband shows cwd mismatch",
			cwd:  filepath.Join(hqPath, rigName, "forge"),
			envVars: []string{"HD_ROLE=forge", "HD_WARBAND=" + rigName},
			wantStderr: "cwd",
		},
		{
			name: "raider without /warband shows cwd mismatch",
			cwd:  filepath.Join(hqPath, rigName, "raiders", "Toast"),
			envVars: []string{"HD_ROLE=raider", "HD_WARBAND=" + rigName, "HD_RAIDER=Toast"},
			wantStderr: "cwd",
		},
		{
			name: "clan without /warband shows cwd mismatch",
			cwd:  filepath.Join(hqPath, rigName, "clan", "worker1"),
			envVars: []string{"HD_ROLE=clan", "HD_WARBAND=" + rigName, "HD_CLAN=worker1"},
			wantStderr: "cwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "env")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
			cmd.Env = append(cmd.Env, tt.envVars...)

			combined, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hd role env failed: %v\nOutput: %s", err, combined)
			}

			// Check for cwd mismatch warning
			if !strings.Contains(string(combined), tt.wantStderr) {
				t.Errorf("output should contain %q warning\ngot: %s", tt.wantStderr, combined)
			}
		})
	}
}

// TestRoleHomeInvalidPaths validates that commands fail gracefully for incomplete paths.
func TestRoleHomeInvalidPaths(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	gtBinary := buildGT(t)

	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	rigName := "testrig"

	// Create incomplete directory structures
	dirs := []string{
		filepath.Join(hqPath, rigName),
		filepath.Join(hqPath, rigName, "raiders"),
		filepath.Join(hqPath, rigName, "clan"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name      string
		cwd       string
		shouldErr bool
	}{
		{
			name:      "warband root fails",
			cwd:       filepath.Join(hqPath, rigName),
			shouldErr: true,
		},
		{
			name:      "raiders without name fails",
			cwd:       filepath.Join(hqPath, rigName, "raiders"),
			shouldErr: true,
		},
		{
			name:      "clan without name fails",
			cwd:       filepath.Join(hqPath, rigName, "clan"),
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(gtBinary, "role", "home")
			cmd.Dir = tt.cwd
			cmd.Env = append(cleanHDEnv(), "HOME="+tmpDir)

			_, err := cmd.CombinedOutput()
			if tt.shouldErr && err == nil {
				t.Errorf("expected error but command succeeded")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("expected success but got error: %v", err)
			}
		})
	}
}

