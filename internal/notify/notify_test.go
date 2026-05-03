package notify

import (
	"errors"
	"strings"
	"testing"
)

func TestSplitURLs(t *testing.T) {
	got := SplitURLs(" gotify://h/m?token=t , ntfy://x/y ")
	if len(got) != 2 || got[0] != "gotify://h/m?token=t" || got[1] != "ntfy://x/y" {
		t.Fatalf("got %#v", got)
	}
	if len(SplitURLs("")) != 0 || len(SplitURLs("  ,  ")) != 0 {
		t.Fatal("expected empty")
	}
}

func TestValidate_empty(t *testing.T) {
	if err := Validate(""); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_unknownScheme(t *testing.T) {
	err := Validate("not-a-real-scheme-xyz://foo")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestJoinErrors(t *testing.T) {
	if err := joinErrors([]error{nil, nil}); err != nil {
		t.Fatal(err)
	}
	err := joinErrors([]error{nil, errors.New("a"), errors.New("b")})
	if err == nil || !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("got %v", err)
	}
}
