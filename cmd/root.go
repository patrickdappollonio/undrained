package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/patrickdappollonio/undrained/internal/analyzer"
	"github.com/patrickdappollonio/undrained/internal/kube"
	"github.com/spf13/cobra"
)

// ErrIssuesFound is returned when problematic PDBs are detected.
// This is not a runtime error but a signal for non-zero exit.
var ErrIssuesFound = errors.New("problematic PDBs found")

var (
	kubeconfig  string
	kubecontext string
	namespace   string
	output      string
	showAll     bool
)

// NewRootCommand creates the root cobra command.
func NewRootCommand(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "undrained",
		Short:   "Analyze PodDisruptionBudgets for misconfigurations",
		Version: version,
		Long: `undrained examines PodDisruptionBudgets in your Kubernetes cluster
and identifies configurations that prevent or hinder pod evictions
during voluntary disruptions such as node drains, cluster upgrades,
and scaling operations.

It detects:
  - PDBs that structurally block all evictions (maxUnavailable=0, minAvailable=100%)
  - PDBs where current pod count leaves zero room for disruption
  - Orphaned PDBs whose selectors match no pods

Exit codes:
  0  No issues found
  1  Problematic PDBs detected
  2  Runtime error`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          run,
	}

	flags := cmd.Flags()
	flags.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file (defaults to KUBECONFIG env or ~/.kube/config)")
	flags.StringVar(&kubecontext, "context", "", "kubernetes context to use")
	flags.StringVarP(&namespace, "namespace", "n", "", "namespace to analyze (default: all namespaces)")
	flags.StringVarP(&output, "output", "o", "table", "output format: table, json, wide")
	flags.BoolVarP(&showAll, "all", "a", false, "show all PDBs including healthy ones")

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	client, err := kube.NewClient(kubeconfig, kubecontext)
	if err != nil {
		return err
	}

	a := analyzer.New(client)
	results, err := a.Analyze(cmd.Context(), namespace)
	if err != nil {
		return err
	}

	hasIssues := false
	for _, r := range results {
		if r.HasIssues() {
			hasIssues = true
			break
		}
	}

	// Filter to only problematic PDBs unless --all is set.
	displayResults := results
	if !showAll {
		filtered := make([]analyzer.Result, 0)
		for _, r := range results {
			if r.HasIssues() {
				filtered = append(filtered, r)
			}
		}
		displayResults = filtered
	}

	switch output {
	case "json":
		if err := outputJSON(displayResults); err != nil {
			return err
		}
	case "wide":
		if err := outputWide(displayResults); err != nil {
			return err
		}
	default:
		if err := outputTable(displayResults); err != nil {
			return err
		}
	}

	if hasIssues {
		return ErrIssuesFound
	}
	return nil
}

func outputJSON(results []analyzer.Result) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func outputTable(results []analyzer.Result) error {
	if len(results) == 0 {
		fmt.Println("No problematic PDBs found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAMESPACE\tNAME\tALLOWED\tISSUES")

	for _, r := range results {
		issues := formatIssues(r.Issues)
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			r.Namespace, r.Name, r.DisruptionsAllowed, issues)
	}

	return w.Flush()
}

func outputWide(results []analyzer.Result) error {
	if len(results) == 0 {
		fmt.Println("No problematic PDBs found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAMESPACE\tNAME\tMIN-AVAIL\tMAX-UNAVAIL\tPODS\tHEALTHY\tALLOWED\tISSUES")

	for _, r := range results {
		minAvail := r.MinAvailable
		if minAvail == "" {
			minAvail = "-"
		}
		maxUnavail := r.MaxUnavailable
		if maxUnavail == "" {
			maxUnavail = "-"
		}
		issues := formatIssues(r.Issues)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			r.Namespace, r.Name, minAvail, maxUnavail,
			r.ExpectedPods, r.CurrentHealthy, r.DisruptionsAllowed, issues)
	}

	return w.Flush()
}

func formatIssues(issues []analyzer.Issue) string {
	if len(issues) == 0 {
		return "OK"
	}

	msgs := make([]string, 0, len(issues))
	for _, issue := range issues {
		msgs = append(msgs, issue.Message)
	}
	return strings.Join(msgs, "; ")
}
