package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLastPushApproval(t *testing.T) {
	for _, a := range []bool{false, true} {
		for _, b := range []bool{false, true} {
			t.Run(fmt.Sprintf("%t/%t", a, b), func(t *testing.T) {
				if got := MergeProtective(Rules{RequireLastPushApproval: a}, Rules{RequireLastPushApproval: b}).RequireLastPushApproval; got != (a || b) {
					t.Fatalf("merge = %t", got)
				}
				desired := Rules{RequirePullRequest: true, RequireLastPushApproval: a}
				actual := Rules{RequirePullRequest: true, RequireLastPushApproval: b}
				for _, stricter := range []bool{false, true} {
					found := false
					for _, d := range Compare(desired, actual, stricter) {
						if d.Rule == "require_last_push_approval" {
							found = true
							if d.Pass != (a == b || stricter && b) || d.Want != fmt.Sprint(a) || d.Got != fmt.Sprint(b) {
								t.Errorf("diff = %+v", d)
							}
						}
					}
					if !found {
						t.Fatal("missing diff")
					}
				}
			})
		}
		r := Rules{RequirePullRequest: true, RequireLastPushApproval: a}
		if got := r.ToAPIPayload()["required_pull_request_reviews"].(map[string]interface{})["require_last_push_approval"]; got != a {
			t.Errorf("payload = %v", got)
		}
	}
	for _, tt := range []struct {
		raw  string
		want bool
	}{
		{"{}", false},
		{`{"required_pull_request_reviews":{}}`, false},
		{`{"required_pull_request_reviews":{"require_last_push_approval":false}}`, false},
		{`{"required_pull_request_reviews":{"require_last_push_approval":true}}`, true},
	} {
		var resp ProtectionResponse
		if err := json.Unmarshal([]byte(tt.raw), &resp); err != nil {
			t.Fatal(err)
		}
		if got := RulesFromResponse(resp).RequireLastPushApproval; got != tt.want {
			t.Errorf("%s = %t", tt.raw, got)
		}
	}
}

func TestLastPushApprovalOverrides(t *testing.T) {
	for _, base := range []bool{false, true} {
		for _, override := range []string{"", "require_last_push_approval: true", "require_last_push_approval: false"} {
			body := fmt.Sprintf("rules:\n  require_last_push_approval: %t\noverrides:\n  - repos: [widget]\n    rules: {%s}\n", base, override)
			cfg, err := Load(writeTempConfig(t, body))
			if err != nil {
				t.Fatal(err)
			}
			want := base
			if override != "" {
				want = strings.HasSuffix(override, "true")
			}
			if got := cfg.RulesForRepo("widget").RequireLastPushApproval; got != want {
				t.Errorf("%s = %t, want %t", body, got, want)
			}
		}
	}
	for _, body := range []string{"rules: {require_last_push_aproval: true}", "overrides: [{repos: [widget], rules: {require_last_push_aproval: true}}]"} {
		_, err := Load(writeTempConfig(t, body))
		if err == nil || !strings.Contains(err.Error(), "require_last_push_aproval") {
			t.Errorf("typo error = %v", err)
		}
	}
}
