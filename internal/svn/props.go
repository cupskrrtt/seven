package svn

import (
	"context"
	"encoding/xml"
	"fmt"
)

// Property is a versioned property such as svn:ignore or svn:externals.
type Property struct {
	Name  string
	Value string
}

type xmlProps struct {
	Targets []struct {
		Props []struct {
			Name  string `xml:"name,attr"`
			Value string `xml:",chardata"`
		} `xml:"property"`
	} `xml:"target"`
}

// ParseProps parses `svn proplist -v --xml` output.
func ParseProps(data []byte) ([]Property, error) {
	var x xmlProps
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parse svn proplist: %w", err)
	}
	var props []Property
	for _, t := range x.Targets {
		for _, p := range t.Props {
			props = append(props, Property{Name: p.Name, Value: p.Value})
		}
	}
	return props, nil
}

// Props lists the properties of a working-copy path or URL.
func (c *Client) Props(ctx context.Context, target string) ([]Property, error) {
	out, err := c.Run(ctx, "proplist", "-v", "--xml", "--", pathArg(target))
	if err != nil {
		return nil, err
	}
	return ParseProps(out)
}

// SetProp sets name to value on a working-copy path (committed later).
func (c *Client) SetProp(ctx context.Context, name, value, path string) error {
	_, err := withMessageFile(value, func(valueFile string) ([]byte, error) {
		return c.Run(ctx, "propset", name, "-F", valueFile, "--", pathArg(path))
	})
	return err
}

// DeleteProp removes name from a working-copy path.
func (c *Client) DeleteProp(ctx context.Context, name, path string) error {
	_, err := c.Run(ctx, "propdel", name, "--", pathArg(path))
	return err
}
