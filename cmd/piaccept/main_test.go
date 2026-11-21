package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

// The risk notice must stay at the top of --help: it is what users see before running the active tests.
func TestUsageShowsNotice(t *testing.T) {
	var buf bytes.Buffer
	flag.CommandLine.SetOutput(&buf)
	defer flag.CommandLine.SetOutput(nil)
	usage()
	got := buf.String()
	for _, want := range []string{"Use at your own risk", "no warranty", "Back up important data", "--quick", "Usage: piaccept"} {
		if !strings.Contains(got, want) {
			t.Errorf("--help lacks %q", want)
		}
	}
	if !strings.HasPrefix(got, "piaccept — Raspberry Pi acceptance test") {
		t.Errorf("notice is not at the top:\n%s", got)
	}
}
