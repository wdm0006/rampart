package github

import (
	"reflect"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

func TestClassifyProtectionResult(t *testing.T) {
	tests := []struct {
		name    string
		stderr  string
		want    config.Rules
		wantOK  bool
		wantErr string
	}{
		{name: "404 status", stderr: "gh: Branch protection not found (HTTP 404)", want: config.NoProtectionRules(), wantOK: true},
		{name: "not found wording", stderr: "gh: Not Found", want: config.NoProtectionRules(), wantOK: true},
		{name: "branch not protected wording", stderr: "gh: Branch not protected", want: config.NoProtectionRules(), wantOK: true},
		{name: "403 status", stderr: "gh: Resource not accessible (HTTP 403)", wantErr: "insufficient permissions"},
		{name: "admin permission wording", stderr: "gh: Must have admin rights", wantErr: "insufficient permissions"},
		{name: "unrecognized error", stderr: "gh: service unavailable", wantErr: "gh api failed: gh: service unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK, err := classifyProtectionResult(tt.stderr)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("classifyProtectionResult() rules = %+v, want %+v", got, tt.want)
			}
			if gotOK != tt.wantOK {
				t.Errorf("classifyProtectionResult() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotErr := errorString(err); gotErr != tt.wantErr {
				t.Errorf("classifyProtectionResult() error = %q, want %q", gotErr, tt.wantErr)
			}
		})
	}
}

func TestReposEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		ownerType   string
		owner       string
		currentUser string
		want        string
	}{
		{
			name:      "organization",
			ownerType: "Organization",
			owner:     "acme",
			want:      "orgs/acme/repos?per_page=100",
		},
		{
			name:        "organization matching the authenticated login",
			ownerType:   "Organization",
			owner:       "acme",
			currentUser: "acme",
			want:        "orgs/acme/repos?per_page=100",
		},
		{
			name:        "authenticated user",
			ownerType:   "User",
			owner:       "octocat",
			currentUser: "octocat",
			want:        "user/repos?affiliation=owner&per_page=100",
		},
		{
			name:        "authenticated user with differing case",
			ownerType:   "User",
			owner:       "OctoCat",
			currentUser: "octocat",
			want:        "user/repos?affiliation=owner&per_page=100",
		},
		{
			name:        "other user",
			ownerType:   "User",
			owner:       "someone-else",
			currentUser: "octocat",
			want:        "users/someone-else/repos?type=owner&per_page=100",
		},
		{
			name:      "unresolved authenticated login falls back to public endpoint",
			ownerType: "User",
			owner:     "octocat",
			want:      "users/octocat/repos?type=owner&per_page=100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reposEndpoint(tt.ownerType, tt.owner, tt.currentUser); got != tt.want {
				t.Errorf("reposEndpoint(%q, %q, %q) = %q, want %q",
					tt.ownerType, tt.owner, tt.currentUser, got, tt.want)
			}
		})
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
