package analyzer

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

// Test helpers

func readyPod(namespace, name string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func unhealthyPod(namespace, name string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

func runningNotReadyPod(namespace, name string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
		},
	}
}

func pdbWithMinAvailable(namespace, name string, selector map[string]string, minAvail intstr.IntOrString) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvail,
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
		},
	}
}

func pdbWithMaxUnavailable(namespace, name string, selector map[string]string, maxUnavail intstr.IntOrString) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavail,
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
		},
	}
}

func extractIssueTypes(issues []Issue) []IssueType {
	types := make([]IssueType, len(issues))
	for i, issue := range issues {
		types[i] = issue.Type
	}
	return types
}

func issueTypesEqual(a, b []IssueType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Tests

func TestIsPodHealthy(t *testing.T) {
	tests := []struct {
		name string
		pod  corev1.Pod
		want bool
	}{
		{
			name: "running and ready",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "running but not ready",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			want: false,
		},
		{
			name: "pending phase",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			want: false,
		},
		{
			name: "succeeded phase",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			},
			want: false,
		},
		{
			name: "failed phase",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			want: false,
		},
		{
			name: "running with no conditions",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPodHealthy(tt.pod); got != tt.want {
				t.Errorf("isPodHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAlwaysBlocking(t *testing.T) {
	tests := []struct {
		name    string
		spec    policyv1.PodDisruptionBudgetSpec
		want    bool
		wantMsg string
	}{
		{
			name: "maxUnavailable 0",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromInt32(0)),
			},
			want:    true,
			wantMsg: "maxUnavailable is 0: no disruptions are ever allowed",
		},
		{
			name: "maxUnavailable 0%",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromString("0%")),
			},
			want:    true,
			wantMsg: "maxUnavailable is 0%: no disruptions are ever allowed",
		},
		{
			name: "minAvailable 100%",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromString("100%")),
			},
			want:    true,
			wantMsg: "minAvailable is 100%: no disruptions are ever allowed",
		},
		{
			name: "maxUnavailable 1 is not always blocking",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromInt32(1)),
			},
			want: false,
		},
		{
			name: "maxUnavailable 50% is not always blocking",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromString("50%")),
			},
			want: false,
		},
		{
			name: "minAvailable 1 is not always blocking",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromInt32(1)),
			},
			want: false,
		},
		{
			name: "minAvailable 50% is not always blocking",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromString("50%")),
			},
			want: false,
		},
		{
			name: "minAvailable 99% is not always blocking",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromString("99%")),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, msg := isAlwaysBlocking(tt.spec)
			if got != tt.want {
				t.Errorf("isAlwaysBlocking() = %v, want %v", got, tt.want)
			}
			if tt.want && msg != tt.wantMsg {
				t.Errorf("isAlwaysBlocking() message = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestComputeDisruptionsAllowed(t *testing.T) {
	tests := []struct {
		name        string
		spec        policyv1.PodDisruptionBudgetSpec
		totalPods   int
		healthyPods int
		want        int
	}{
		{
			name: "maxUnavailable 1 with 3 healthy pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromInt32(1)),
			},
			totalPods:   3,
			healthyPods: 3,
			want:        1,
		},
		{
			name: "maxUnavailable 1 with 2 total but 1 healthy",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromInt32(1)),
			},
			totalPods:   2,
			healthyPods: 1,
			want:        0,
		},
		{
			name: "minAvailable 1 with 3 healthy pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromInt32(1)),
			},
			totalPods:   3,
			healthyPods: 3,
			want:        2,
		},
		{
			name: "minAvailable 1 with 1 healthy pod",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromInt32(1)),
			},
			totalPods:   1,
			healthyPods: 1,
			want:        0,
		},
		{
			name: "minAvailable 2 with 2 healthy pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromInt32(2)),
			},
			totalPods:   2,
			healthyPods: 2,
			want:        0,
		},
		{
			name: "minAvailable 5 with 2 healthy pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromInt32(5)),
			},
			totalPods:   2,
			healthyPods: 2,
			want:        0,
		},
		{
			name: "minAvailable 50% with 2 healthy pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromString("50%")),
			},
			totalPods:   2,
			healthyPods: 2,
			want:        1, // ceil(50%*2) = 1, 2-1 = 1
		},
		{
			name: "minAvailable 50% with 1 healthy pod",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromString("50%")),
			},
			totalPods:   1,
			healthyPods: 1,
			want:        0, // ceil(50%*1) = 1, 1-1 = 0
		},
		{
			name: "maxUnavailable 50% with 3 healthy pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromString("50%")),
			},
			totalPods:   3,
			healthyPods: 3,
			want:        2, // ceil(50%*3)=2, desiredHealthy=3-2=1, allowed=3-1=2
		},
		{
			name: "maxUnavailable 30% with 3 healthy pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: intOrStringPtr(intstr.FromString("30%")),
			},
			totalPods:   3,
			healthyPods: 3,
			want:        1, // ceil(30%*3)=ceil(0.9)=1, desiredHealthy=3-1=2, allowed=3-2=1
		},
		{
			name: "neither set defaults to minAvailable 1",
			spec: policyv1.PodDisruptionBudgetSpec{},
			totalPods:   3,
			healthyPods: 3,
			want:        2, // desiredHealthy=1, 3-1=2
		},
		{
			name: "minAvailable 75% with 2 pods",
			spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intOrStringPtr(intstr.FromString("75%")),
			},
			totalPods:   2,
			healthyPods: 2,
			want:        0, // ceil(75%*2)=ceil(1.5)=2, 2-2=0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeDisruptionsAllowed(tt.spec, tt.totalPods, tt.healthyPods)
			if got != tt.want {
				t.Errorf("computeDisruptionsAllowed() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAnalyze(t *testing.T) {
	labels := map[string]string{"app": "test"}

	type expected struct {
		name               string
		namespace          string
		issueTypes         []IssueType
		disruptionsAllowed int
		expectedPods       int
		currentHealthy     int
	}

	tests := []struct {
		name      string
		objects   []runtime.Object
		namespace string
		expected  []expected
	}{
		{
			name: "maxUnavailable 0 always blocks",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromInt32(0)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueAlwaysBlocking},
					disruptionsAllowed: 0,
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
		{
			name: "maxUnavailable 0% always blocks",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromString("0%")),
				readyPod("default", "pod-1", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueAlwaysBlocking},
					disruptionsAllowed: 0,
					expectedPods:       1,
					currentHealthy:     1,
				},
			},
		},
		{
			name: "minAvailable 100% always blocks",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromString("100%")),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueAlwaysBlocking},
					disruptionsAllowed: 0,
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
		{
			name: "minAvailable equals pod count blocks",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(2)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
		{
			name: "minAvailable exceeds pod count blocks",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(5)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
		{
			name: "single pod with minAvailable 1 blocks",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(1)),
				readyPod("default", "pod-1", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       1,
					currentHealthy:     1,
				},
			},
		},
		{
			name: "maxUnavailable 1 with one already unhealthy blocks",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromInt32(1)),
				readyPod("default", "pod-1", labels),
				unhealthyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       2,
					currentHealthy:     1,
				},
			},
		},
		{
			name: "maxUnavailable 1 with running but not ready pod blocks",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromInt32(1)),
				readyPod("default", "pod-1", labels),
				runningNotReadyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       2,
					currentHealthy:     1,
				},
			},
		},
		{
			name: "no matching pods is orphaned",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(1)),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueNoMatchingPods},
					disruptionsAllowed: 0,
					expectedPods:       0,
					currentHealthy:     0,
				},
			},
		},
		{
			name: "selector mismatch is orphaned",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(1)),
				readyPod("default", "pod-1", map[string]string{"app": "other"}),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueNoMatchingPods},
					disruptionsAllowed: 0,
					expectedPods:       0,
					currentHealthy:     0,
				},
			},
		},
		{
			name: "healthy PDB maxUnavailable 1 with 3 pods",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromInt32(1)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
				readyPod("default", "pod-3", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         nil,
					disruptionsAllowed: 1,
					expectedPods:       3,
					currentHealthy:     3,
				},
			},
		},
		{
			name: "healthy PDB minAvailable 1 with 3 pods",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(1)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
				readyPod("default", "pod-3", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         nil,
					disruptionsAllowed: 2,
					expectedPods:       3,
					currentHealthy:     3,
				},
			},
		},
		{
			name: "percentage minAvailable 50% with 1 pod blocks",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromString("50%")),
				readyPod("default", "pod-1", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       1,
					currentHealthy:     1,
				},
			},
		},
		{
			name: "percentage minAvailable 50% with 2 pods is healthy",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromString("50%")),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         nil,
					disruptionsAllowed: 1,
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
		{
			name: "percentage minAvailable 75% with 2 pods blocks",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromString("75%")),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
		{
			name: "maxUnavailable 2 with 3 healthy pods allows 2",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromInt32(2)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
				readyPod("default", "pod-3", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         nil,
					disruptionsAllowed: 2,
					expectedPods:       3,
					currentHealthy:     3,
				},
			},
		},
		{
			name: "namespace filtering returns only matching namespace",
			objects: []runtime.Object{
				pdbWithMinAvailable("ns-a", "pdb-a", labels, intstr.FromInt32(1)),
				readyPod("ns-a", "pod-1", labels),
				pdbWithMinAvailable("ns-b", "pdb-b", labels, intstr.FromInt32(1)),
				readyPod("ns-b", "pod-1", labels),
			},
			namespace: "ns-a",
			expected: []expected{
				{
					name:               "pdb-a",
					namespace:          "ns-a",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       1,
					currentHealthy:     1,
				},
			},
		},
		{
			name: "all namespaces when namespace is empty",
			objects: []runtime.Object{
				pdbWithMinAvailable("ns-a", "pdb-a", labels, intstr.FromInt32(1)),
				readyPod("ns-a", "pod-1", labels),
				pdbWithMinAvailable("ns-b", "pdb-b", labels, intstr.FromInt32(1)),
				readyPod("ns-b", "pod-1", labels),
			},
			namespace: "",
			expected: []expected{
				{
					name:               "pdb-a",
					namespace:          "ns-a",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       1,
					currentHealthy:     1,
				},
				{
					name:               "pdb-b",
					namespace:          "ns-b",
					issueTypes:         []IssueType{IssueCurrentlyBlocking},
					disruptionsAllowed: 0,
					expectedPods:       1,
					currentHealthy:     1,
				},
			},
		},
		{
			name: "pods in different namespace not counted",
			objects: []runtime.Object{
				pdbWithMinAvailable("ns-a", "test-pdb", labels, intstr.FromInt32(1)),
				readyPod("ns-b", "pod-1", labels), // different namespace
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "ns-a",
					issueTypes:         []IssueType{IssueNoMatchingPods},
					disruptionsAllowed: 0,
					expectedPods:       0,
					currentHealthy:     0,
				},
			},
		},
		{
			name: "mixed healthy and unhealthy pods",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(2)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
				readyPod("default", "pod-3", labels),
				unhealthyPod("default", "pod-4", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         nil,
					disruptionsAllowed: 1,
					expectedPods:       4,
					currentHealthy:     3,
				},
			},
		},
		{
			name: "no PDBs returns empty results",
			objects: []runtime.Object{
				readyPod("default", "pod-1", labels),
			},
			expected: []expected{},
		},
		{
			name: "maxUnavailable larger than pod count",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromInt32(10)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         nil,
					disruptionsAllowed: 2, // can't disrupt more than exist: healthy(2) - max(0, 2-10) = 2
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
		{
			name: "matchExpressions selector works",
			objects: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pdb", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						MinAvailable: intOrStringPtr(intstr.FromInt32(1)),
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"test", "test2"},
								},
							},
						},
					},
				},
				readyPod("default", "pod-1", map[string]string{"app": "test"}),
				readyPod("default", "pod-2", map[string]string{"app": "test2"}),
				readyPod("default", "pod-3", map[string]string{"app": "other"}),
			},
			expected: []expected{
				{
					name:               "test-pdb",
					namespace:          "default",
					issueTypes:         nil,
					disruptionsAllowed: 1,
					expectedPods:       2,
					currentHealthy:     2,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			a := New(client)

			results, err := a.Analyze(context.Background(), tt.namespace)
			if err != nil {
				t.Fatalf("Analyze() unexpected error: %v", err)
			}

			if len(results) != len(tt.expected) {
				t.Fatalf("Analyze() returned %d results, want %d", len(results), len(tt.expected))
			}

			for i, want := range tt.expected {
				got := results[i]

				if got.Name != want.name {
					t.Errorf("result[%d].Name = %q, want %q", i, got.Name, want.name)
				}
				if got.Namespace != want.namespace {
					t.Errorf("result[%d].Namespace = %q, want %q", i, got.Namespace, want.namespace)
				}
				if got.DisruptionsAllowed != want.disruptionsAllowed {
					t.Errorf("result[%d].DisruptionsAllowed = %d, want %d", i, got.DisruptionsAllowed, want.disruptionsAllowed)
				}
				if got.ExpectedPods != want.expectedPods {
					t.Errorf("result[%d].ExpectedPods = %d, want %d", i, got.ExpectedPods, want.expectedPods)
				}
				if got.CurrentHealthy != want.currentHealthy {
					t.Errorf("result[%d].CurrentHealthy = %d, want %d", i, got.CurrentHealthy, want.currentHealthy)
				}

				gotTypes := extractIssueTypes(got.Issues)
				if !issueTypesEqual(gotTypes, want.issueTypes) {
					t.Errorf("result[%d] issue types = %v, want %v", i, gotTypes, want.issueTypes)
				}
			}
		})
	}
}

func TestAnalyze_IssueMessages(t *testing.T) {
	labels := map[string]string{"app": "test"}

	tests := []struct {
		name        string
		objects     []runtime.Object
		wantMsgSub  string // substring expected in the issue message
	}{
		{
			name: "always blocking message mentions maxUnavailable",
			objects: []runtime.Object{
				pdbWithMaxUnavailable("default", "test-pdb", labels, intstr.FromInt32(0)),
				readyPod("default", "pod-1", labels),
			},
			wantMsgSub: "maxUnavailable is 0",
		},
		{
			name: "always blocking message mentions minAvailable 100%",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromString("100%")),
				readyPod("default", "pod-1", labels),
			},
			wantMsgSub: "minAvailable is 100%",
		},
		{
			name: "currently blocking message mentions pod counts",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(2)),
				readyPod("default", "pod-1", labels),
				readyPod("default", "pod-2", labels),
			},
			wantMsgSub: "2 of 2 pod(s) are healthy",
		},
		{
			name: "orphaned PDB message",
			objects: []runtime.Object{
				pdbWithMinAvailable("default", "test-pdb", labels, intstr.FromInt32(1)),
			},
			wantMsgSub: "matches no pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			a := New(client)

			results, err := a.Analyze(context.Background(), "")
			if err != nil {
				t.Fatalf("Analyze() unexpected error: %v", err)
			}

			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}

			if len(results[0].Issues) == 0 {
				t.Fatal("expected at least 1 issue")
			}

			msg := results[0].Issues[0].Message
			if !containsSubstring(msg, tt.wantMsgSub) {
				t.Errorf("issue message %q does not contain %q", msg, tt.wantMsgSub)
			}
		})
	}
}

func TestResult_HasIssues(t *testing.T) {
	t.Run("no issues", func(t *testing.T) {
		r := Result{}
		if r.HasIssues() {
			t.Error("HasIssues() = true, want false")
		}
	})

	t.Run("with issues", func(t *testing.T) {
		r := Result{
			Issues: []Issue{{Type: IssueAlwaysBlocking, Severity: SeverityError, Message: "test"}},
		}
		if !r.HasIssues() {
			t.Error("HasIssues() = false, want true")
		}
	})
}

// Helpers

func intOrStringPtr(v intstr.IntOrString) *intstr.IntOrString {
	return &v
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
