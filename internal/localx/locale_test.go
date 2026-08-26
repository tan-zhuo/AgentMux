package localx

import (
	"slices"
	"testing"
)

func TestWithUTF8Locale(t *testing.T) {
	cases := []struct {
		name string
		goos string
		env  []string
		want string // the LC_CTYPE that should be appended, or "" for untouched
	}{
		{
			name: "a Mac launched from the Dock has no locale at all",
			goos: "darwin",
			env:  []string{"PATH=/usr/bin", "HOME=/Users/tan"},
			want: "LC_CTYPE=UTF-8",
		},
		{
			name: "a locale the user chose is never overruled",
			goos: "darwin",
			env:  []string{"PATH=/usr/bin", "LANG=zh_CN.UTF-8"},
		},
		{
			name: "even a deliberate C locale stands",
			goos: "darwin",
			env:  []string{"LC_ALL=C"},
		},
		{
			name: "LC_CTYPE alone counts as a locale",
			goos: "darwin",
			env:  []string{"LC_CTYPE=en_US.ISO8859-1"},
		},
		{
			name: "an empty value is no locale",
			goos: "darwin",
			env:  []string{"LANG="},
			want: "LC_CTYPE=UTF-8",
		},
		{
			name: "Linux keeps whatever its session gave it, including nothing",
			goos: "linux",
			env:  []string{"PATH=/usr/bin"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := slices.Clone(c.env)
			got := withUTF8LocaleFor(c.env, c.goos)
			if c.want == "" {
				if !slices.Equal(got, before) {
					t.Errorf("environment was changed: %v", got)
				}
				return
			}
			if len(got) != len(before)+1 || got[len(got)-1] != c.want {
				t.Errorf("got %v, want %v appended", got, c.want)
			}
		})
	}
}
