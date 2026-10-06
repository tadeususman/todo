package task

import "testing"

func TestNormalize(t *testing.T) {
	if NormPriority(" URGENT ") != "urgent" || NormPriority("bogus") != "medium" || NormPriority("") != "medium" {
		t.Error("NormPriority")
	}
	if NormComplexity("Complex") != "complex" || NormComplexity("x") != "simple" {
		t.Error("NormComplexity")
	}
	if validStatus("done") != true || validStatus("nope") {
		t.Error("validStatus")
	}
}
