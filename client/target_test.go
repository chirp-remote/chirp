// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Fabrintek Engenharia Digital Ltda

package client

import (
	"errors"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	token := strings.Repeat("aZ0-_", 8) + "abc" // 43 characters
	for _, tc := range []struct {
		in   string
		want Target
	}{
		{"482913", Target{Code: "482913"}},
		{" 482 913 ", Target{Code: "482913"}},
		{"482-913", Target{Code: "482913"}},
		{"482.913", Target{Code: "482913"}},
		{token, Target{Token: token}},
		{"https://chirp.example/l#" + token, Target{Token: token}},
		{"  https://chirp.example/#" + token + "\n", Target{Token: token}},
		{"482-913-5071", Target{Code: "482913", Secret: "5071"}},
		{"482 913 5071", Target{Code: "482913", Secret: "5071"}},
		{"4829135071", Target{Code: "482913", Secret: "5071"}},
		{token + ".0042", Target{Token: token, Secret: "0042"}},
		{"https://chirp.example/l#" + token + ".5071", Target{Token: token, Secret: "5071"}},
	} {
		got, err := ParseTarget(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseTarget(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{
		"",
		"48291",                            // five digits
		"4829135",                          // seven
		"482 91a",                          // a letter
		"٤٨٢٩١٣",                           // digits from another script
		token[:42],                         // a token one short
		token + "x",                        // one long
		"https://chirp.example/l/" + token, // the token outside the fragment
		"https://chirp.example/l#",
		"hello",
		"482-913-507",   // nine digits
		"482-913-50712", // eleven
		token + ".507",  // a short secret
		token + ".50a1", // a letter in it
		token + ".",     // a dot and nothing
		"https://chirp.example/l#" + token + ".12345",
	} {
		if _, err := ParseTarget(in); !errors.Is(err, ErrNotATarget) {
			t.Errorf("ParseTarget(%q) = %v, want ErrNotATarget", in, err)
		}
	}
}

func FuzzParseTarget(f *testing.F) {
	v := readLinkFragments(f)
	for _, c := range v.Cases {
		f.Add(c.Fragment)
		f.Add(v.Page + c.Fragment)
	}
	for _, s := range []string{
		"",
		"482913",
		"482-913",
		"482-913-5071",
		"482 913 5071",
		"hello",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := ParseTarget(s)
		if err != nil {
			return
		}
		if (got.Code != "") == (got.Token != "") {
			t.Fatalf("ParseTarget(%q) = %+v, want exactly one of Code or Token", s, got)
		}
		if got.Code != "" {
			checkCodeRoundTrips(t, s, got.Code)
		}
		if got.Token != "" && !validToken(got.Token) {
			t.Fatalf("ParseTarget(%q).Token = %q is not a valid token", s, got.Token)
		}
		if got.Secret != "" && !validSecret(got.Secret) {
			t.Fatalf("ParseTarget(%q).Secret = %q is not a valid secret", s, got.Secret)
		}
	})
}

func checkCodeRoundTrips(t *testing.T, s, code string) {
	t.Helper()
	if len(code) != 6 {
		t.Fatalf("ParseTarget(%q).Code = %q, want 6 digits", s, code)
	}
	again, err := ParseTarget(code)
	if err != nil || again.Code != code || again.Secret != "" {
		t.Fatalf("ParseTarget(%q) -> %+v did not round-trip Code", s, again)
	}
}

func validToken(s string) bool {
	return len(s) == 43 && isToken(s)
}

func validSecret(s string) bool {
	return len(s) == SecretLength && isSecret(s)
}
