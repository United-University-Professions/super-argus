package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Every run of a Job or CronJob must map to one service, not one per run.
func TestServiceFromPodCollapsesJobRuns(t *testing.T) {
	cases := map[string]string{
		// RMS job runner: Job "<prefix>-<uuid>", pod adds a 5-char suffix
		"jobs-runner-prod-3f2b8c1e-9a4d-4e7b-8c21-5d6e7f8a9b0c-x2k4p": "jobs-runner-prod",
		"jobs-runner-dev-0a1b2c3d-4e5f-6789-abcd-ef0123456789-9xk2z":  "jobs-runner-dev",
		// CronJob: "<name>-<scheduled minute>-<suffix>"
		"prod-jobs-29412345-x2k4p": "prod-jobs", // digits outside the generated alphabet
		"prod-jobs-29458888-x2k4p": "prod-jobs", // digits inside it
		// unchanged behaviour
		"api-7d9f8b6c5d-x2k4p": "api",
		"postgres-0":           "postgres",
	}
	for pod, want := range cases {
		if got := serviceFromPod(pod); got != want {
			t.Errorf("serviceFromPod(%q) = %q, want %q", pod, got, want)
		}
	}
}

func jobPod(name string) *corev1.Pod {
	p := pod(name, corev1.PodSucceeded, false)
	p.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: "owner"}}
	return p
}

// Batch pods finish by design: reporting them would show their service as down
// whenever no run is in flight.
func TestPodRollupIgnoresJobPods(t *testing.T) {
	roll := &podRollup{ready: map[string]bool{}}
	r := NewLivenessReporter("http://x", "k")

	roll.update(jobPod("jobs-runner-prod-3f2b8c1e-9a4d-4e7b-8c21-5d6e7f8a9b0c-x2k4p"), r)
	if len(r.pending) != 0 {
		t.Fatalf("job pods must not be liveness-tracked, got %+v", r.pending)
	}
	roll.update(pod("api-7d9f8b6c5d-x2k4p", corev1.PodRunning, true), r) // non-job pods still are
	if !r.pending["api"].Alive {
		t.Fatalf("regular pods must still report, got %+v", r.pending)
	}
}
