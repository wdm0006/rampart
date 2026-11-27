package github

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wdm0006/rampart/internal/config"
)

func TestLastPushApprovalParameters(t *testing.T) {
	for _, tt := range []struct {
		params string
		want   bool
	}{
		{"", false}, {"{}", false}, {`{"require_last_push_approval":false}`, false}, {`{"require_last_push_approval":true}`, true},
	} {
		var params json.RawMessage
		if tt.params != "" {
			params = json.RawMessage(tt.params)
		}
		got, err := rulesFromBranchRules([]branchRule{{Type: "pull_request", Parameters: params}})
		if err != nil {
			t.Fatal(err)
		}
		if got.RequireLastPushApproval != tt.want {
			t.Errorf("%q = %t", tt.params, got.RequireLastPushApproval)
		}
	}
	for _, want := range []bool{false, true} {
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			payload := buildRulesetPayload("main", config.Rules{RequirePullRequest: true, RequireLastPushApproval: want})
			found := false
			for _, raw := range payload["rules"].([]interface{}) {
				rule := raw.(map[string]interface{})
				if rule["type"] == "pull_request" {
					found = true
					if got := rule["parameters"].(map[string]interface{})["require_last_push_approval"]; got != want {
						t.Errorf("payload = %v, want %t", got, want)
					}
				}
			}
			if !found {
				t.Fatal("missing pull request rule")
			}
		})
	}
}
