package reconcile

// recordedIdentities returns the non-empty identities among ids, in order: the
// list a prune or a deletion cleanup judges with. It is empty when nothing is
// recorded, and the prune then judges with no identity.
func recordedIdentities(ids ...string) []string {
	var out []string
	for _, id := range ids {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}
