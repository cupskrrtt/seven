package svn

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
)

// Item is the working copy state of a path (svn's wc-status item attribute).
type Item string

const (
	ItemModified    Item = "modified"
	ItemAdded       Item = "added"
	ItemDeleted     Item = "deleted"
	ItemReplaced    Item = "replaced"
	ItemConflicted  Item = "conflicted"
	ItemUnversioned Item = "unversioned"
	ItemMissing     Item = "missing"
	ItemIgnored     Item = "ignored"
	ItemObstructed  Item = "obstructed"
	ItemExternal    Item = "external"
	ItemIncomplete  Item = "incomplete"
	ItemNormal      Item = "normal"
	ItemNone        Item = "none"
)

// Code returns the single-letter code used by `svn status`.
func (i Item) Code() string {
	switch i {
	case ItemModified:
		return "M"
	case ItemAdded:
		return "A"
	case ItemDeleted:
		return "D"
	case ItemReplaced:
		return "R"
	case ItemConflicted:
		return "C"
	case ItemUnversioned:
		return "?"
	case ItemMissing:
		return "!"
	case ItemIgnored:
		return "I"
	case ItemObstructed:
		return "~"
	case ItemExternal:
		return "X"
	case ItemIncomplete:
		return "!"
	}
	return " "
}

// FileStatus is one entry from `svn status`.
type FileStatus struct {
	Path         string
	Item         Item
	Props        Item
	TreeConflict bool
	Copied       bool
	Locked       bool // this working copy holds a lock token
	Changelist   string
	Revision     int

	// Only set by StatusRemote:
	RemoteChanged   bool // a newer version exists in the repository
	RemoteItem      Item // what happened there: modified, added, deleted…
	RemoteLockOwner string
}

// Conflicted reports whether the path has any kind of conflict.
func (f FileStatus) Conflicted() bool {
	return f.Item == ItemConflicted || f.Props == ItemConflicted || f.TreeConflict
}

// LocalChange reports whether the working copy itself changed the path
// (as opposed to an entry listed only for an incoming change or a lock).
func (f FileStatus) LocalChange() bool {
	switch f.Item {
	case ItemNormal, ItemNone, "":
		return f.Props == ItemModified || f.Props == ItemConflicted || f.TreeConflict
	}
	return true
}

// Versioned reports whether the path is under version control.
func (f FileStatus) Versioned() bool {
	return f.Item != ItemUnversioned && f.Item != ItemIgnored
}

type xmlEntry struct {
	Path     string `xml:"path,attr"`
	WCStatus struct {
		Item         string    `xml:"item,attr"`
		Props        string    `xml:"props,attr"`
		Revision     int       `xml:"revision,attr"`
		TreeConflict bool      `xml:"tree-conflicted,attr"`
		Copied       bool      `xml:"copied,attr"`
		Lock         *struct{} `xml:"lock"`
	} `xml:"wc-status"`
	ReposStatus *struct {
		Item string `xml:"item,attr"`
		Lock *struct {
			Owner string `xml:"owner"`
		} `xml:"lock"`
	} `xml:"repos-status"`
}

type xmlStatus struct {
	Targets []struct {
		Entries []xmlEntry `xml:"entry"`
		Against *struct {
			Revision int `xml:"revision,attr"`
		} `xml:"against"`
	} `xml:"target"`
	Changelists []struct {
		Name    string     `xml:"name,attr"`
		Entries []xmlEntry `xml:"entry"`
	} `xml:"changelist"`
}

func (e xmlEntry) toStatus(changelist string) FileStatus {
	fs := FileStatus{
		Path:         e.Path,
		Item:         Item(e.WCStatus.Item),
		Props:        Item(e.WCStatus.Props),
		Revision:     e.WCStatus.Revision,
		TreeConflict: e.WCStatus.TreeConflict,
		Copied:       e.WCStatus.Copied,
		Locked:       e.WCStatus.Lock != nil,
		Changelist:   changelist,
	}
	if r := e.ReposStatus; r != nil {
		if r.Item != "none" && r.Item != "" {
			fs.RemoteChanged, fs.RemoteItem = true, Item(r.Item)
		}
		if r.Lock != nil {
			fs.RemoteLockOwner = r.Lock.Owner
		}
	}
	return fs
}

// ParseStatus parses `svn status --xml` output. Entries with no local
// change (normal text and props, no conflicts) are dropped.
func ParseStatus(data []byte) ([]FileStatus, error) {
	var x xmlStatus
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parse svn status: %w", err)
	}
	var files []FileStatus
	add := func(e xmlEntry, cl string) {
		fs := e.toStatus(cl)
		if fs.Item == ItemNormal && (fs.Props == ItemNormal || fs.Props == ItemNone) &&
			!fs.TreeConflict && !fs.RemoteChanged && !fs.Locked && fs.RemoteLockOwner == "" && cl == "" {
			return
		}
		files = append(files, fs)
	}
	for _, t := range x.Targets {
		for _, e := range t.Entries {
			add(e, "")
		}
	}
	for _, cl := range x.Changelists {
		for _, e := range cl.Entries {
			add(e, cl.Name)
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		if gi, gj := groupRank(files[i]), groupRank(files[j]); gi != gj {
			return gi < gj
		}
		return files[i].Path < files[j].Path
	})
	return hideConflictArtifacts(files), nil
}

// groupRank orders conflicts first, then versioned changes, then the rest.
func groupRank(f FileStatus) int {
	switch {
	case f.Conflicted():
		return 0
	case f.Item == ItemUnversioned:
		return 2
	case f.Item == ItemExternal || f.Item == ItemIgnored:
		return 3
	}
	return 1
}

// Status returns local modifications in the working copy.
func (c *Client) Status(ctx context.Context) ([]FileStatus, error) {
	out, err := c.Run(ctx, "status", "--xml")
	if err != nil {
		return nil, err
	}
	return ParseStatus(out)
}

// StatusRemote is Status plus what changed in the repository since the
// working copy was updated (svn status -u). It contacts the server and
// returns the HEAD revision compared against.
func (c *Client) StatusRemote(ctx context.Context) ([]FileStatus, int, error) {
	out, err := c.Run(ctx, "status", "-u", "--xml")
	if err != nil {
		return nil, 0, err
	}
	files, err := ParseStatus(out)
	if err != nil {
		return nil, 0, err
	}
	var x xmlStatus
	against := 0
	if xml.Unmarshal(out, &x) == nil {
		for _, t := range x.Targets {
			if t.Against != nil {
				against = t.Against.Revision
			}
		}
	}
	return files, against, nil
}
