package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/history"
)

func mustRegisterFlagCompletion(cmd *cobra.Command, flagName string, fn cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(flagName, fn); err != nil {
		panic(fmt.Sprintf("registering completion for %s %q: %v", cmd.CommandPath(), flagName, err))
	}
}

func registerOwnerRepoHistoryCompletions(cmd *cobra.Command, presentOnly bool) {
	mustRegisterFlagCompletion(cmd, "owner", func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		records := loadHistoryRecordsForCompletion(presentOnly)
		return completeOwnersFromRecords(records, toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	mustRegisterFlagCompletion(cmd, "repo", func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		records := loadHistoryRecordsForCompletion(presentOnly)
		owner, _ := cmd.Flags().GetString("owner")
		return completeReposFromRecords(records, owner, toComplete), cobra.ShellCompDirectiveNoFileComp
	})
}

func completeInstalledUpgradeTargets(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	upgradeAll, _ := cmd.Flags().GetBool("all")
	if upgradeAll {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	records := loadHistoryRecordsForCompletion(true)
	return completeUpgradeTargetsFromRecords(records, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func loadHistoryRecordsForCompletion(presentOnly bool) []history.Record {
	histPath, err := config.HistoryFilePath()
	if err != nil {
		return nil
	}

	store := history.NewStore(histPath)
	if err := store.Load(); err != nil {
		return nil
	}

	records := store.Records()
	if presentOnly {
		return presentHistoryRecords(records)
	}
	return records
}

func completeOwnersFromRecords(records []history.Record, toComplete string) []cobra.Completion {
	counts := make(map[string]int)
	for _, rec := range records {
		if rec.Owner == "" {
			continue
		}
		counts[rec.Owner]++
	}

	owners := sortedKeys(counts)
	completions := make([]cobra.Completion, 0, len(owners))
	for _, owner := range owners {
		if !matchesCompletion(owner, toComplete) {
			continue
		}
		desc := "1 install"
		if counts[owner] != 1 {
			desc = fmt.Sprintf("%d installs", counts[owner])
		}
		completions = append(completions, cobra.CompletionWithDesc(owner, desc))
	}

	return completions
}

func completeReposFromRecords(records []history.Record, owner, toComplete string) []cobra.Completion {
	ownersByRepo := make(map[string]map[string]struct{})
	for _, rec := range records {
		if rec.Repo == "" {
			continue
		}
		if owner != "" && !strings.EqualFold(rec.Owner, owner) {
			continue
		}
		if ownersByRepo[rec.Repo] == nil {
			ownersByRepo[rec.Repo] = make(map[string]struct{})
		}
		ownersByRepo[rec.Repo][rec.Owner] = struct{}{}
	}

	repos := sortedKeys(ownersByRepo)
	completions := make([]cobra.Completion, 0, len(repos))
	for _, repo := range repos {
		if !matchesCompletion(repo, toComplete) {
			continue
		}

		owners := sortedKeys(ownersByRepo[repo])
		desc := strings.Join(owners, ", ")
		if desc == "" {
			completions = append(completions, cobra.Completion(repo))
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(repo, desc))
	}

	return completions
}

func completeUpgradeTargetsFromRecords(records []history.Record, toComplete string) []cobra.Completion {
	binaryMatches := make(map[string]int)
	binaryDescriptions := make(map[string]string)
	repoDescriptions := make(map[string]string)

	for _, rec := range records {
		target := fmt.Sprintf("%s/%s", rec.Owner, rec.Repo)
		if rec.Owner != "" && rec.Repo != "" {
			repoDescriptions[target] = describeRecordBinaries(rec)
		}

		seen := make(map[string]struct{})
		for _, bin := range rec.Binaries {
			name := bin.InstalledAs
			if name == "" {
				name = bin.Name
			}
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			binaryMatches[name]++
			if binaryDescriptions[name] == "" {
				binaryDescriptions[name] = fmt.Sprintf("%s/%s", rec.Owner, rec.Repo)
			}
		}
	}

	var completions []cobra.Completion
	for _, name := range sortedKeys(binaryMatches) {
		if binaryMatches[name] < 2 {
			continue
		}
		if !matchesCompletion(name, toComplete) {
			continue
		}
		desc := fmt.Sprintf("%d installs", binaryMatches[name])
		completions = append(completions, cobra.CompletionWithDesc(name, desc))
	}

	for _, target := range sortedKeys(repoDescriptions) {
		if !matchesCompletion(target, toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(target, repoDescriptions[target]))
	}

	return completions
}

// completeHistoryRemoveTargets is history remove's ValidArgsFunction. It
// suggests both record IDs and installed binary names — history remove
// accepts either, and unlike completeInstalledUpgradeTargets it must also
// surface records for binaries no longer present on disk (that's what
// history prune exists to clean up), so presentOnly is always false.
func completeHistoryRemoveTargets(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	records := loadHistoryRecordsForCompletion(false)
	return completeHistoryRemoveTargetsFromRecords(records, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeHistoryRemoveTargetsFromRecords(records []history.Record, toComplete string) []cobra.Completion {
	binaryMatches := make(map[string]int)
	binaryDescriptions := make(map[string]string)
	idDescriptions := make(map[string]string)

	for _, rec := range records {
		if rec.ID != "" {
			idDescriptions[rec.ID] = fmt.Sprintf("%s/%s@%s", rec.Owner, rec.Repo, rec.Tag)
		}

		seen := make(map[string]struct{})
		for _, bin := range rec.Binaries {
			name := bin.InstalledAs
			if name == "" {
				name = bin.Name
			}
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			binaryMatches[name]++
			if binaryDescriptions[name] == "" {
				binaryDescriptions[name] = fmt.Sprintf("%s/%s", rec.Owner, rec.Repo)
			}
		}
	}

	var completions []cobra.Completion
	for _, name := range sortedKeys(binaryMatches) {
		if !matchesCompletion(name, toComplete) {
			continue
		}
		desc := binaryDescriptions[name]
		if binaryMatches[name] > 1 {
			desc = fmt.Sprintf("%d matches, ambiguous - use ID", binaryMatches[name])
		}
		completions = append(completions, cobra.CompletionWithDesc(name, desc))
	}

	for _, id := range sortedKeys(idDescriptions) {
		if !matchesCompletion(id, toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(id, idDescriptions[id]))
	}

	return completions
}

func completeHistoryListSortValues(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	options := []struct {
		value       string
		description string
	}{
		{value: historyListSortBinary, description: "sort by installed binary name"},
		{value: historyListSortOwner, description: "sort by repository owner"},
		{value: historyListSortRepo, description: "sort by repository name"},
		{value: historyListSortInstalled, description: "sort by installed date, oldest first"},
	}

	completions := make([]cobra.Completion, 0, len(options))
	for _, option := range options {
		if !matchesCompletion(option.value, toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(option.value, option.description))
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeOutputFormatValues(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	options := []struct {
		value       string
		description string
	}{
		{value: "text", description: "human-readable output"},
		{value: "json", description: "machine-readable JSON output"},
	}

	completions := make([]cobra.Completion, 0, len(options))
	for _, option := range options {
		if !matchesCompletion(option.value, toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(option.value, option.description))
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completePinLevels(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	options := []struct {
		value       string
		description string
	}{
		{value: "patch", description: "lock to current exact release"},
		{value: "minor", description: "allow patch updates within current minor"},
		{value: "major", description: "allow minor and patch updates within current major"},
	}

	completions := make([]cobra.Completion, 0, len(options))
	for _, option := range options {
		if !matchesCompletion(option.value, toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(option.value, option.description))
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeConfigKeys(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	completions := make([]cobra.Completion, 0, len(configKeys))
	for _, option := range configKeys {
		if !matchesCompletion(option.value, toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(option.value, option.description))
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// completeConfigKeyArg is the ValidArgsFunction shared by config
// get/set/reset. It only ever completes the first positional (the key);
// config set's second positional (the value) is intentionally left
// uncompleted (freeform/typed, no clean enumeration source).
func completeConfigKeyArg(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeConfigKeys(cmd, args, toComplete)
}

func describeRecordBinaries(rec history.Record) string {
	var names []string
	seen := make(map[string]struct{})
	for _, bin := range rec.Binaries {
		name := bin.InstalledAs
		if name == "" {
			name = bin.Name
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "installed package"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func matchesCompletion(value, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix))
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
