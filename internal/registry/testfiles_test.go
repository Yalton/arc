package registry

import "testing"

func TestIsTest(t *testing.T) {
	for p, want := range map[string]bool{
		"internal/a/a_test.go":       true,
		"internal/a/a.go":            false,
		"pkg/test_x.py":              true,
		"pkg/x_test.py":              true,
		"pkg/x.py":                   false,
		"web/app.test.ts":            true,
		"web/app.spec.jsx":           true,
		"web/app.ts":                 false,
		"testdata/x.go":              true,
		"internal/tests/helper.py":   true,
		"src/__tests__/a.js":         true,
		"internal/testing/helper.go": false,
		"latest/x.go":                false,
	} {
		if got := IsTest(p); got != want {
			t.Errorf("IsTest(%q) = %v", p, got)
		}
	}
}
