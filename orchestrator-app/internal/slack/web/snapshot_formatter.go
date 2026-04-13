package web

import (
	"fmt"
	"strings"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
)

// FormatFeatureSummary produces a compact multi-line summary string from a FeatureSnapshot.
// Only non-empty fields are included. Returns empty string for nil snapshot.
func FormatFeatureSummary(snap *featurecontext.FeatureSnapshot) string {
	if snap == nil {
		return ""
	}

	var lines []string

	if snap.Issue != nil {
		lines = append(lines, fmt.Sprintf("Issue: #%d %s (%s)", snap.Issue.Number, snap.Issue.Title, snap.Issue.State))
	}

	if snap.PR != nil {
		pr := snap.PR
		prLine := fmt.Sprintf("PR: #%d by %s — %s", pr.Number, pr.Author, pr.State)
		if pr.Additions > 0 || pr.Deletions > 0 {
			prLine += fmt.Sprintf(", +%d/-%d lines", pr.Additions, pr.Deletions)
		}
		if pr.HeadRef != "" {
			prLine += fmt.Sprintf(", branch %s", pr.HeadRef)
		}
		if pr.Merged {
			prLine = fmt.Sprintf("PR: #%d by %s — merged", pr.Number, pr.Author)
		}
		lines = append(lines, prLine)
	}

	if snap.CIStatus != featurecontext.CIUnknown && len(snap.CIDetails) > 0 {
		var parts []string
		for _, c := range snap.CIDetails {
			conclusion := c.Conclusion
			if conclusion == "" {
				conclusion = "running"
			}
			parts = append(parts, fmt.Sprintf("%s %s", c.Name, conclusion))
		}
		lines = append(lines, fmt.Sprintf("CI: %s", strings.Join(parts, ", ")))
	}

	if snap.Preview != nil {
		previewLine := fmt.Sprintf("Preview: %s", snap.Preview.Status)
		if snap.Preview.URL != "" {
			previewLine += fmt.Sprintf(" at %s", snap.Preview.URL)
		}
		lines = append(lines, previewLine)
	}

	return strings.Join(lines, "\n")
}
