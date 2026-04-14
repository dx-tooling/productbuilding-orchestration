package domain

// filterTools returns the subset of ToolDefinitions() whose names are in the keep set.
func filterTools(keep map[string]bool) []ToolDef {
	var out []ToolDef
	for _, td := range ToolDefinitions() {
		if keep[td.Function.Name] {
			out = append(out, td)
		}
	}
	return out
}

// IssueCreatorTools returns tools for the issue_creator specialist.
func IssueCreatorTools() []ToolDef {
	return filterTools(map[string]bool{
		"search_github_issues": true,
		"create_github_issue":  true,
	})
}

// DelegatorTools returns tools for the delegator specialist.
func DelegatorTools() []ToolDef {
	return filterTools(map[string]bool{
		"get_github_issue":   true,
		"add_github_comment": true,
	})
}

// CommenterTools returns tools for the commenter specialist.
func CommenterTools() []ToolDef {
	return filterTools(map[string]bool{
		"get_github_issue":   true,
		"add_github_comment": true,
	})
}

// ResearcherTools returns tools for the researcher specialist.
func ResearcherTools() []ToolDef {
	return filterTools(map[string]bool{
		"search_github_issues":    true,
		"get_github_issue":        true,
		"list_github_issues":      true,
		"search_pr_diff":          true,
		"search_repo_code":        true,
		"get_file_contents":       true,
		"list_conversations":      true,
		"list_workflow_runs":      true,
		"get_workflow_run_jobs":   true,
		"get_job_failure_context": true,
	})
}

// EventNarratorTools returns tools for the event_narrator specialist (none — tool-free).
func EventNarratorTools() []ToolDef {
	return []ToolDef{}
}

// CloserTools returns tools for the closer specialist.
func CloserTools() []ToolDef {
	return filterTools(map[string]bool{
		"get_github_issue":   true,
		"close_github_issue": true,
		"close_github_pr":    true,
	})
}
