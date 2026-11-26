package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/spf13/cobra"
)

// cooldownSettings holds the invocation-wide cooldown configuration resolved
// from the --cooldown flag and config file. Per-repo decisions (trusted owner
// exemptions) are made by policyFor.
type cooldownSettings struct {
	days          int    // effective minimum release age in days; 0 = disabled
	source        string // "flag" or "config"
	trustedOwners []hostOwner
	now           func() time.Time
}

// hostOwner is a GitHub owner on a specific Host. An empty host means
// github.com.
type hostOwner struct {
	host  string
	owner string
}

// parseTrustedOwner parses a trustedOwners entry of the form "owner" (meaning
// github.com) or "host/owner". The returned host is normalized.
func parseTrustedOwner(entry string) (hostOwner, error) {
	host, owner := "", entry
	if before, after, found := strings.Cut(entry, "/"); found {
		if strings.Contains(after, "/") {
			return hostOwner{}, errors.New("want owner or host/owner")
		}
		if strings.TrimSpace(before) == "" {
			return hostOwner{}, errors.New("empty host")
		}
		host, owner = before, after
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return hostOwner{}, errors.New("empty owner")
	}
	normalized, err := github.NormalizeHost(host)
	if err != nil {
		return hostOwner{}, err
	}
	return hostOwner{host: normalized, owner: owner}, nil
}

// configEntry returns o in trustedOwners config entry form: the bare owner
// for github.com, host/owner for any other Host.
func (o hostOwner) configEntry() string {
	if o.host == "" || o.host == github.DefaultHost {
		return o.owner
	}
	return o.host + "/" + o.owner
}

// resolveCooldownSettings applies precedence: an explicitly set --cooldown
// flag wins over the config/env value. Negative values are clamped to 0.
func resolveCooldownSettings(cmd *cobra.Command, cfg *config.AppConfig) cooldownSettings {
	settings := cooldownSettings{
		days:   cfg.Cooldown,
		source: "config",
		now:    time.Now,
	}
	for _, entry := range cfg.TrustedOwners {
		trusted, err := parseTrustedOwner(entry)
		if err != nil {
			slog.Warn("ignoring invalid trustedOwners entry", "entry", entry, "err", err)
			continue
		}
		settings.trustedOwners = append(settings.trustedOwners, trusted)
	}
	if flag := cmd.Flags().Lookup("cooldown"); flag != nil && flag.Changed {
		if days, err := cmd.Flags().GetInt("cooldown"); err == nil {
			settings.days = days
			settings.source = "flag"
		}
	}
	if settings.days < 0 {
		slog.Warn("negative cooldown clamped to 0", "value", settings.days, "source", settings.source)
		settings.days = 0
	}
	return settings
}

// cooldownPolicy is the cooldown decision for a specific repo owner.
type cooldownPolicy struct {
	days   int
	reason string
	// trustedOwnersEntry is the trustedOwners config entry that would
	// exempt the repo owner, or empty when unknown.
	trustedOwnersEntry string
	now                func() time.Time
}

// policyFor returns the policy to enforce for a repo owned by repo.owner on
// repo.host, disabling the cooldown when that owner is trusted on that Host.
func (s cooldownSettings) policyFor(repo hostOwner) cooldownPolicy {
	host, err := github.NormalizeHost(repo.host)
	if err != nil {
		// Failing closed is safe: enforcing the cooldown can only delay an
		// install, never exempt an owner whose Host cannot be verified.
		slog.Warn("cannot verify host for trusted owner check; enforcing cooldown", "host", repo.host, "owner", repo.owner, "err", err)
		return cooldownPolicy{days: s.days, reason: s.source, now: s.now}
	}
	repo.host = host
	for _, trusted := range s.trustedOwners {
		if trusted.host == repo.host && strings.EqualFold(repo.owner, trusted.owner) {
			slog.Debug("cooldown skipped for trusted owner", "host", repo.host, "owner", repo.owner)
			return cooldownPolicy{days: 0, reason: "trusted owner " + trusted.configEntry(), now: s.now}
		}
	}
	return cooldownPolicy{
		days:               s.days,
		reason:             s.source,
		trustedOwnersEntry: repo.configEntry(),
		now:                s.now,
	}
}

func (p cooldownPolicy) enabled() bool {
	return p.days > 0
}

// eligibleAt reports whether a timestamp is old enough to satisfy the
// cooldown. Zero timestamps are never eligible: age cannot be verified.
func (p cooldownPolicy) eligibleAt(t time.Time) bool {
	if t.IsZero() {
		return false
	}
	cutoff := p.now().Add(-time.Duration(p.days) * 24 * time.Hour)
	return !t.After(cutoff)
}

// ageDays returns the whole days elapsed since t, clamped to >= 0.
func (p cooldownPolicy) ageDays(t time.Time) int {
	days := int(p.now().Sub(t).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

func (p cooldownPolicy) releaseEligible(rel *github.Release) bool {
	return p.eligibleAt(rel.PublishedAt)
}

// checkRelease hard-fails when the release is inside the cooldown window.
// Used on paths with no fallback, such as an explicit --tag request.
func (p cooldownPolicy) checkRelease(rel *github.Release) error {
	if !p.enabled() || p.releaseEligible(rel) {
		return nil
	}
	if rel.PublishedAt.IsZero() {
		return fmt.Errorf("release %s has no published date; cannot verify the %d-day cooldown (use --cooldown 0 to override)", rel.TagName, p.days)
	}
	trustHint := "the owner"
	if p.trustedOwnersEntry != "" {
		trustHint = strconv.Quote(p.trustedOwnersEntry)
	}
	return fmt.Errorf("release %s is %d day(s) old; cooldown is %d days (use --cooldown 0 to override, or add %s to trustedOwners)", rel.TagName, p.ageDays(rel.PublishedAt), p.days, trustHint)
}

// checkAsset guards against assets re-uploaded onto an already-eligible
// release, which would otherwise bypass the release-level cooldown. It only
// fires when the asset was modified after the release was published and that
// modification is still inside the cooldown window.
func (p cooldownPolicy) checkAsset(rel *github.Release, asset github.Asset) error {
	if !p.enabled() {
		return nil
	}
	assetTime := asset.UpdatedAt
	if assetTime.IsZero() {
		assetTime = asset.CreatedAt
	}
	if assetTime.IsZero() || !assetTime.After(rel.PublishedAt) {
		return nil
	}
	if p.eligibleAt(assetTime) {
		return nil
	}
	return fmt.Errorf("asset %s was updated %d day(s) ago, after release %s was published; a re-uploaded asset can indicate tampering — refusing under the %d-day cooldown (use --cooldown 0 to override)", asset.Name, p.ageDays(assetTime), rel.TagName, p.days)
}

// findCooldownFallback returns the newest non-draft, non-prerelease release
// in releases (ordered most-recent-first) that satisfies the cooldown.
func findCooldownFallback(releases []github.Release, owner, repo string, p cooldownPolicy) (*github.Release, error) {
	for i := range releases {
		rel := &releases[i]
		if rel.Draft || rel.Prerelease {
			continue
		}
		if p.releaseEligible(rel) {
			return rel, nil
		}
	}
	return nil, fmt.Errorf("no release for %s/%s among the %d most recent satisfies the %d-day cooldown (use --cooldown 0 to override)", owner, repo, len(releases), p.days)
}
