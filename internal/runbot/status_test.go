package runbot

import "testing"

func b(result, state string) *Build {
	return &Build{GlobalResult: result, GlobalState: state}
}

func TestSummarize(t *testing.T) {
	for _, tc := range []struct {
		name        string
		slots       []Slot
		want        string
		wantFailing int
	}{
		{"all green", []Slot{{Trigger: "a", Build: b("ok", "done")}, {Trigger: "b", Build: b("ok", "done")}}, StateSuccess, 0},
		{"one failure wins", []Slot{{Trigger: "a", Build: b("ok", "done")}, {Trigger: "b", Build: b("ko", "done")}}, StateFailure, 1},
		{"failure beats pending", []Slot{{Trigger: "a", Build: b("", "testing")}, {Trigger: "b", Build: b("ko", "done")}}, StateFailure, 1},
		{"still testing", []Slot{{Trigger: "a", Build: b("ok", "done")}, {Trigger: "b", Build: b("", "testing")}}, StatePending, 0},
		{"slot without a build is pending", []Slot{{Trigger: "a", Build: b("ok", "done")}, {Trigger: "b"}}, StatePending, 0},
		{"killed counts as failure", []Slot{{Trigger: "a", Build: b("killed", "done")}}, StateFailure, 1},
		{"no slots", nil, StateUnknown, 0},
		{"only empty slots", []Slot{{Trigger: "a"}}, StatePending, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, failing := summarize(tc.slots)
			if got != tc.want {
				t.Errorf("state = %q, want %q", got, tc.want)
			}
			if len(failing) != tc.wantFailing {
				t.Errorf("failing = %d, want %d", len(failing), tc.wantFailing)
			}
		})
	}
}
