package reconcile

// renderKey names one object for the render pool, so the pool can refuse a
// second render of an object whose timed-out render is still running.
func renderKey(kind, namespace, name string) string {
	return kind + "/" + namespace + "/" + name
}
