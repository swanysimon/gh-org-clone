package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const (
	exitSuccess     = 0
	exitRuntimeFail = 1
	exitUsage       = 2
	exitInterrupted = 130
)

var orgNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

type config struct {
	Org          string
	Root         string // org dir is <Root>/<Org>
	Concurrency  int
	Timeout      time.Duration // per subprocess
	MaxRepos     int           // gh --limit
	Protocol     string        // "ssh" | "https"
	IncludeForks bool
	Archive      bool
	Force        bool
	DryRun       bool
	Verbose      bool
}

type fileConfig struct {
	Root         *string `json:"root"`
	Concurrency  *int    `json:"concurrency"`
	Timeout      *string `json:"timeout"` // parsed with time.ParseDuration
	MaxRepos     *int    `json:"maxRepos"`
	Protocol     *string `json:"protocol"`
	IncludeForks *bool   `json:"includeForks"`
	Archive      *bool   `json:"archive"`
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gh-org-clone", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gh-org-clone [flags] <org>")
		fs.PrintDefaults()
	}

	cfg, err := resolveConfig(fs, args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	_ = cfg

	return exitSuccess
}

func defaultRoot() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "gh-org-clone")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return filepath.Join(home, ".local", "share", "gh-org-clone")
}

func defaultConfig() config {
	return config{
		Root:         defaultRoot(),
		Concurrency:  8,
		Timeout:      30 * time.Minute,
		MaxRepos:     10000,
		Protocol:     "ssh",
		IncludeForks: false,
		Archive:      true,
	}
}

// resolveConfig applies flags > env > file > defaults. fs.Visit reports only
// flags the caller actually typed, so an unset flag never clobbers a value
// already set by the env or the config file.
func resolveConfig(fs *flag.FlagSet, args []string, stderr io.Writer) (config, error) {
	var (
		root         string
		concurrency  int
		timeoutStr   string
		maxRepos     int
		protocol     string
		includeForks bool
		archive      bool
		force        bool
		dryRun       bool
		verbose      bool
		configPath   string
	)
	fs.StringVar(&root, "root", "", "root directory for cloned orgs")
	fs.IntVar(&concurrency, "concurrency", 0, "number of repos to sync in parallel")
	fs.StringVar(&timeoutStr, "timeout", "", "per-subprocess timeout (e.g. 30m)")
	fs.IntVar(&maxRepos, "max-repos", 0, "maximum repos to list from the org (gh --limit)")
	fs.StringVar(&protocol, "protocol", "", "clone protocol: ssh or https")
	fs.BoolVar(&includeForks, "include-forks", false, "include forked repos")
	fs.BoolVar(&archive, "archive", false, "tarball archived repos and remove their clones")
	fs.BoolVar(&force, "force", false, "ignore stored pushedAt and re-sync every repo")
	fs.BoolVar(&dryRun, "dry-run", false, "print the planned actions without doing them")
	fs.BoolVar(&verbose, "v", false, "verbose output")
	fs.StringVar(&configPath, "config", "", "path to a JSON config file")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return config{}, fmt.Errorf("expected exactly one org argument, got %d", fs.NArg())
	}

	cfg := defaultConfig()

	fc, err := loadFileConfig(resolveConfigPath(configPath))
	if err != nil {
		return config{}, err
	}
	if fc != nil {
		if fc.Root != nil {
			cfg.Root = *fc.Root
		}
		if fc.Concurrency != nil {
			cfg.Concurrency = *fc.Concurrency
		}
		if fc.Timeout != nil {
			d, err := time.ParseDuration(*fc.Timeout)
			if err != nil {
				return config{}, fmt.Errorf("config file: invalid timeout %q: %w", *fc.Timeout, err)
			}
			cfg.Timeout = d
		}
		if fc.MaxRepos != nil {
			cfg.MaxRepos = *fc.MaxRepos
		}
		if fc.Protocol != nil {
			cfg.Protocol = *fc.Protocol
		}
		if fc.IncludeForks != nil {
			cfg.IncludeForks = *fc.IncludeForks
		}
		if fc.Archive != nil {
			cfg.Archive = *fc.Archive
		}
	}

	if err := overlayEnv(&cfg); err != nil {
		return config{}, err
	}

	var flagErr error
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "root":
			cfg.Root = root
		case "concurrency":
			cfg.Concurrency = concurrency
		case "timeout":
			d, err := time.ParseDuration(timeoutStr)
			if err != nil {
				flagErr = fmt.Errorf("-timeout: invalid duration %q: %w", timeoutStr, err)
				return
			}
			cfg.Timeout = d
		case "max-repos":
			cfg.MaxRepos = maxRepos
		case "protocol":
			cfg.Protocol = protocol
		case "include-forks":
			cfg.IncludeForks = includeForks
		case "archive":
			cfg.Archive = archive
		}
	})
	if flagErr != nil {
		return config{}, flagErr
	}

	cfg.Org = fs.Arg(0)
	cfg.Force = force
	cfg.DryRun = dryRun
	cfg.Verbose = verbose

	if err := validateConfig(cfg); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("GH_ORG_CLONE_CONFIG"); env != "" {
		return env
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "gh-org-clone", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return filepath.Join(home, ".config", "gh-org-clone", "config.json")
}

// loadFileConfig returns nil, nil when the file does not exist. Any other
// read or parse failure is a hard error — never guess at config intent.
func loadFileConfig(path string) (*fileConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}
	defer f.Close()

	var fc fileConfig
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fc); err != nil {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}
	return &fc, nil
}

func overlayEnv(cfg *config) error {
	if v := os.Getenv("GH_ORG_CLONE_ROOT"); v != "" {
		cfg.Root = v
	}
	if v := os.Getenv("GH_ORG_CLONE_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("GH_ORG_CLONE_CONCURRENCY: invalid integer %q: %w", v, err)
		}
		cfg.Concurrency = n
	}
	if v := os.Getenv("GH_ORG_CLONE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("GH_ORG_CLONE_TIMEOUT: invalid duration %q: %w", v, err)
		}
		cfg.Timeout = d
	}
	if v := os.Getenv("GH_ORG_CLONE_MAX_REPOS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("GH_ORG_CLONE_MAX_REPOS: invalid integer %q: %w", v, err)
		}
		cfg.MaxRepos = n
	}
	if v := os.Getenv("GH_ORG_CLONE_PROTOCOL"); v != "" {
		cfg.Protocol = v
	}
	if v := os.Getenv("GH_ORG_CLONE_INCLUDE_FORKS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("GH_ORG_CLONE_INCLUDE_FORKS: invalid bool %q: %w", v, err)
		}
		cfg.IncludeForks = b
	}
	if v := os.Getenv("GH_ORG_CLONE_ARCHIVE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("GH_ORG_CLONE_ARCHIVE: invalid bool %q: %w", v, err)
		}
		cfg.Archive = b
	}
	return nil
}

func validateConfig(cfg config) error {
	if cfg.Org == "" {
		return fmt.Errorf("org must not be empty")
	}
	if !orgNamePattern.MatchString(cfg.Org) {
		return fmt.Errorf("org %q is not a valid GitHub org name", cfg.Org)
	}
	if cfg.Concurrency < 1 {
		return fmt.Errorf("concurrency must be >= 1, got %d", cfg.Concurrency)
	}
	if cfg.MaxRepos < 1 {
		return fmt.Errorf("max-repos must be >= 1, got %d", cfg.MaxRepos)
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be > 0, got %s", cfg.Timeout)
	}
	if cfg.Protocol != "ssh" && cfg.Protocol != "https" {
		return fmt.Errorf("protocol must be ssh or https, got %q", cfg.Protocol)
	}
	if !filepath.IsAbs(cfg.Root) {
		return fmt.Errorf("root must be an absolute path, got %q", cfg.Root)
	}
	return nil
}
