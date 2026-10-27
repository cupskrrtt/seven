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

// ParseLocation derives the branch location from a relative URL like
// "^/project/branches/feature-x/src". The first trunk/branches/tags segment wins.
func ParseLocation(relURL string) Location {
	p := strings.TrimPrefix(strings.TrimPrefix(relURL, "^"), "/")
	segs := strings.Split(p, "/")
	for i, s := range segs {
		switch s {
		case "trunk":
			return Location{Kind: KindTrunk, Name: "trunk"}
		case "branches", "tags":
			if i+1 < len(segs) && segs[i+1] != "" {
				kind := KindBranch
				if s == "tags" {
					kind = KindTag
				}
				return Location{Kind: kind, Name: segs[i+1]}
			}
		}
	}
	return Location{Kind: KindOther, Name: "/" + p}
}
