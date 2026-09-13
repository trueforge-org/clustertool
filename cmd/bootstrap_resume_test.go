package cmd

import (
	"errors"
	"reflect"
	"testing"
)

func TestResumeBootstrapConfirmation(t *testing.T) {
	for _, yes := range []bool{false, true} {
		calls := 0
		cause := errors.New("setup interrupted")
		args := []string{"--mode=auto"}
		err := resumeBootstrap("192.0.2.11", args, func(prompt string, defaultYes bool) bool {
			if prompt != "Resume cluster setup? [y/n]: " || defaultYes {
				t.Fatal(prompt, defaultYes)
			}
			return yes
		}, func(got []string) error {
			calls++
			if !reflect.DeepEqual(got, args) {
				t.Fatal(got)
			}
			return cause
		})
		if yes {
			if calls != 1 || !errors.Is(err, cause) {
				t.Fatal(calls, err)
			}
		} else if calls != 0 || err == nil {
			t.Fatal(calls, err)
		}
	}
}
