package main

import (
	"strings"
	"testing"
)

func TestVerifyEventsRequiresPassWithoutSkip(t *testing.T) {
	if err := verifyEvents("fixture", `{"Action":"pass","Test":"TestOne"}`, []string{"TestOne"}); err != nil {
		t.Fatal(err)
	}
	if err := verifyEvents("fixture", `{"Action":"skip","Test":"TestOne"}`, []string{"TestOne"}); err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("skip error = %v", err)
	}
	if err := verifyEvents("fixture", `{"Action":"output","Test":"TestOne"}`, []string{"TestOne"}); err == nil || !strings.Contains(err.Error(), "did not pass") {
		t.Fatalf("missing pass error = %v", err)
	}
}

func TestInventoryIsStableAndDeduplicatesPackages(t *testing.T) {
	packages, names := inventory([]testEntry{
		{Package: "./z", Name: "TestZ"},
		{Package: "./a", Name: "TestA"},
		{Package: "./a", Name: "TestB"},
	})
	if strings.Join(packages, ",") != "./a,./z" {
		t.Fatalf("packages = %v", packages)
	}
	if strings.Join(names, ",") != "TestA,TestB,TestZ" {
		t.Fatalf("names = %v", names)
	}
}
