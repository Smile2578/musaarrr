package main

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

// TorrentFile is one file a .torrent lists: its path inside the torrent and its size.
type TorrentFile struct {
	Path []string
	Size int64
}

var errTorrent = errors.New("unreadable .torrent file")

// torrentFiles lists the files of a .torrent (v1 « files », single file, or v2 « file tree »).
func torrentFiles(b []byte) ([]TorrentFile, error) {
	d := &bdecoder{b: b}
	v, err := d.value(0)
	if err != nil || d.i != len(b) {
		return nil, errTorrent
	}
	top, _ := v.(map[string]any)
	info, _ := top["info"].(map[string]any)
	if info == nil {
		return nil, errTorrent
	}
	name, _ := info["name"].(string)
	out := []TorrentFile{} // read, even when empty: never nil
	if files, ok := info["files"].([]any); ok {
		for _, f := range files {
			fm, _ := f.(map[string]any)
			if attr, _ := fm["attr"].(string); strings.ContainsRune(attr, 'p') {
				continue // a BEP 47 padding file, not the release's
			}
			size, ok := fm["length"].(int64)
			parts, _ := fm["path"].([]any)
			if !ok || len(parts) == 0 {
				return nil, errTorrent
			}
			var path []string
			for _, p := range parts {
				s, ok := p.(string)
				if !ok {
					return nil, errTorrent
				}
				path = append(path, s)
			}
			out = append(out, TorrentFile{path, size})
		}
		return out, nil
	}
	if tree, ok := info["file tree"].(map[string]any); ok {
		if err := walkTree(tree, nil, &out); err != nil {
			return nil, err
		}
		slices.SortFunc(out, func(a, b TorrentFile) int { return slices.Compare(a.Path, b.Path) }) // a dict has no order
		return out, nil
	}
	if size, ok := info["length"].(int64); ok && name != "" {
		return []TorrentFile{{[]string{name}, size}}, nil
	}
	return nil, errTorrent
}

// walkTree reads a v2 file tree: a dict of names whose leaves hold the file under the key "".
func walkTree(node map[string]any, path []string, out *[]TorrentFile) error {
	for name, child := range node {
		cm, ok := child.(map[string]any)
		if !ok {
			return errTorrent
		}
		if leaf, ok := cm[""].(map[string]any); ok {
			size, ok := leaf["length"].(int64)
			if !ok {
				return errTorrent
			}
			*out = append(*out, TorrentFile{append(append([]string{}, path...), name), size})
			continue
		}
		if err := walkTree(cm, append(path, name), out); err != nil {
			return err
		}
	}
	return nil
}

// bdecoder reads bencode: integers, strings, lists and dicts, 64 levels deep at most.
type bdecoder struct {
	b []byte
	i int
}

func (d *bdecoder) value(depth int) (any, error) {
	if depth > 64 || d.i >= len(d.b) {
		return nil, errTorrent
	}
	switch c := d.b[d.i]; {
	case c == 'i':
		end := d.find('e')
		if end < 0 {
			return nil, errTorrent
		}
		n, err := strconv.ParseInt(string(d.b[d.i+1:end]), 10, 64)
		if err != nil {
			return nil, errTorrent
		}
		d.i = end + 1
		return n, nil
	case c == 'l':
		d.i++
		var list []any
		for d.i < len(d.b) && d.b[d.i] != 'e' {
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		if d.i >= len(d.b) {
			return nil, errTorrent
		}
		d.i++
		return list, nil
	case c == 'd':
		d.i++
		m := map[string]any{}
		for d.i < len(d.b) && d.b[d.i] != 'e' {
			k, err := d.str()
			if err != nil {
				return nil, err
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		if d.i >= len(d.b) {
			return nil, errTorrent
		}
		d.i++
		return m, nil
	case c >= '0' && c <= '9':
		return d.str()
	}
	return nil, errTorrent
}

func (d *bdecoder) str() (string, error) {
	colon := d.find(':')
	if colon < 0 {
		return "", errTorrent
	}
	n, err := strconv.Atoi(string(d.b[d.i:colon]))
	if err != nil || n < 0 || colon+1+n > len(d.b) {
		return "", errTorrent
	}
	s := string(d.b[colon+1 : colon+1+n])
	d.i = colon + 1 + n
	return s, nil
}

func (d *bdecoder) find(c byte) int {
	for j := d.i; j < len(d.b) && j < d.i+32; j++ { // a length or an integer never needs 32 digits
		if d.b[j] == c {
			return j
		}
	}
	return -1
}
