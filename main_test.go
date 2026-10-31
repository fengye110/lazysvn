package main

import (
	"strings"
	"testing"
)

func TestParseStatusWithConflicts(t *testing.T) {
	out := `D     C .dvt
        >   local dir unversioned, incoming dir add upon update
?       .dvt/.build_config_dict
?       .dvt/default.build.auto_1
?       design/test.txt
Summary of conflicts:
  Tree conflicts: 1`

	entries := parseStatus(out)
	found := map[string]rune{}
	for _, e := range entries {
		found[e.Path] = e.Code
	}
	if found[".dvt"] != 'D' {
		t.Fatalf(".dvt code = %q, want D", found[".dvt"])
	}
	if found["design/test.txt"] != '?' {
		t.Fatalf("design/test.txt code = %q, want ?", found["design/test.txt"])
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Code == 'S' || e.Code == ' ' {
			t.Fatalf("unexpected summary line parsed as entry: %+v", e)
		}
	}
}

func TestShellQuoteArg(t *testing.T) {
	cases := map[string]string{
		"README.md": "README.md",
		"fix bug":   `"fix bug"`,
		"修复 空格":     `"修复 空格"`,
		"a\"b":      `"a\"b"`,
		"":          `""`,
	}
	for in, want := range cases {
		if got := shellQuoteArg(in); got != want {
			t.Errorf("shellQuoteArg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatSVNResultQuotesMessage(t *testing.T) {
	out := formatSVNResult([]string{"commit", "-m", "fix bug", "src/main.c"}, "", nil)
	if !strings.Contains(out, `$ svn commit -m "fix bug" src/main.c`) {
		t.Fatalf("command line not quoted: %q", out)
	}
}
