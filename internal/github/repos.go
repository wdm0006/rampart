package github

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wdm0006/rampart/internal/config"
)

type Repo struct {
	Name          string `json:"name"`
	Fork          bool   `json:"fork"`
	Archived      bool   `json:"archived"`
	DefaultBranch string `json:"default_branch"`
}

// GetCurrentUser returns the currently authenticated GitHub username
func GetCurrentUser() (string, error) {
	cmd := exec.Command("gh", "api", "user", "--jq", ".login")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "auth login") || strings.Contains(stderr, "not logged") {
				return "", fmt.Errorf("not authenticated with GitHub CLI\n\nRun: gh auth login")
			}
			return "", fmt.Errorf("gh command failed: %s", stderr)
		}
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			return "", fmt.Errorf("GitHub CLI (gh) not found\n\nInstall it from: https://cli.github.com\nThen run: gh auth login")
		}
		return "", fmt.Errorf("failed to run gh: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// ListRepos lists non-fork, non-archived repos for an owner (user or org)
func ListRepos(owner string) ([]Repo, error) {
	// Try user repos first
	repos, err := listReposFromEndpoint(fmt.Sprintf("users/%s/repos?type=owner&per_page=100", owner))
	if err != nil {
		// Fall back to org repos
		repos, err = listReposFromEndpoint(fmt.Sprintf("orgs/%s/repos?per_page=100", owner))
		if err != nil {
			return nil, fmt.Errorf("failed to list repos for %s: %w", owner, err)
		}
	}

	// Filter out forks and archived repos
	var filtered []Repo
	for _, r := range repos {
		if !r.Fork && !r.Archived {
			filtered = append(filtered, r)
		}
	}

	return filtered, nil
}

func listReposFromEndpoint(endpoint string) ([]Repo, error) {
	cmd := exec.Command("gh", "api", endpoint, "--paginate")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh api failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run gh: %w", err)
	}

	var repos []Repo
	if err := json.Unmarshal(output, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse repos: %w", err)
	}

	return repos, nil
}

// GetRepo fetches a single repo's metadata
func GetRepo(owner, name string) (Repo, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/%s", owner, name))
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return Repo{}, fmt.Errorf("gh api failed: %s", string(exitErr.Stderr))
		}
		return Repo{}, fmt.Errorf("failed to run gh: %w", err)
	}

	var repo Repo
	if err := json.Unmarshal(output, &repo); err != nil {
		return Repo{}, fmt.Errorf("failed to parse repo: %w", err)
	}

	return repo, nil
}

// GetBranchProtection gets the current branch protection rules for a repo.
// Returns zero Rules if no protection is set (404).
// Returns an error string for permission errors (403) that should be surfaced per-repo.
func GetBranchProtection(owner, repo, branch string) (config.Rules, bool, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/branches/%s/protection", owner, repo, branch)
	cmd := exec.Command("gh", "api", endpoint)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			// 404 = no protection configured
			if strings.Contains(stderr, "404") || strings.Contains(stderr, "Not Found") ||
				strings.Contains(stderr, "Branch not protected") {
				return config.Rules{RequiredChecks: []string{}}, true, nil
			}
			// 403 = no permission
			if strings.Contains(stderr, "403") || strings.Contains(stderr, "Must have admin") {
				return config.Rules{}, false, fmt.Errorf("insufficient permissions")
			}
			return config.Rules{}, false, fmt.Errorf("gh api failed: %s", stderr)
		}
		return config.Rules{}, false, fmt.Errorf("failed to run gh: %w", err)
	}

	var resp config.ProtectionResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return config.Rules{}, false, fmt.Errorf("failed to parse protection response: %w", err)
	}

	return config.RulesFromResponse(resp), true, nil
}

// SetBranchProtection applies branch protection rules to a repo
func SetBranchProtection(owner, repo, branch string, rules config.Rules) error {
	payload := rules.ToAPIPayload()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Write payload to temp file for --input
	tmpFile, err := os.CreateTemp("", "rampart-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(payloadJSON); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	endpoint := fmt.Sprintf("repos/%s/%s/branches/%s/protection", owner, repo, branch)
	cmd := exec.Command("gh", "api", endpoint,
		"--method", "PUT",
		"--input", tmpFile.Name(),
		"-H", "Accept: application/vnd.github+json",
	)
	_, err = cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("failed to set protection: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to run gh: %w", err)
	}

	return nil
}
