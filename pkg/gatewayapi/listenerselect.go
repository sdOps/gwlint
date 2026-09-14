package gatewayapi

// ListenersFor returns the listeners a parentRef actually selects: the one it
// names with sectionName, or every listener on the parent when it names none.
//
// The result is empty when the manifest set does not describe the parent at
// all, and when a sectionName resolves to no listener. Those are
// dangling-parent-ref's and listener-not-found's findings; a check that judges
// what happens on a listener has nothing to judge either way, and staying
// quiet is also what keeps it silent on a routes-only manifest set, where the
// Gateway lives in another chart or repo.
func ListenersFor(byOwner map[ObjectRef][]Listener, parent Parent) []Listener {
	listeners := byOwner[ObjectRef{Kind: parent.Kind, Namespace: parent.Namespace, Name: parent.Name}]
	if parent.Section == nil {
		return listeners
	}
	for _, l := range listeners {
		if l.Name == *parent.Section {
			return []Listener{l}
		}
	}
	return nil
}
