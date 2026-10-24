package main

import (
	"strings"
	"testing"
)

func TestGateUsageErrors(t *testing.T) {
	cases := []struct {
		args      []string
		want, not string
	}{
		{[]string{"gate", "some/path"}, "unexpected arguments: some/path; gate judges the change set", "at most one"},
		{[]string{"gate", "--staged", "a", "b"}, "unexpected arguments: a b", "at most one"},
		{[]string{"gate", "--staged", "--all"}, "use at most one of --base <ref>, --staged, --all", "unexpected"},
		{[]string{"gate", "--base", "main", "--staged"}, "use at most one of", "unexpected"},
	}
	for _, c := range cases {
		code, _, errOut := runArc(c.args...)
		if code != exitUsage || !strings.Contains(errOut, c.want) || strings.Contains(errOut, c.not) {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, errOut)
		}
	}
}
