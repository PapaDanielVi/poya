package provider_test

import (
	"testing"

	"github.com/PapaDanielVi/poya/provider"
)

func TestCommonPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		keys []string
		want string
	}{
		{"empty", nil, ""},
		{"single", []string{"myapp/timeout"}, "myapp/timeout"},
		{"shared prefix", []string{"myapp/timeout", "myapp/db/host", "myapp/db/port"}, "myapp/"},
		{"no shared prefix", []string{"a/x", "b/y"}, ""},
		{"identical", []string{"k", "k"}, "k"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := provider.CommonPrefix(tc.keys); got != tc.want {
				t.Fatalf("CommonPrefix(%v) = %q, want %q", tc.keys, got, tc.want)
			}
		})
	}
}
