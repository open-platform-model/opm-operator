package apply

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	fluxssa "github.com/fluxcd/pkg/ssa"
	ssautils "github.com/fluxcd/pkg/ssa/utils"
	"golang.org/x/mod/modfile"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	libobject "github.com/open-platform-model/library/opm/k8s/object"
)

// The tests in this file guard one rule: Flux's staged apply, which is the
// operator's only apply order, may refine the order the library's kind
// weights give and never contradicts it (0012:D5:R1). Flux's order is read
// by calling the pinned Flux module (its stage rules, its sorter and its
// ReconcileOrder variable), so a Flux bump that moves a kind fails here.
// This file holds no copy of Flux's kind list.

const (
	fluxModule    = "github.com/fluxcd/pkg/ssa"
	libraryModule = "github.com/open-platform-model/library"

	// Groups that sort before every built-in group (the core group aside) and
	// after all of them, so a same-named or custom kind meets Flux's group
	// tie-break on both sides.
	groupBefore = "a.example"
	groupAfter  = "zz.example"
)

// libraryOnlyKinds are the kinds the library's weight table names and Flux's
// ReconcileOrder does not. The list is literal because the library's tables
// are not exported; a row the library adds is compared by the library's own
// guard.
var libraryOnlyKinds = []string{
	"PersistentVolume",
	"PersistentVolumeClaim",
	"DaemonSet",
	"ReplicaSet",
	"Job",
	"Ingress",
	"NetworkPolicy",
	"HorizontalPodAutoscaler",
	"VerticalPodAutoscaler",
}

// ownGVK is where each kind lives in Kubernetes or its common extensions. A
// kind of Flux's order with no entry here is still compared in the two
// example groups.
var ownGVK = map[string]schema.GroupVersionKind{
	"CustomResourceDefinition":       {Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
	"Namespace":                      {Version: "v1", Kind: "Namespace"},
	"ClusterRole":                    {Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
	"ClusterClass":                   {Group: "cluster.x-k8s.io", Version: "v1beta1", Kind: "ClusterClass"},
	"RuntimeClass":                   {Group: "node.k8s.io", Version: "v1", Kind: "RuntimeClass"},
	"PriorityClass":                  {Group: "scheduling.k8s.io", Version: "v1", Kind: "PriorityClass"},
	"StorageClass":                   {Group: "storage.k8s.io", Version: "v1", Kind: "StorageClass"},
	"VolumeSnapshotClass":            {Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshotClass"},
	"IngressClass":                   {Group: "networking.k8s.io", Version: "v1", Kind: "IngressClass"},
	"GatewayClass":                   {Group: "gateway.networking.k8s.io", Version: "v1", Kind: "GatewayClass"},
	"ClusterRoleBinding":             {Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
	"ResourceQuota":                  {Version: "v1", Kind: "ResourceQuota"},
	"ServiceAccount":                 {Version: "v1", Kind: "ServiceAccount"},
	"Role":                           {Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
	"RoleBinding":                    {Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
	"ConfigMap":                      {Version: "v1", Kind: "ConfigMap"},
	"Secret":                         {Version: "v1", Kind: "Secret"},
	"Service":                        {Version: "v1", Kind: "Service"},
	"LimitRange":                     {Version: "v1", Kind: "LimitRange"},
	"Deployment":                     {Group: "apps", Version: "v1", Kind: "Deployment"},
	"StatefulSet":                    {Group: "apps", Version: "v1", Kind: "StatefulSet"},
	"CronJob":                        {Group: "batch", Version: "v1", Kind: "CronJob"},
	"PodDisruptionBudget":            {Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
	"MutatingWebhookConfiguration":   {Group: "admissionregistration.k8s.io", Version: "v1", Kind: "MutatingWebhookConfiguration"},
	"ValidatingWebhookConfiguration": {Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingWebhookConfiguration"},
	"PersistentVolume":               {Version: "v1", Kind: "PersistentVolume"},
	"PersistentVolumeClaim":          {Version: "v1", Kind: "PersistentVolumeClaim"},
	"DaemonSet":                      {Group: "apps", Version: "v1", Kind: "DaemonSet"},
	"ReplicaSet":                     {Group: "apps", Version: "v1", Kind: "ReplicaSet"},
	"Job":                            {Group: "batch", Version: "v1", Kind: "Job"},
	"Ingress":                        {Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"},
	"NetworkPolicy":                  {Group: "networking.k8s.io", Version: "v1", Kind: "NetworkPolicy"},
	"HorizontalPodAutoscaler":        {Group: "autoscaling", Version: "v2", Kind: "HorizontalPodAutoscaler"},
	"VerticalPodAutoscaler":          {Group: "autoscaling.k8s.io", Version: "v1", Kind: "VerticalPodAutoscaler"},
}

// fluxOrderUniverse returns one object per group, version and kind the guard
// compares, all with the same name and no namespace so that only the kind
// decides the order, and the kinds of Flux's order that have no entry in
// ownGVK. It holds every kind of Flux's ReconcileOrder (read now, from the
// pinned module) and every library-only kind, each in its own group and in
// the two example groups; the v1beta1 CustomResourceDefinition and
// ClusterRole; a custom kind in both example groups; a custom class kind; and
// a kind whose name ends in a lower-case "class", which is no class kind on
// either side.
//
// The core Namespace is here only at v1: Flux treats only v1 as a cluster
// definition, the library every version, and no other version exists.
func fluxOrderUniverse() (objs []*unstructured.Unstructured, withoutOwnGroup []string) {
	kinds := slices.Concat(fluxssa.ReconcileOrder.First, fluxssa.ReconcileOrder.Last, libraryOnlyKinds)
	slices.Sort(kinds)
	kinds = slices.Compact(kinds)

	var gvks []schema.GroupVersionKind
	for _, k := range kinds {
		if gvk, ok := ownGVK[k]; ok {
			gvks = append(gvks, gvk)
		} else {
			withoutOwnGroup = append(withoutOwnGroup, k)
		}
		gvks = append(gvks,
			schema.GroupVersionKind{Group: groupBefore, Version: "v1", Kind: k},
			schema.GroupVersionKind{Group: groupAfter, Version: "v1", Kind: k},
		)
	}
	gvks = append(gvks,
		schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"},
		schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1beta1", Kind: "ClusterRole"},
		schema.GroupVersionKind{Group: groupBefore, Version: "v1", Kind: "Widget"},
		schema.GroupVersionKind{Group: groupAfter, Version: "v1", Kind: "Widget"},
		schema.GroupVersionKind{Group: groupBefore, Version: "v1", Kind: "WidgetClass"},
		schema.GroupVersionKind{Group: groupBefore, Version: "v1", Kind: "Widgetclass"},
	)

	for _, gvk := range gvks {
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(gvk)
		o.SetName("x")
		objs = append(objs, o)
	}
	return objs, withoutOwnGroup
}

// stagedOrder returns objs in the order Flux's staged apply writes them:
// stage after stage, each stage in the order of Flux's sorter. The stage
// rules and the sorter are Flux's own. The precedence of the stages is
// written inline in ApplyAllStaged and is mirrored here; the integration
// suite checks it against a real apply.
func stagedOrder(objs []*unstructured.Unstructured, opts fluxssa.ApplyOptions) []*unstructured.Unstructured {
	var defs, classes, custom, rest []*unstructured.Unstructured
	for _, o := range objs {
		switch {
		case ssautils.IsClusterDefinition(o):
			defs = append(defs, o)
		case ssautils.IsClassDefinition(o):
			classes = append(classes, o)
		case ssautils.IsCustomStage(o, opts.CustomStageKinds):
			custom = append(custom, o)
		default:
			rest = append(rest, o)
		}
	}
	out := make([]*unstructured.Unstructured, 0, len(objs))
	for _, stage := range [][]*unstructured.Unstructured{defs, classes, custom, rest} {
		sort.Sort(fluxssa.SortableUnstructureds(stage))
		out = append(out, stage...)
	}
	return out
}

// orderPair is two kinds that Flux applies first-then-second, with the
// library weight of each.
type orderPair struct {
	first, second             schema.GroupKind
	firstWeight, secondWeight int
}

func (p orderPair) String() string {
	return fmt.Sprintf("%s/%s (%d) before %s/%s (%d)",
		p.first.Group, p.first.Kind, p.firstWeight, p.second.Group, p.second.Kind, p.secondWeight)
}

// comparison is what the two orders say about every pair of an order.
type comparison struct {
	// contradictions are the pairs Flux applies first-then-second while the
	// weight puts the second strictly before the first.
	contradictions []orderPair
	// refined counts the pairs the weight leaves equal and Flux orders.
	refined int
}

// compareOrders compares every pair of order, which is in Flux's apply
// order, by weight. Two objects of the same group and kind (two versions of
// one kind) are not compared: Flux orders them by namespace and name only,
// so neither order separates them by kind.
func compareOrders(order []*unstructured.Unstructured, weight func(schema.GroupVersionKind) int) comparison {
	var c comparison
	seen := map[[2]schema.GroupKind]bool{}
	for i, a := range order {
		for _, b := range order[i+1:] {
			agk, bgk := a.GroupVersionKind().GroupKind(), b.GroupVersionKind().GroupKind()
			if agk == bgk {
				continue
			}
			wa, wb := weight(a.GroupVersionKind()), weight(b.GroupVersionKind())
			switch {
			case wa > wb:
				if key := [2]schema.GroupKind{agk, bgk}; !seen[key] {
					seen[key] = true
					c.contradictions = append(c.contradictions,
						orderPair{first: agk, second: bgk, firstWeight: wa, secondWeight: wb})
				}
			case wa == wb:
				c.refined++
			}
		}
	}
	return c
}

// goModPath is the operator's go.mod, seen from this package's directory,
// which is where go test runs the package's tests.
const goModPath = "../../go.mod"

// moduleVersion returns the version go.mod requires a module at, or the
// version it replaces it with. A test binary carries no dependency versions
// in its build info, so go.mod is the one place that names the pins.
func moduleVersion(path string) string {
	const unknown = "version not found, see go.mod"
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return unknown
	}
	mod, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return unknown
	}
	for _, r := range mod.Replace {
		if r.Old.Path == path {
			return r.New.Version
		}
	}
	for _, r := range mod.Require {
		if r.Mod.Path == path {
			return r.Mod.Version
		}
	}
	return unknown
}

// contradictionMessage is the failure text: the pairs, the two pinned
// modules and what the maintainer does next.
func contradictionMessage(pairs []orderPair) string {
	var b strings.Builder
	b.WriteString("Flux's staged apply order contradicts the library's kind weights (0012:D5:R1).\n")
	fmt.Fprintf(&b, "Flux:    %s %s\n", fluxModule, moduleVersion(fluxModule))
	fmt.Fprintf(&b, "Library: %s %s\n\n", libraryModule, moduleVersion(libraryModule))
	b.WriteString("Flux applies the first kind before the second; the library weighs the first above the second:\n")
	for _, p := range pairs {
		fmt.Fprintf(&b, "  %s\n", p)
	}
	b.WriteString(`
What to do:
  - Do not edit this test or its list of kinds to make it pass.
  - The library's weight table follows Flux, not the other way round. Change
    opm/k8s/object/weights.go in the library, and the Flux list in its
    flux_order_test.go, so that the table agrees with this Flux version. A changed
    weight value is a breaking change of the library.
  - Hold this pin bump until that library release exists, then bump Flux and the
    library here in one PR.
  - If a library bump alone caused this, the library's table left Flux's order:
    fix it in the library and hold the bump.
`)
	return b.String()
}

func hasPair(pairs []orderPair, first, second schema.GroupKind) bool {
	return slices.ContainsFunc(pairs, func(p orderPair) bool {
		return p.first == first && p.second == second
	})
}

// TestFluxOrderNeverContradictsLibraryWeights is the guard: at the pins of
// go.mod, no pair of kinds is applied by Flux in the opposite order to the
// library's weights.
func TestFluxOrderNeverContradictsLibraryWeights(t *testing.T) {
	universe, withoutOwnGroup := fluxOrderUniverse()
	order := stagedOrder(universe, fluxssa.DefaultApplyOptions())
	got := compareOrders(order, libobject.Weight)

	if len(withoutOwnGroup) > 0 {
		t.Logf("kinds of Flux's order compared in the example groups only (no entry in ownGVK): %v", withoutOwnGroup)
	}
	t.Logf("%d objects compared; Flux orders %d pairs that the library weighs equally (a refinement, not a failure)",
		len(order), got.refined)

	if len(got.contradictions) > 0 {
		t.Fatal(contradictionMessage(got.contradictions))
	}
	// The library weighs many kinds equally (every kind Flux does not list,
	// for one), so a count of zero means the tie branch no longer counts.
	if got.refined == 0 {
		t.Fatal("the comparison found no pair that Flux orders and the library weighs equally")
	}
}

// TestFluxOrderRefinesATie pins that a tie is no failure and is counted: the
// library weighs a ConfigMap and a Secret equally, and Flux applies the
// ConfigMap first.
func TestFluxOrderRefinesATie(t *testing.T) {
	mk := func(kind string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(ownGVK[kind])
		o.SetName("x")
		return o
	}
	order := stagedOrder([]*unstructured.Unstructured{mk("Secret"), mk("ConfigMap")}, fluxssa.DefaultApplyOptions())
	if got := []string{order[0].GetKind(), order[1].GetKind()}; !slices.Equal(got, []string{"ConfigMap", "Secret"}) {
		t.Fatalf("want Flux to apply the ConfigMap before the Secret, got %v", got)
	}
	got := compareOrders(order, libobject.Weight)
	if len(got.contradictions) != 0 || got.refined != 1 {
		t.Fatalf("want no contradiction and one refined pair, got %v and %d", got.contradictions, got.refined)
	}
}

// TestFluxOrderUniverseNamesEveryLibraryKind keeps ownGVK in step with the
// literal list of library-only kinds.
func TestFluxOrderUniverseNamesEveryLibraryKind(t *testing.T) {
	for _, k := range libraryOnlyKinds {
		if _, ok := ownGVK[k]; !ok {
			t.Errorf("library-only kind %q has no entry in ownGVK", k)
		}
	}
}

// TestFluxOrderComparisonCanFail proves the guard cannot pass vacuously. No
// sub-test may call t.Parallel: one of them changes a variable of the Flux
// package.
func TestFluxOrderComparisonCanFail(t *testing.T) {
	service := schema.GroupKind{Kind: "Service"}
	deployment := schema.GroupKind{Group: "apps", Kind: "Deployment"}

	t.Run("a weight that leaves Flux's rank order", func(t *testing.T) {
		universe, _ := fluxOrderUniverse()
		weight := func(gvk schema.GroupVersionKind) int {
			if gvk.GroupKind() == deployment {
				return libobject.WeightService - 1
			}
			return libobject.Weight(gvk)
		}
		got := compareOrders(stagedOrder(universe, fluxssa.DefaultApplyOptions()), weight)
		if !hasPair(got.contradictions, service, deployment) {
			t.Fatalf("want the pair Service before Deployment, got %v", got.contradictions)
		}
	})

	t.Run("a weight that leaves Flux's group tie-break", func(t *testing.T) {
		universe, _ := fluxOrderUniverse()
		hpa := schema.GroupKind{Group: "autoscaling", Kind: "HorizontalPodAutoscaler"}
		job := schema.GroupKind{Group: "batch", Kind: "Job"}
		weight := func(gvk schema.GroupVersionKind) int {
			if gvk.GroupKind() == hpa {
				return libobject.WeightDefault + 1
			}
			return libobject.Weight(gvk)
		}
		got := compareOrders(stagedOrder(universe, fluxssa.DefaultApplyOptions()), weight)
		if !hasPair(got.contradictions, hpa, job) {
			t.Fatalf("want the pair HorizontalPodAutoscaler before Job, got %v", got.contradictions)
		}
	})

	t.Run("a Flux order that moves a kind", func(t *testing.T) {
		first := fluxssa.ReconcileOrder.First
		si, di := slices.Index(first, "Service"), slices.Index(first, "Deployment")
		if si < 0 || di < 0 {
			t.Fatalf("Flux's ReconcileOrder.First no longer names Service and Deployment; "+
				"swap two other entries of different library weight here: %v", first)
		}
		// The universe is built before the swap; the swap changes only the order.
		universe, _ := fluxOrderUniverse()
		first[si], first[di] = first[di], first[si]
		t.Cleanup(func() { first[si], first[di] = first[di], first[si] })

		got := compareOrders(stagedOrder(universe, fluxssa.DefaultApplyOptions()), libobject.Weight)
		if !hasPair(got.contradictions, deployment, service) {
			t.Fatalf("want the pair Deployment before Service, got %v", got.contradictions)
		}
	})

	t.Run("a Flux order that names a new kind", func(t *testing.T) {
		original := fluxssa.ReconcileOrder.First
		fluxssa.ReconcileOrder.First = append([]string{"Gadget"}, original...)
		t.Cleanup(func() { fluxssa.ReconcileOrder.First = original })

		// The universe is built after the change: the new kind enters it
		// from Flux's order alone, and it has no group of its own here.
		universe, withoutOwnGroup := fluxOrderUniverse()
		if !slices.Contains(withoutOwnGroup, "Gadget") {
			t.Fatalf("want Gadget reported as a kind without its own group, got %v", withoutOwnGroup)
		}
		gadget := schema.GroupKind{Group: groupBefore, Kind: "Gadget"}
		binding := schema.GroupKind{Group: "rbac.authorization.k8s.io", Kind: "ClusterRoleBinding"}
		got := compareOrders(stagedOrder(universe, fluxssa.DefaultApplyOptions()), libobject.Weight)
		if !hasPair(got.contradictions, gadget, binding) {
			t.Fatalf("want the pair Gadget before ClusterRoleBinding, got %v", got.contradictions)
		}
	})

	t.Run("a custom stage", func(t *testing.T) {
		universe, _ := fluxOrderUniverse()
		job := schema.GroupKind{Group: "batch", Kind: "Job"}
		opts := fluxssa.DefaultApplyOptions()
		opts.CustomStageKinds = map[schema.GroupKind]struct{}{job: {}}
		got := compareOrders(stagedOrder(universe, opts), libobject.Weight)
		if !hasPair(got.contradictions, job, service) {
			t.Fatalf("want the pair Job before Service, got %v", got.contradictions)
		}
	})
}

// TestFluxOrderMessageNamesThePins pins that the failure message can name the
// two versions that disagree.
func TestFluxOrderMessageNamesThePins(t *testing.T) {
	for _, path := range []string{fluxModule, libraryModule} {
		if v := moduleVersion(path); !strings.HasPrefix(v, "v") {
			t.Errorf("want the version go.mod pins %s at, got %q", path, v)
		}
	}
}

// TestFluxOrderContradictionMessage pins what a failure tells the maintainer.
func TestFluxOrderContradictionMessage(t *testing.T) {
	msg := contradictionMessage([]orderPair{{
		first:       schema.GroupKind{Kind: "Service"},
		second:      schema.GroupKind{Group: "apps", Kind: "Deployment"},
		firstWeight: 50, secondWeight: 40,
	}})
	for _, want := range []string{
		"/Service (50) before apps/Deployment (40)",
		fluxModule + " " + moduleVersion(fluxModule),
		libraryModule + " " + moduleVersion(libraryModule),
		"Do not edit this test",
		"Hold this pin bump",
		"opm/k8s/object/weights.go",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message lacks %q:\n%s", want, msg)
		}
	}
}
