package common

import "testing"

func TestQuoteReply(t *testing.T) {
	tests := map[string]string{
		"":                           "",
		"  \n ":                      "",
		"hello":                      "> hello\n\n",
		"line one\r\nline two\n":     "> line one\n> line two\n\n",
		"para one\n\npara two":       "> para one\n>\n> para two\n\n",
		"> already quoted\nmy reply": "> > already quoted\n> my reply\n\n",
	}
	for body, want := range tests {
		if got := QuoteReply(body); got != want {
			t.Errorf("QuoteReply(%q) = %q, want %q", body, got, want)
		}
	}
}
