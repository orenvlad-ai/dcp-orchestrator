package domain

import "testing"

func TestDCPPolicyTargetSpecAcceptsWBCReadmissionMarker(t *testing.T) {
	repoOnly, ok := DCPPolicyTarget("wb-core", "repo-only")
	if !ok {
		t.Fatal("wb-core repo-only target is unavailable")
	}
	liveRuntime, ok := DCPPolicyTarget("wb-core", "live-runtime")
	if !ok {
		t.Fatal("wb-core live-runtime target is unavailable")
	}
	direct, ok := DCPPolicyTarget("wb-browser-extension", "repo-only")
	if !ok {
		t.Fatal("direct repo-only target is unavailable")
	}

	tests := []struct {
		name   string
		spec   DCPPolicyTargetSpec
		marker string
		want   bool
	}{
		{name: "repo-only v2", spec: repoOnly, marker: DCPWBCHandoffV2CompatibilityMarker, want: true},
		{name: "repo-only legacy v1", spec: repoOnly, marker: DCPWBCHandoffV1CompatibilityMarker, want: true},
		{name: "live-runtime v2", spec: liveRuntime, marker: DCPWBCHandoffV2CompatibilityMarker, want: true},
		{name: "live-runtime rejects v1", spec: liveRuntime, marker: DCPWBCHandoffV1CompatibilityMarker},
		{name: "repo-only rejects foreign", spec: repoOnly, marker: "wb-core.dcp-release-handoff/foreign"},
		{name: "direct target rejects WBC marker", spec: direct, marker: DCPWBCHandoffV2CompatibilityMarker},
		{name: "foreign train target rejects WBC marker", spec: DCPPolicyTargetSpec{
			Target: "other", Profile: "repo-only", Repository: "orenvlad-ai/other",
			ReleaseAuthority: DCPReleaseWBCTrainOnly, CompatibilityMarker: DCPWBCHandoffV2CompatibilityMarker,
		}, marker: DCPWBCHandoffV2CompatibilityMarker},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.AcceptsWBCReadmissionMarker(tc.marker); got != tc.want {
				t.Fatalf("AcceptsWBCReadmissionMarker(%q) = %v, want %v", tc.marker, got, tc.want)
			}
		})
	}
}
