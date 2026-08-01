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
	// Determine whether the owner is a user or organization so we use the
	// correct endpoint (see reposEndpoint).
	ownerType, err := getOwnerType(owner)
	if err != nil {
		return nil, fmt.Errorf("failed to look up owner %s: %w", owner, err)
	}

	// A failure to resolve the authenticated login is not fatal: we fall back
	// to the public users/ endpoint rather than aborting the run.
	currentUser, _ := GetCurrentUser()

	repos, err := listReposFromEndpoint(reposEndpoint(ownerType, owner, currentUser))
	if err != nil {
		return nil, fmt.Errorf("failed to list repos for %s: %w", owner, err)
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

// reposEndpoint picks the repository listing endpoint for an owner.
//
// users/{owner}/repos lists only PUBLIC repositories, and type=owner does not
// change that, so when the owner is the authenticated user we use user/repos
// instead — that endpoint includes private repositories. Auditing any other
// user's account can only ever see their public repositories.
//
// orgs/{owner}/repos defaults to type=all and already returns private
// repositories to authorized members.
func reposEndpoint(ownerType, owner, currentUser string) string {
	if ownerType == "Organization" {
		return fmt.Sprintf("orgs/%s/repos?per_page=100", owner)
	}
	if currentUser != "" && strings.EqualFold(owner, currentUser) {
		return "user/repos?affiliation=owner&per_page=100"
	}
	return fmt.Sprintf("users/%s/repos?type=owner&per_page=100", owner)
}

// getOwnerType returns the GitHub account type ("User" or "Organization")
// for the given owner name.
func getOwnerType(owner string) (string, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("users/%s", owner), "--jq", ".type")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gh api failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to run gh: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
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
			return classifyProtectionResult(string(exitErr.Stderr))
		}
		return config.Rules{}, false, fmt.Errorf("failed to run gh: %w", err)
	}

	var resp config.ProtectionResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return config.Rules{}, false, fmt.Errorf("failed to parse protection response: %w", err)
	}

	return config.RulesFromResponse(resp), true, nil
}

func classifyProtectionResult(stderr string) (config.Rules, bool, error) {
	if strings.Contains(stderr, "404") || strings.Contains(stderr, "Not Found") ||
		strings.Contains(stderr, "Branch not protected") {
		return config.NoProtectionRules(), true, nil
	}
	if strings.Contains(stderr, "403") || strings.Contains(stderr, "Must have admin") {
		return config.Rules{}, false, fmt.Errorf("insufficient permissions")
	}
	return config.Rules{}, false, fmt.Errorf("gh api failed: %s", stderr)
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
