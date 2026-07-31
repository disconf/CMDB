package monitor

import "testing"

func TestEscalationPolicyValidation(t *testing.T) {
	valid := CreateEscalationPolicyInput{Name: "critical timeout", Severity: "critical", TimeoutMinutes: 15, Team: "SRE", Owner: "sre-lead"}
	if err := validateEscalation(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.TimeoutMinutes = 0
	if err := validateEscalation(invalid); err == nil {
		t.Fatal("expected timeout validation")
	}
}
