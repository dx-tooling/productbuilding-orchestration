package facade

// PREventDTO is the cross-vertical representation of a GitHub PR event.
type PREventDTO struct {
	Action    string
	RepoOwner string
	RepoName  string
	PRNumber  int
	Branch    string
	HeadSHA   string
}
