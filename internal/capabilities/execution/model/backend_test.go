package model

import "testing"

func TestExecutionBackendRefValidate(t *testing.T) {
	valid := ExecutionBackendRef{Kind: BackendKindRemote, Provider: "genesis-sandbox", InstanceID: "worker-1", Authority: "executor"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid backend rejected: %v", err)
	}
	for _, invalid := range []ExecutionBackendRef{
		{},
		{Kind: "other", Authority: "executor"},
		{Kind: BackendKindHost},
		{Kind: BackendKindHost, Authority: "bad/authority"},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid backend accepted: %+v", invalid)
		}
	}
}
