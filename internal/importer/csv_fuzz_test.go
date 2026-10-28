package importer

import (
	"context"
	"strings"
	"testing"
)

// An uploaded CSV of any shape imports or fails with an error — never a panic.
func FuzzImportCSV(f *testing.F) {
	f.Add(linearCSV, false)
	f.Add("Issue key,Summary,Status,Priority,Created\nPROJ-1,a,Done,High,01/Jan/26 9:00 AM\n", true)
	f.Add("\ufeffID,Title\n\"unterminated", false)
	f.Add("Summary,Summary,Summary\n,,\n", true)
	f.Fuzz(func(t *testing.T, csv string, jira bool) {
		imp, _ := newTestImporter()
		if jira {
			_, _ = imp.ImportJiraCSV(context.Background(), "ws-1", "team-1", strings.NewReader(csv))
		} else {
			_, _ = imp.ImportLinearCSV(context.Background(), "ws-1", "team-1", strings.NewReader(csv))
		}
	})
}
