package cli

import "testing"

func TestApplyResultError(t *testing.T) {
	tests := []struct {
		name    string
		dryRun  bool
		failed  int
		wantErr bool
	}{
		{name: "all updates succeed", wantErr: false},
		{name: "one update fails", failed: 1, wantErr: true},
		{name: "all updates fail", failed: 3, wantErr: true},
		{name: "dry run", dryRun: true, failed: 3, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := applyResultError(tt.dryRun, tt.failed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyResultError(%v, %d) error = %v, wantErr %v", tt.dryRun, tt.failed, err, tt.wantErr)
			}
		})
	}
}
