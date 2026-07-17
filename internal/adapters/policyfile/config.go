// Package policyfile loads the installer-owned permission policy from a TOML
// file and persists temporal arming grants. It is an adapter at the edge of the
// hexagon: it maps a file format into core domain types (PolicyRules, ArmState)
// and never lets the core depend on TOML or the filesystem.
//
// Responsibility rests with whoever installs hyprvalet: the shipped defaults are
// conservative, and the policy file — which the installer owns and edits — is
// authoritative. A malformed file fails closed (an error), never a silent
// permissive fallback.
package policyfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/xebastian153/hyprvalet/internal/core"
)

// defaultArmMinutes is the arming window used when neither a capability rule nor
// the config sets one.
const defaultArmMinutes = 5

// ConfigPath returns the policy file location: $XDG_CONFIG_HOME/hyprvalet/
// policy.toml, falling back to ~/.config/hyprvalet/policy.toml.
func ConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hyprvalet", "policy.toml")
}

// DefaultRules is the conservative policy shipped when no config file exists:
// reversible (Safe) actions run, disruptive (Confirm) actions ask, Forbidden
// actions never run, and anything otherwise unclassified asks. The installer
// overrides any of this by writing a config file.
func DefaultRules() core.PolicyRules {
	return core.PolicyRules{
		Default: core.Rule{Decision: core.DecisionAsk},
		ByRisk: map[core.Risk]core.Rule{
			core.RiskSafe:      {Decision: core.DecisionAllow},
			core.RiskConfirm:   {Decision: core.DecisionAsk},
			core.RiskForbidden: {Decision: core.DecisionDeny},
		},
		ByAccess:      map[core.AccessKind]core.Rule{},
		ByCapID:       map[string]core.Rule{},
		DefaultArmFor: defaultArmMinutes * time.Minute,
	}
}

// fileConfig is the on-disk TOML shape. Coarse levels (default, risk, access)
// are just a decision word; the capability level is a full table so it can also
// opt into arming. Keeping this separate from core.PolicyRules is deliberate:
// the file format is an adapter concern, not a domain type.
type fileConfig struct {
	Default           string             `toml:"default"`
	DefaultArmMinutes int                `toml:"default_arm_minutes"`
	Risk              map[string]string  `toml:"risk"`
	Access            map[string]string  `toml:"access"`
	Capability        map[string]capRule `toml:"capability"`
}

type capRule struct {
	Decision       string `toml:"decision"`
	RequiresArming bool   `toml:"requires_arming"`
	ArmMinutes     int    `toml:"arm_minutes"`
}

// Load reads the policy file at path. A missing file returns DefaultRules() so
// the tool works out of the box. A file that exists but is malformed, or names
// an unknown decision/tier/access, returns an error — Load never falls back to a
// permissive default, so a broken config fails closed.
func Load(path string) (core.PolicyRules, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultRules(), nil
	}
	if err != nil {
		return core.PolicyRules{}, err
	}
	var fc fileConfig
	if err := toml.Unmarshal(data, &fc); err != nil {
		return core.PolicyRules{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return fc.toRules(path)
}

// toRules overlays the file onto the shipped defaults: unspecified levels keep
// their conservative baseline, and every value the file does set is validated.
func (fc fileConfig) toRules(path string) (core.PolicyRules, error) {
	rules := DefaultRules()

	if fc.Default != "" {
		d, err := parseDecision(fc.Default)
		if err != nil {
			return core.PolicyRules{}, fmt.Errorf("%s: default: %w", path, err)
		}
		rules.Default = core.Rule{Decision: d}
	}
	if fc.DefaultArmMinutes > 0 {
		rules.DefaultArmFor = time.Duration(fc.DefaultArmMinutes) * time.Minute
	}

	for name, dec := range fc.Risk {
		risk, err := parseRisk(name)
		if err != nil {
			return core.PolicyRules{}, fmt.Errorf("%s: [risk]: %w", path, err)
		}
		d, err := parseDecision(dec)
		if err != nil {
			return core.PolicyRules{}, fmt.Errorf("%s: risk.%s: %w", path, name, err)
		}
		rules.ByRisk[risk] = core.Rule{Decision: d}
	}

	for name, dec := range fc.Access {
		access, err := parseAccess(name)
		if err != nil {
			return core.PolicyRules{}, fmt.Errorf("%s: [access]: %w", path, err)
		}
		d, err := parseDecision(dec)
		if err != nil {
			return core.PolicyRules{}, fmt.Errorf("%s: access.%s: %w", path, name, err)
		}
		rules.ByAccess[access] = core.Rule{Decision: d}
	}

	for id, cr := range fc.Capability {
		// A capability entry may set only requires_arming; an empty decision
		// means "ask" (the fail-safe default), not an error.
		d := core.DecisionAsk
		if cr.Decision != "" {
			var err error
			if d, err = parseDecision(cr.Decision); err != nil {
				return core.PolicyRules{}, fmt.Errorf("%s: capability %q: %w", path, id, err)
			}
		}
		r := core.Rule{Decision: d, RequiresArming: cr.RequiresArming}
		if cr.ArmMinutes > 0 {
			r.ArmFor = time.Duration(cr.ArmMinutes) * time.Minute
		}
		rules.ByCapID[id] = r
	}

	return rules, nil
}

func parseDecision(s string) (core.Decision, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "allow":
		return core.DecisionAllow, nil
	case "ask":
		return core.DecisionAsk, nil
	case "deny":
		return core.DecisionDeny, nil
	default:
		return 0, fmt.Errorf("invalid decision %q (want allow|ask|deny)", s)
	}
}

func parseRisk(s string) (core.Risk, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "safe":
		return core.RiskSafe, nil
	case "confirm":
		return core.RiskConfirm, nil
	case "forbidden":
		return core.RiskForbidden, nil
	default:
		return 0, fmt.Errorf("invalid risk tier %q (want safe|confirm|forbidden)", s)
	}
}

func parseAccess(s string) (core.AccessKind, error) {
	switch k := core.AccessKind(strings.ToLower(strings.TrimSpace(s))); k {
	case core.AccessApp, core.AccessWindow, core.AccessWorkspace, core.AccessCommand:
		return k, nil
	default:
		return "", fmt.Errorf("invalid access kind %q (want app|window|workspace|command)", s)
	}
}
