package reconcile

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// healthReader is a client.Reader over a fixed set of objects and errors,
// keyed by "Kind namespace/name". before runs at the start of every Get.
type healthReader struct {
	objs   map[string]map[string]any
	errs   map[string]error
	before func(ctx context.Context, key string) error
}

func (r *healthReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	u := obj.(*unstructured.Unstructured)
	k := u.GetKind() + " " + key.Namespace + "/" + key.Name
	if r.before != nil {
		if err := r.before(ctx, k); err != nil {
			return err
		}
	}
	if err, ok := r.errs[k]; ok {
		return err
	}
	o, ok := r.objs[k]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Resource: strings.ToLower(u.GetKind())}, key.Name)
	}
	u.Object = deepCopyMap(o)
	return nil
}

func (r *healthReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("not used")
}

func deepCopyMap(m map[string]any) map[string]any {
	return (&unstructured.Unstructured{Object: m}).DeepCopy().Object
}

func entry(kind, name string) releasesv1alpha1.InventoryEntry {
	e := releasesv1alpha1.InventoryEntry{Kind: kind, Namespace: "apps", Name: name, Version: "v1"}
	if kind == "Deployment" {
		e.Group = "apps"
	}
	return e
}

func configMap(name string) map[string]any {
	return map[string]any{"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": name, "namespace": "apps"}}
}

// deployment builds the Deployment apps/web at generation 2 with one desired
// replica.
// rolledOut gives it a finished rollout; stalled gives it a rollout stalled
// past its progress deadline at the observed generation.
func deployment(rolledOut, stalled bool) map[string]any {
	st := map[string]any{"observedGeneration": int64(2)}
	if rolledOut {
		st["replicas"], st["updatedReplicas"], st["availableReplicas"] = int64(1), int64(1), int64(1)
	} else {
		st["observedGeneration"] = int64(1)
	}
	if stalled {
		st["observedGeneration"] = int64(2)
		st["conditions"] = []any{map[string]any{
			"type": "Progressing", "status": "False", "reason": "ProgressDeadlineExceeded"}}
	}
	return map[string]any{"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "web", "namespace": "apps", "generation": int64(2)},
		"spec":     map[string]any{"replicas": int64(1)},
		"status":   st}
}

func TestJudgeHealth_Rows(t *testing.T) {
	ctx := context.Background()
	readErr := errors.New("boom")
	tests := []struct {
		name        string
		entries     []releasesv1alpha1.InventoryEntry
		objs        map[string]map[string]any
		errs        map[string]error
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
		wantRequeue healthRequeueClass
	}{
		{
			name:    "every object healthy",
			entries: []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg"), entry("Deployment", "web")},
			objs: map[string]map[string]any{
				"ConfigMap apps/cfg":  configMap("cfg"),
				"Deployment apps/web": deployment(true, false)},
			wantStatus: metav1.ConditionTrue, wantReason: status.RolledOutReason,
			wantMessage: "2/2 objects ready", wantRequeue: healthNoRequeue,
		},
		{
			name:    "a rollout in progress",
			entries: []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg"), entry("Deployment", "web")},
			objs: map[string]map[string]any{
				"ConfigMap apps/cfg":  configMap("cfg"),
				"Deployment apps/web": deployment(false, false)},
			wantStatus: metav1.ConditionFalse, wantReason: status.NotRolledOutReason,
			wantMessage: "1/2 objects ready: Deployment apps/web (NotReady)", wantRequeue: healthBackoff,
		},
		{
			name:       "a missing object",
			entries:    []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg")},
			wantStatus: metav1.ConditionFalse, wantReason: status.NotRolledOutReason,
			wantMessage: "0/1 objects ready: ConfigMap apps/cfg (Missing)", wantRequeue: healthBackoff,
		},
		{
			name:    "a kind no longer served counts as missing",
			entries: []releasesv1alpha1.InventoryEntry{entry("Widget", "w")},
			errs: map[string]error{"Widget apps/w": &apimeta.NoKindMatchError{
				GroupKind: schema.GroupKind{Kind: "Widget"}}},
			wantStatus: metav1.ConditionFalse, wantReason: status.NotRolledOutReason,
			wantMessage: "0/1 objects ready: Widget apps/w (Missing)", wantRequeue: healthBackoff,
		},
		{
			name:       "an unreadable object",
			entries:    []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg"), entry("ConfigMap", "other")},
			objs:       map[string]map[string]any{"ConfigMap apps/cfg": configMap("cfg")},
			errs:       map[string]error{"ConfigMap apps/other": readErr},
			wantStatus: metav1.ConditionUnknown, wantReason: status.HealthUnknownReason,
			wantMessage: "1/2 objects ready: reading ConfigMap apps/other: boom", wantRequeue: healthBackoff,
		},
		{
			name:       "an empty inventory",
			wantStatus: metav1.ConditionUnknown, wantReason: status.HealthUnknownReason,
			wantMessage: "0/0 objects ready: the inventory is empty", wantRequeue: healthNoRequeue,
		},
		{
			name:       "a stalled Deployment outranks a not-ready object",
			entries:    []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "gone"), entry("Deployment", "web")},
			objs:       map[string]map[string]any{"Deployment apps/web": deployment(false, true)},
			wantStatus: metav1.ConditionFalse, wantReason: status.ProgressDeadlineExceededReason,
			wantMessage: "0/2 objects ready: Deployment apps/web (NotReady), ConfigMap apps/gone (Missing)",
			wantRequeue: healthStalled,
		},
		{
			name:       "a not-ready object outranks an unreadable one",
			entries:    []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "bad"), entry("Deployment", "web")},
			objs:       map[string]map[string]any{"Deployment apps/web": deployment(false, false)},
			errs:       map[string]error{"ConfigMap apps/bad": readErr},
			wantStatus: metav1.ConditionFalse, wantReason: status.NotRolledOutReason,
			wantMessage: "0/2 objects ready: Deployment apps/web (NotReady)", wantRequeue: healthBackoff,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := judgeHealth(ctx, &healthReader{objs: tt.objs, errs: tt.errs}, tt.entries)
			assert.Equal(t, tt.wantStatus, v.status)
			assert.Equal(t, tt.wantReason, v.reason)
			assert.Equal(t, tt.wantMessage, v.message)
			assert.Equal(t, tt.wantRequeue, v.requeue)
		})
	}
}

func TestJudgeHealth_MessageNamesFiveAndIsStable(t *testing.T) {
	entries := make([]releasesv1alpha1.InventoryEntry, 0, 6)
	for i := range 6 {
		entries = append(entries, entry("ConfigMap", "cm"+strconv.Itoa(i)))
	}
	r := &healthReader{}
	first := judgeHealth(context.Background(), r, entries)
	second := judgeHealth(context.Background(), r, entries)
	assert.Equal(t, first, second)
	assert.Equal(t, "0/6 objects ready: ConfigMap apps/cm0 (Missing), ConfigMap apps/cm1 (Missing), "+
		"ConfigMap apps/cm2 (Missing), ConfigMap apps/cm3 (Missing), ConfigMap apps/cm4 (Missing), and 1 more",
		first.message)
}

func TestJudgeHealth_StalledDeploymentNamedFirst(t *testing.T) {
	entries := make([]releasesv1alpha1.InventoryEntry, 0, 6)
	for i := range 5 {
		entries = append(entries, entry("ConfigMap", "cm"+strconv.Itoa(i)))
	}
	entries = append(entries, entry("Deployment", "web"))
	r := &healthReader{objs: map[string]map[string]any{"Deployment apps/web": deployment(false, true)}}
	v := judgeHealth(context.Background(), r, entries)
	assert.Equal(t, status.ProgressDeadlineExceededReason, v.reason)
	assert.True(t, strings.HasPrefix(v.message, "0/6 objects ready: Deployment apps/web (NotReady), ConfigMap apps/cm0"),
		v.message)
	assert.True(t, strings.HasSuffix(v.message, "and 1 more"), v.message)
}

func TestJudgeHealth_FirstErrorByInventoryOrder(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "a"), entry("ConfigMap", "b")}
	// "a" finishes last: its read waits until "b" has returned.
	bDone := make(chan struct{})
	r := &healthReader{
		errs: map[string]error{"ConfigMap apps/a": errors.New("a failed"), "ConfigMap apps/b": errors.New("b failed")},
		before: func(ctx context.Context, key string) error {
			if key == "ConfigMap apps/a" {
				<-bDone
			} else {
				defer close(bDone)
			}
			return nil
		},
	}
	v := judgeHealth(context.Background(), r, entries)
	assert.Equal(t, "0/2 objects ready: reading ConfigMap apps/a: a failed", v.message)
}

func TestJudgeHealth_BoundsReadsInFlight(t *testing.T) {
	entries := make([]releasesv1alpha1.InventoryEntry, 0, 40)
	for i := range 40 {
		entries = append(entries, entry("ConfigMap", "cm"+strconv.Itoa(i)))
	}
	var inFlight, peak atomic.Int32
	var mu sync.Mutex
	r := &healthReader{before: func(context.Context, string) error {
		n := inFlight.Add(1)
		mu.Lock()
		if n > peak.Load() {
			peak.Store(n)
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	}}
	judgeHealth(context.Background(), r, entries)
	assert.LessOrEqual(t, peak.Load(), int32(healthReadParallelism))
	assert.Positive(t, peak.Load())
}

func TestJudgeHealth_Deadline(t *testing.T) {
	old := healthReadTimeout
	healthReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { healthReadTimeout = old })

	r := &healthReader{before: func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	start := time.Now()
	v := judgeHealth(context.Background(), r, []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg")})
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Equal(t, metav1.ConditionUnknown, v.status)
	assert.Equal(t, status.HealthUnknownReason, v.reason)
	assert.Contains(t, v.message, "deadline exceeded")
}

func TestUnreadableVerdict(t *testing.T) {
	v := unreadableVerdict([]releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg")}, errors.New("serviceAccount apps/deployer not found"))
	assert.Equal(t, metav1.ConditionUnknown, v.status)
	assert.Equal(t, status.HealthUnknownReason, v.reason)
	assert.Equal(t, "0/1 objects ready: building the health reader: serviceAccount apps/deployer not found", v.message)
	assert.Equal(t, healthBackoff, v.requeue)
}

func TestHealthRequeue(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) *metav1.Time { t := metav1.NewTime(now.Add(-ago)); return &t }
	backoff := healthVerdict{requeue: healthBackoff}
	tests := []struct {
		name string
		v    healthVerdict
		at   *metav1.Time
		want time.Duration
	}{
		{"just applied", backoff, at(0), healthRequeueFloor},
		{"4s ago", backoff, at(4 * time.Second), healthRequeueFloor},
		{"10s ago", backoff, at(10 * time.Second), healthRequeueFloor},
		{"1m ago", backoff, at(time.Minute), 30 * time.Second},
		{"10m ago", backoff, at(10 * time.Minute), healthRequeueCeiling},
		{"future", backoff, at(-time.Minute), healthRequeueFloor},
		{"nil", backoff, nil, healthRequeueFloor},
		{"stalled", healthVerdict{requeue: healthStalled}, at(time.Minute), StalledRecheckInterval},
		{"rolled out", healthVerdict{requeue: healthNoRequeue}, at(time.Minute), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, healthRequeue(tt.v, tt.at, now))
		})
	}
}

func TestPackageRequeue(t *testing.T) {
	assert.Equal(t, time.Minute, packageRequeue(2*time.Minute, time.Minute))
	assert.Equal(t, 5*time.Second, packageRequeue(5*time.Second, time.Minute))
	assert.Equal(t, time.Minute, packageRequeue(0, time.Minute))
}

// TestHealthJudgesOnlyThroughTheLibrary keeps every readiness rule in the
// library's opm/k8s/health (kubernetes-tier-adoption): health.go may fetch
// objects and hand them over, but it reads no field of them.
func TestHealthJudgesOnlyThroughTheLibrary(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "health.go", nil, 0)
	require.NoError(t, err)
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "unstructured" && strings.HasPrefix(x.Sel.Name, "Nested") {
				t.Errorf("health.go calls unstructured.%s; judge readiness only through opm/k8s/health", x.Sel.Name)
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING && x.Value == `"status"` {
				t.Errorf("health.go reads a status field; judge readiness only through opm/k8s/health")
			}
		}
		return true
	})
}

// An instance the manager applied as itself is read through the manager's
// uncached API reader, never its cached client.
func TestManagerReader(t *testing.T) {
	apiReader := &healthReader{}
	assert.Same(t, apiReader, managerReader(apiReader, nil))
	assert.Nil(t, managerReader(nil, nil))
}
