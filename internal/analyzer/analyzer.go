package analyzer

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// Analyzer examines PodDisruptionBudgets in a Kubernetes cluster and
// identifies configurations that may prevent pod evictions.
type Analyzer struct {
	client kubernetes.Interface
}

// New creates a new Analyzer with the given Kubernetes client.
func New(client kubernetes.Interface) *Analyzer {
	return &Analyzer{client: client}
}

// Analyze examines all PDBs in the given namespace and returns analysis
// results. Pass an empty string to analyze all namespaces.
func (a *Analyzer) Analyze(ctx context.Context, namespace string) ([]Result, error) {
	pdbs, err := a.client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing PDBs: %w", err)
	}

	results := make([]Result, 0, len(pdbs.Items))
	for i := range pdbs.Items {
		result, err := a.analyzePDB(ctx, pdbs.Items[i])
		if err != nil {
			return nil, fmt.Errorf("analyzing PDB %s/%s: %w", pdbs.Items[i].Namespace, pdbs.Items[i].Name, err)
		}
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Namespace != results[j].Namespace {
			return results[i].Namespace < results[j].Namespace
		}
		return results[i].Name < results[j].Name
	})

	return results, nil
}

func (a *Analyzer) analyzePDB(ctx context.Context, pdb policyv1.PodDisruptionBudget) (Result, error) {
	result := Result{
		Namespace: pdb.Namespace,
		Name:      pdb.Name,
	}

	if pdb.Spec.MinAvailable != nil {
		result.MinAvailable = pdb.Spec.MinAvailable.String()
	}
	if pdb.Spec.MaxUnavailable != nil {
		result.MaxUnavailable = pdb.Spec.MaxUnavailable.String()
	}

	matchingPods, err := a.findMatchingPods(ctx, pdb.Namespace, pdb.Spec.Selector)
	if err != nil {
		return result, fmt.Errorf("finding matching pods: %w", err)
	}

	totalPods := len(matchingPods)
	healthyPods := countHealthyPods(matchingPods)

	result.ExpectedPods = totalPods
	result.CurrentHealthy = healthyPods

	// No pods matched the selector: orphaned PDB.
	if totalPods == 0 {
		result.Issues = append(result.Issues, Issue{
			Type:     IssueNoMatchingPods,
			Severity: SeverityWarning,
			Message:  "PDB selector matches no pods; this PDB is not protecting any workload",
		})
		return result, nil
	}

	// Structural check: PDB can never allow disruptions, regardless of pod count.
	if blocking, msg := isAlwaysBlocking(pdb.Spec); blocking {
		result.DisruptionsAllowed = 0
		result.Issues = append(result.Issues, Issue{
			Type:     IssueAlwaysBlocking,
			Severity: SeverityError,
			Message:  msg,
		})
		return result, nil
	}

	// Compute allowed disruptions based on spec and current pod state.
	disruptionsAllowed := computeDisruptionsAllowed(pdb.Spec, totalPods, healthyPods)
	result.DisruptionsAllowed = disruptionsAllowed

	if disruptionsAllowed == 0 {
		msg := currentlyBlockingMessage(pdb.Spec, totalPods, healthyPods)
		result.Issues = append(result.Issues, Issue{
			Type:     IssueCurrentlyBlocking,
			Severity: SeverityError,
			Message:  msg,
		})
	}

	return result, nil
}

func (a *Analyzer) findMatchingPods(ctx context.Context, namespace string, pdbSelector *metav1.LabelSelector) ([]corev1.Pod, error) {
	if pdbSelector == nil {
		return nil, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(pdbSelector)
	if err != nil {
		return nil, fmt.Errorf("converting label selector: %w", err)
	}

	pods, err := a.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	var matching []corev1.Pod
	for _, pod := range pods.Items {
		if selector.Matches(labels.Set(pod.Labels)) {
			matching = append(matching, pod)
		}
	}
	return matching, nil
}

func countHealthyPods(pods []corev1.Pod) int {
	count := 0
	for _, pod := range pods {
		if isPodHealthy(pod) {
			count++
		}
	}
	return count
}

func isPodHealthy(pod corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isAlwaysBlocking(spec policyv1.PodDisruptionBudgetSpec) (bool, string) {
	if mu := spec.MaxUnavailable; mu != nil {
		if mu.Type == intstr.Int && mu.IntValue() == 0 {
			return true, "maxUnavailable is 0: no disruptions are ever allowed"
		}
		if mu.Type == intstr.String && isZeroPercent(mu.String()) {
			return true, fmt.Sprintf("maxUnavailable is %s: no disruptions are ever allowed", mu.String())
		}
	}

	if ma := spec.MinAvailable; ma != nil {
		if ma.Type == intstr.String && isHundredPercent(ma.String()) {
			return true, fmt.Sprintf("minAvailable is %s: no disruptions are ever allowed", ma.String())
		}
	}

	return false, ""
}

func currentlyBlockingMessage(spec policyv1.PodDisruptionBudgetSpec, totalPods, healthyPods int) string {
	if spec.MinAvailable != nil {
		resolved := resolveIntOrPercent(spec.MinAvailable, totalPods, true)
		return fmt.Sprintf(
			"minAvailable requires %d pod(s) but only %d of %d pod(s) are healthy, allowing 0 disruptions",
			resolved, healthyPods, totalPods,
		)
	}

	if spec.MaxUnavailable != nil {
		resolved := resolveIntOrPercent(spec.MaxUnavailable, totalPods, true)
		unavailable := totalPods - healthyPods
		return fmt.Sprintf(
			"maxUnavailable allows %d but %d of %d pod(s) are already unavailable, allowing 0 additional disruptions",
			resolved, unavailable, totalPods,
		)
	}

	return "PDB currently allows 0 disruptions"
}

func computeDisruptionsAllowed(spec policyv1.PodDisruptionBudgetSpec, totalPods, healthyPods int) int {
	var desiredHealthy int

	if spec.MaxUnavailable != nil {
		maxUnavail := resolveIntOrPercent(spec.MaxUnavailable, totalPods, true)
		desiredHealthy = totalPods - maxUnavail
		if desiredHealthy < 0 {
			desiredHealthy = 0
		}
	} else if spec.MinAvailable != nil {
		desiredHealthy = resolveIntOrPercent(spec.MinAvailable, totalPods, true)
	} else {
		// Kubernetes defaults to minAvailable=1 when neither is specified.
		desiredHealthy = 1
	}

	allowed := healthyPods - desiredHealthy
	if allowed < 0 {
		allowed = 0
	}
	return allowed
}

func resolveIntOrPercent(val *intstr.IntOrString, total int, roundUp bool) int {
	if val == nil {
		return 0
	}

	if val.Type == intstr.Int {
		return val.IntValue()
	}

	pct := parsePercentage(val.String())
	value := float64(pct) / 100.0 * float64(total)
	if roundUp {
		return int(math.Ceil(value))
	}
	return int(math.Floor(value))
}

func parsePercentage(s string) int {
	s = strings.TrimSuffix(s, "%")
	v, _ := strconv.Atoi(s)
	return v
}

func isZeroPercent(s string) bool {
	s = strings.TrimSuffix(s, "%")
	v, err := strconv.Atoi(s)
	return err == nil && v == 0
}

func isHundredPercent(s string) bool {
	s = strings.TrimSuffix(s, "%")
	v, err := strconv.Atoi(s)
	return err == nil && v >= 100
}
