package rtspeek

import "testing"

func TestClassifyError(t *testing.T) {
	cases := []struct {
		in   error
		want string
	}{
		{in: wrapErr("connection refused"), want: "connection_refused"},
		{in: wrapErr("i/o timeout"), want: "timeout"},
		{in: wrapErr("no such host"), want: "dns_error"},
		{in: wrapErr("401 unauthorized"), want: "auth_required"},
		{in: wrapErr("random other"), want: "other"},
	}
	for _, c := range cases {
		if got := classifyError(c.in); got != c.want {
			t.Fatalf("classifyError(%v)=%s want %s", c.in, got, c.want)
		}
	}
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }
func wrapErr(s string) error      { return simpleErr(s) }
