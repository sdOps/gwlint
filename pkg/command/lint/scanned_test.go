package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// This line is the only thing distinguishing "checked everything and it is
// clean" from "loaded nothing a check applies to", and it is read by people
// who have never seen gwlint before, so its wording is part of the contract.
func TestScanSummaryLine(t *testing.T) {
	cases := []struct {
		name    string
		summary scanSummary
		want    string
	}{
		{
			name: "names the kind it checked",
			summary: scanSummary{
				Objects: 98, Files: 38, Checked: 2,
				CheckedByKind: map[string]int{"BackendTrafficPolicy": 2},
				LooksFor:      []string{"BackendTrafficPolicy"},
			},
			want: "Checked 2 BackendTrafficPolicy objects (98 objects loaded from 38 files).",
		},
		{
			name: "says what was missing when nothing matched",
			summary: scanSummary{
				Objects: 2, Files: 2, CheckedByKind: map[string]int{},
				LooksFor: []string{"BackendTrafficPolicy"},
			},
			want: "No BackendTrafficPolicy objects found, so nothing was checked (2 objects loaded from 2 files).",
		},
		{
			name: "lists every kind it looks for when there are several",
			summary: scanSummary{
				Objects: 5, Files: 3, CheckedByKind: map[string]int{},
				LooksFor: []string{"BackendTrafficPolicy", "HTTPRoute"},
			},
			want: "No BackendTrafficPolicy or HTTPRoute objects found, so nothing was checked (5 objects loaded from 3 files).",
		},
		{
			name: "three or more kinds read as a list",
			summary: scanSummary{
				Objects: 5, Files: 3, CheckedByKind: map[string]int{},
				LooksFor: []string{"Backend", "BackendTrafficPolicy", "HTTPRoute"},
			},
			want: "No Backend, BackendTrafficPolicy or HTTPRoute objects found, so nothing was checked " +
				"(5 objects loaded from 3 files).",
		},
		{
			name: "breaks down by kind once more than one matches",
			summary: scanSummary{
				Objects: 98, Files: 38, Checked: 51,
				CheckedByKind: map[string]int{"HTTPRoute": 49, "BackendTrafficPolicy": 2},
				LooksFor:      []string{"BackendTrafficPolicy", "HTTPRoute"},
			},
			want: "Checked 51 objects: 49 HTTPRoute, 2 BackendTrafficPolicy (98 objects loaded from 38 files).",
		},
		{
			name: "singulars read correctly",
			summary: scanSummary{
				Objects: 1, Files: 1, Checked: 1,
				CheckedByKind: map[string]int{"BackendTrafficPolicy": 1},
				LooksFor:      []string{"BackendTrafficPolicy"},
			},
			want: "Checked 1 BackendTrafficPolicy object (1 object loaded from 1 file).",
		},
		{
			name: "an empty run is not silently clean",
			summary: scanSummary{
				CheckedByKind: map[string]int{},
				LooksFor:      []string{"BackendTrafficPolicy"},
			},
			want: "No BackendTrafficPolicy objects found, so nothing was checked (0 objects loaded from 0 files).",
		},
		{
			name: "unparsed objects are reported",
			summary: scanSummary{
				Objects: 4, Files: 5, Checked: 1,
				CheckedByKind: map[string]int{"BackendTrafficPolicy": 1},
				LooksFor:      []string{"BackendTrafficPolicy"},
				Unparsed:      2,
			},
			want: "Checked 1 BackendTrafficPolicy object (4 objects loaded from 5 files). " +
				"2 objects could not be parsed and were not checked; re-run with --verbose to see them.",
		},
		{
			name: "a single unparsed object reads correctly",
			summary: scanSummary{
				Objects: 4, Files: 5, Checked: 1,
				CheckedByKind: map[string]int{"BackendTrafficPolicy": 1},
				LooksFor:      []string{"BackendTrafficPolicy"},
				Unparsed:      1,
			},
			want: "Checked 1 BackendTrafficPolicy object (4 objects loaded from 5 files). " +
				"1 object could not be parsed and was not checked; re-run with --verbose to see them.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.summary.String())
		})
	}
}

// The by-kind breakdown is ordered so the same input always renders the same
// way, commonest kind first and ties broken by name.
func TestScanSummaryKindOrderIsStable(t *testing.T) {
	counts := map[string]int{"HTTPRoute": 3, "BackendTrafficPolicy": 3, "TCPRoute": 9}
	for range 20 {
		assert.Equal(t, "9 TCPRoute, 3 BackendTrafficPolicy, 3 HTTPRoute", byKind(counts))
	}
}
