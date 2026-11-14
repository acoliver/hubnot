package main

import (
	"strings"
	"testing"
)

func TestFix4StrictWire(t *testing.T) {
	root := "sha256:" + strings.Repeat("a", 64)
	current := "sha256:" + strings.Repeat("b", 64)
	tip := strings.Repeat("c", 40)
	env := []string{"GIT_PUSH_OPTION_COUNT=4", "GIT_PUSH_OPTION_0=hn-advance=" + current, "GIT_PUSH_OPTION_1=hn-expected-tip=" + tip, "GIT_PUSH_OPTION_2=hn-rule=auto", "GIT_PUSH_OPTION_3=hn-strict-jit-root=" + root}
	if _, err := parseServePushOptions(env, 40); err != nil {
		t.Fatalf("valid conditional strict advancement must be supported: %v", err)
	}
	for _, v := range []string{"", "bad", "sha256:" + strings.Repeat("A", 64)} {
		invalid := append([]string{}, env...)
		invalid[4] = "GIT_PUSH_OPTION_3=hn-strict-jit-root=" + v
		if _, err := parseServePushOptions(invalid, 40); err == nil {
			t.Errorf("malformed root %q admitted", v)
		}
	}
	for _, missing := range []int{1, 2, 3} {
		invalid := []string{"GIT_PUSH_OPTION_COUNT=3"}
		n := 0
		for i, v := range env[1:] {
			if i+1 != missing {
				invalid = append(invalid, "GIT_PUSH_OPTION_"+string(rune('0'+n))+"="+strings.SplitN(v, "=", 2)[1])
				n++
			}
		}
		if _, err := parseServePushOptions(invalid, 40); err == nil {
			t.Errorf("missing strict companion %d admitted", missing)
		}
	}
	duplicate := append([]string{}, env...)
	duplicate[3] = "GIT_PUSH_OPTION_2=hn-strict-jit-root=" + root
	if _, err := parseServePushOptions(duplicate, 40); err == nil {
		t.Error("duplicate root admitted")
	}
}
