package analyzer

// Severity represents the severity level of an issue found in a PDB.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// IssueType categorizes the kind of problem found in a PDB.
type IssueType string

const (
	// IssueAlwaysBlocking indicates the PDB is structurally configured
	// to never allow any pod disruptions regardless of replica count.
	// Examples: maxUnavailable=0, maxUnavailable=0%, minAvailable=100%.
	IssueAlwaysBlocking IssueType = "AlwaysBlocking"

	// IssueCurrentlyBlocking indicates the PDB currently prevents all
	// disruptions because there are not enough healthy pods to satisfy
	// the PDB constraints and still allow evictions.
	IssueCurrentlyBlocking IssueType = "CurrentlyBlocking"

	// IssueNoMatchingPods indicates the PDB's label selector does not
	// match any pods, making it an orphaned resource.
	IssueNoMatchingPods IssueType = "NoMatchingPods"
)

// Issue represents a single problem found with a PDB.
type Issue struct {
	Type     IssueType `json:"type"`
	Severity Severity  `json:"severity"`
	Message  string    `json:"message"`
}

// Result represents the analysis of a single PodDisruptionBudget.
type Result struct {
	Namespace          string  `json:"namespace"`
	Name               string  `json:"name"`
	MinAvailable       string  `json:"minAvailable,omitempty"`
	MaxUnavailable     string  `json:"maxUnavailable,omitempty"`
	CurrentHealthy     int     `json:"currentHealthy"`
	ExpectedPods       int     `json:"expectedPods"`
	DisruptionsAllowed int     `json:"disruptionsAllowed"`
	Issues             []Issue `json:"issues"`
}

// HasIssues returns true if the result contains any issues.
func (r Result) HasIssues() bool {
	return len(r.Issues) > 0
}
