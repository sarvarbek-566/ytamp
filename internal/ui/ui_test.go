package ui

import "testing"

func TestVideoID(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.youtube.com/watch?v=34Na4j8AVgA":       "34Na4j8AVgA",
		"https://youtu.be/34Na4j8AVgA?si=abc":               "34Na4j8AVgA",
		"music.youtube.com/watch?v=34Na4j8AVgA&list=RDAMVM": "34Na4j8AVgA",
		"https://www.youtube.com/shorts/34Na4j8AVgA":        "34Na4j8AVgA",
		"the weeknd starboy":                                "",
		"https://www.youtube.com/watch?list=PL123":          "",
	} {
		if got := videoID(in); got != want {
			t.Errorf("videoID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFitAndGradient(t *testing.T) {
	if got := fit("hello world", 5); got != "hell…" {
		t.Errorf("fit truncate = %q", got)
	}
	if got := fit("hi", 4); got != "hi  " {
		t.Errorf("fit pad = %q", got)
	}
	g := gradient([]string{"#000000", "#ffffff"}, 3)
	if string(g[0]) != "#000000" || string(g[2]) != "#ffffff" {
		t.Errorf("gradient ends = %v", g)
	}
}
