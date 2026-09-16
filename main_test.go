package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func newFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("gh-org-clone", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func TestConfigDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HOME", t.TempDir())

	cfg, err := resolveConfig(newFlagSet(), []string{"myorg"}, os.Stderr)
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}

	want := defaultConfig()
	want.Org = "myorg"
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestConfigPrecedence(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HOME", t.TempDir())

	fileDir := t.TempDir()
	configPath := filepath.Join(fileDir, "config.json")
	fileCfg := `{"concurrency": 4, "protocol": "https", "maxRepos": 500}`
	if err := os.WriteFile(configPath, []byte(fileCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// (a) file beats default: concurrency, protocol, maxRepos come from file.
	cfg, err := resolveConfig(newFlagSet(), []string{"-config", configPath, "myorg"}, os.Stderr)
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if cfg.Concurrency != 4 || cfg.Protocol != "https" || cfg.MaxRepos != 500 {
		t.Fatalf("file did not win over default: %+v", cfg)
	}
	// Root was not set in the file, so it must remain the default.
	if cfg.Root != defaultRoot() {
		t.Fatalf("unset file field clobbered default root: got %q", cfg.Root)
	}

	// (b) env beats file: override concurrency and protocol via env.
	t.Setenv("GH_ORG_CLONE_CONCURRENCY", "6")
	t.Setenv("GH_ORG_CLONE_PROTOCOL", "ssh")
	cfg, err = resolveConfig(newFlagSet(), []string{"-config", configPath, "myorg"}, os.Stderr)
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if cfg.Concurrency != 6 || cfg.Protocol != "ssh" {
		t.Fatalf("env did not win over file: %+v", cfg)
	}
	// maxRepos was not overridden by env, so the file's value must survive.
	if cfg.MaxRepos != 500 {
		t.Fatalf("env overlay clobbered unrelated file field: got %d", cfg.MaxRepos)
	}

	// (c) explicit flag beats env.
	cfg, err = resolveConfig(newFlagSet(), []string{"-config", configPath, "-concurrency", "9", "myorg"}, os.Stderr)
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if cfg.Concurrency != 9 {
		t.Fatalf("flag did not win over env: %+v", cfg)
	}
	// protocol flag not passed: env value must survive untouched.
	if cfg.Protocol != "ssh" {
		t.Fatalf("unset flag clobbered env value: got %q", cfg.Protocol)
	}

	// (d) a flag not passed must not clobber the file's value (the fs.Visit regression).
	cfg, err = resolveConfig(newFlagSet(), []string{"-config", configPath, "-include-forks", "myorg"}, os.Stderr)
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if cfg.MaxRepos != 500 {
		t.Fatalf("unrelated flag parse clobbered file value: got %d", cfg.MaxRepos)
	}
	if !cfg.IncludeForks {
		t.Fatalf("explicitly passed flag was not applied")
	}
}

func TestConfigRejects(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HOME", t.TempDir())

	badFile := filepath.Join(t.TempDir(), "bad.json")
	unknownKeyFile := filepath.Join(t.TempDir(), "unknown.json")
	if err := os.WriteFile(badFile, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unknownKeyFile, []byte(`{"nope": true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"no positional arg", []string{}},
		{"two positional args", []string{"a", "b"}},
		{"bad concurrency", []string{"-concurrency", "0", "myorg"}},
		{"bad protocol", []string{"-protocol", "ftp", "myorg"}},
		{"bad timeout", []string{"-timeout", "banana", "myorg"}},
		{"negative max-repos", []string{"-max-repos", "-1", "myorg"}},
		{"malformed config file", []string{"-config", badFile, "myorg"}},
		{"unknown config key", []string{"-config", unknownKeyFile, "myorg"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveConfig(newFlagSet(), tc.args, os.Stderr); err == nil {
				t.Fatalf("expected an error, got none")
			}
		})
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"XDG_DATA_HOME", "XDG_CONFIG_HOME", "GH_ORG_CLONE_CONFIG",
		"GH_ORG_CLONE_ROOT", "GH_ORG_CLONE_CONCURRENCY", "GH_ORG_CLONE_TIMEOUT",
		"GH_ORG_CLONE_MAX_REPOS", "GH_ORG_CLONE_PROTOCOL", "GH_ORG_CLONE_INCLUDE_FORKS",
		"GH_ORG_CLONE_ARCHIVE",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}
