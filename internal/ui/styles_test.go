package ui

import "testing"

func TestContentWidth(t *testing.T) {
	cases := []struct {
		termWidth int
		want      int
	}{
		{termWidth: 40, want: 40},
		{termWidth: MaxContentWidth - 1, want: MaxContentWidth - 1},
		{termWidth: MaxContentWidth, want: MaxContentWidth},
		{termWidth: MaxContentWidth + 1, want: MaxContentWidth},
		{termWidth: 500, want: MaxContentWidth},
	}
	for _, c := range cases {
		if got := ContentWidth(c.termWidth); got != c.want {
			t.Errorf("ContentWidth(%d) = %d, want %d", c.termWidth, got, c.want)
		}
	}
}

func TestContentPad(t *testing.T) {
	cases := []struct {
		termWidth int
		want      int
	}{
		{termWidth: 40, want: 0},
		{termWidth: MaxContentWidth, want: 0},
		{termWidth: MaxContentWidth + 20, want: 10},
		{termWidth: MaxContentWidth + 21, want: 10}, // odd remainder floors
	}
	for _, c := range cases {
		if got := ContentPad(c.termWidth); got != c.want {
			t.Errorf("ContentPad(%d) = %d, want %d", c.termWidth, got, c.want)
		}
	}
}
