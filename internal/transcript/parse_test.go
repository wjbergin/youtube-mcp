package transcript

import (
	"strings"
	"testing"
)

const sampleJSON3 = `{
  "events": [
    {"tStartMs": 0, "dDurationMs": 1000},
    {"tStartMs": 1000, "segs": [{"utf8": "Hello"}, {"utf8": " world"}]},
    {"tStartMs": 3200, "aAppend": 1, "segs": [{"utf8": "\n"}]},
    {"tStartMs": 65000, "segs": [{"utf8": "second\nline"}]},
    {"tStartMs": 3661000, "segs": [{"utf8": "an hour in"}]}
  ]
}`

func TestParseJSON3PlainText(t *testing.T) {
	got, err := parseJSON3([]byte(sampleJSON3), false)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello world\nsecond line\nan hour in"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseJSON3WithTimestamps(t *testing.T) {
	got, err := parseJSON3([]byte(sampleJSON3), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[0:01] Hello world", "[1:05] second line", "[1:01:01] an hour in"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestParseJSON3Empty(t *testing.T) {
	if _, err := parseJSON3([]byte(`{"events":[]}`), false); err == nil {
		t.Error("expected error for empty caption track")
	}
}
