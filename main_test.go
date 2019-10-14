package main

import (
	"os/exec"
	"testing"
)

func TestGoFmt(t *testing.T) {
	out, err := exec.Command("gofmt", "-l", ".").Output()
	if err != nil {
		t.Fatal(err)
	}

	if len(out) > 0 {
		t.Fatalf("gofmt returned results: %s", out)
	}
}
