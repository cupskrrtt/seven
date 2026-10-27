package svn

import "strings"

// BranchKind classifies where a working copy points in the standard layout.
type BranchKind int

const (
	KindOther BranchKind = iota
	KindTrunk
	KindBranch
	KindTag
)

// Location is the working copy's position in a trunk/branches/tags layout.
type Location struct {
	Kind BranchKind
	Name string // branch or tag name; "trunk" for trunk; path otherwise
	Root string // project root holding trunk/branches/tags, e.g. "^/proj" or "^"
	Sub  string // path below the branch root, e.g. "/src"; "" at the branch root
}

func (l Location) String() string {
	switch l.Kind {
	case KindTrunk:
		return "trunk"
	case KindBranch:
		return "branch: " + l.Name
	case KindTag:
		return "tag: " + l.Name
	}
	return l.Name
}

// HasLayout reports whether the standard trunk/branches/tags layout was found.
func (l Location) HasLayout() bool { return l.Kind != KindOther }

// BranchURL returns the repository-relative URL ("^/...") of a branch in
// the same project.
func (l Location) BranchURL(kind BranchKind, name string) string {
	switch kind {
	case KindTrunk:
		return l.Root + "/trunk"
	case KindBranch:
		return l.Root + "/branches/" + name
	case KindTag:
		return l.Root + "/tags/" + name
	}
	return ""
}

// ParseLocation derives the branch location from a relative URL like
// "^/project/branches/feature-x/src". The first trunk/branches/tags segment wins.
func ParseLocation(relURL string) Location {
	p := strings.TrimPrefix(strings.TrimPrefix(relURL, "^"), "/")
	segs := strings.Split(p, "/")
	root := func(i int) string {
		if i == 0 {
			return "^"
		}
		return "^/" + strings.Join(segs[:i], "/")
	}
	sub := func(i int) string {
		if i >= len(segs) {
			return ""
		}
		return "/" + strings.Join(segs[i:], "/")
	}
	for i, s := range segs {
		switch s {
		case "trunk":
			return Location{Kind: KindTrunk, Name: "trunk", Root: root(i), Sub: sub(i + 1)}
		case "branches", "tags":
			if i+1 < len(segs) && segs[i+1] != "" {
				kind := KindBranch
				if s == "tags" {
					kind = KindTag
				}
				return Location{Kind: kind, Name: segs[i+1], Root: root(i), Sub: sub(i + 2)}
			}
		}
	}
	return Location{Kind: KindOther, Name: "/" + p}
}
