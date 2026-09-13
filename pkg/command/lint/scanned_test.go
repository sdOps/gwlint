package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The summary line is the only thing distinguishing "checked everything and it
// is clean" from "loaded nothing a check applies to", so its wording matters.
func TestScanSummaryLine(t *testing.T) {
	cases := []struct {
		name    string
		summary scanSummary
		want    string
	}{
		{
			name:    "nothing in scope is called out",
			summary: scanSummary{Objects: 98, InScope: 0, Files: 38},
			want:    "Checked 98 objects from 38 files, none of which any enabled check applies to.",
		},
		{
			name:    "objects in scope are counted",
			summary: scanSummary{Objects: 98, InScope: 2, Files: 38},
			want:    "Checked 98 objects from 38 files, 2 in scope for the enabled checks.",
		},
		{
			name:    "singulars read correctly",
			summary: scanSummary{Objects: 1, InScope: 1, Files: 1},
			want:    "Checked 1 object from 1 file, 1 in scope for the enabled checks.",
		},
		{
			name:    "an empty run is not silently clean",
			summary: scanSummary{},
			want:    "Checked 0 objects from 0 files, none of which any enabled check applies to.",
		},
		{
			name:    "unparsed objects are reported",
			summary: scanSummary{Objects: 4, InScope: 1, Files: 5, Unparsed: 2},
			want: "Checked 4 objects from 5 files, 1 in scope for the enabled checks. " +
				"2 objects could not be parsed and were not checked; re-run with --verbose to see them.",
		},
		{
			name:    "a single unparsed object reads correctly",
			summary: scanSummary{Objects: 4, InScope: 1, Files: 5, Unparsed: 1},
			want: "Checked 4 objects from 5 files, 1 in scope for the enabled checks. " +
				"1 object could not be parsed and was not checked; re-run with --verbose to see them.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.summary.String())
		})
	}
}
