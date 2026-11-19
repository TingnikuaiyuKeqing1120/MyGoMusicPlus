package sync

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
)

// Document holds a plugin's JSON values without interpreting provider fields.
type Document map[string]json.RawMessage

type DocumentSource interface {
	SnapshotData() (Document, error)
	ApplyData(before, accepted Document) error
}

type DocumentResult struct {
	Changed   bool
	Items     int
	Conflicts int
}

// MergeDocument compares both copies with their common ancestor. Objects merge
// by key, unique scalar arrays by value, and object arrays by their stable id.
// Concurrent scalar edits use the preferred side and report a conflict. Inputs
// are decoded separately, so no map or slice owned by a source is mutated.
func MergeDocument(base, local, remote Document, preferLocal bool) (Document, int, error) {
	decode := func(doc Document) (map[string]any, error) {
		out := map[string]any{}
		for key, raw := range doc {
			reader := json.NewDecoder(bytes.NewReader(raw))
			reader.UseNumber()
			var value any
			if err := reader.Decode(&value); err != nil {
				return nil, err
			}
			if err := reader.Decode(new(any)); err != io.EOF {
				return nil, fmt.Errorf("invalid JSON in %s", key)
			}
			out[key] = value
		}
		return out, nil
	}
	b, err := decode(base)
	if err != nil {
		return nil, 0, err
	}
	l, err := decode(local)
	if err != nil {
		return nil, 0, err
	}
	r, err := decode(remote)
	if err != nil {
		return nil, 0, err
	}
	merger := jsonMerge{preferLocal: preferLocal}
	merged := merger.object(b, l, r)
	out := Document{}
	for key, value := range merged {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, 0, err
		}
		out[key] = raw
	}
	return out, merger.conflicts, nil
}

type jsonMerge struct {
	preferLocal bool
	conflicts   int
}
type jsonEntry struct {
	value  any
	exists bool
}

func (m *jsonMerge) value(base, local, remote jsonEntry) jsonEntry {
	if reflect.DeepEqual(local, remote) {
		return local
	}
	if reflect.DeepEqual(local, base) {
		return remote
	}
	if reflect.DeepEqual(remote, base) {
		return local
	}
	if !base.exists && (!local.exists || !remote.exists) {
		if local.exists {
			return local
		}
		return remote
	}
	l, lok := local.value.(map[string]any)
	r, rok := remote.value.(map[string]any)
	if lok && rok {
		b, _ := base.value.(map[string]any)
		return jsonEntry{m.object(b, l, r), true}
	}
	la, lok := local.value.([]any)
	ra, rok := remote.value.([]any)
	if lok && rok {
		ba, _ := base.value.([]any)
		if result, ok := m.array(ba, la, ra); ok {
			return jsonEntry{result, true}
		}
	}
	// A deletion racing with an edit keeps the edited value.
	m.conflicts++
	if !local.exists {
		return remote
	}
	if !remote.exists || m.preferLocal {
		return local
	}
	return remote
}

func (m *jsonMerge) object(base, local, remote map[string]any) map[string]any {
	keys := map[string]bool{}
	for _, object := range []map[string]any{base, local, remote} {
		for key := range object {
			keys[key] = true
		}
	}
	out := map[string]any{}
	for key := range keys {
		b, bok := base[key]
		l, lok := local[key]
		r, rok := remote[key]
		entry := m.value(jsonEntry{b, bok}, jsonEntry{l, lok}, jsonEntry{r, rok})
		if entry.exists {
			out[key] = entry.value
		}
	}
	return out
}

func arrayEntries(items []any) (map[string]any, []string, bool) {
	values := map[string]any{}
	var order []string
	for _, value := range items {
		identity := value
		if obj, ok := value.(map[string]any); ok {
			id, exists := obj["id"]
			if !exists {
				return nil, nil, false
			}
			switch id.(type) {
			case string, json.Number:
			default:
				return nil, nil, false
			}
			identity = id
		} else {
			switch value.(type) {
			case nil, string, json.Number, bool:
			default:
				return nil, nil, false
			}
		}
		keyBytes, _ := json.Marshal(identity)
		key := string(keyBytes)
		if _, exists := values[key]; exists {
			return nil, nil, false
		}
		values[key] = value
		order = append(order, key)
	}
	return values, order, true
}

func (m *jsonMerge) array(base, local, remote []any) ([]any, bool) {
	b, bi, bok := arrayEntries(base)
	l, li, lok := arrayEntries(local)
	r, ri, rok := arrayEntries(remote)
	if !bok || !lok || !rok {
		return nil, false
	}
	values := m.object(b, l, r)
	first, second := ri, li
	if slices.Equal(ri, bi) || m.preferLocal {
		first, second = li, ri
	}
	out := make([]any, 0, len(values))
	for _, order := range [][]string{first, second} {
		for _, id := range order {
			if value, ok := values[id]; ok {
				out = append(out, value)
				delete(values, id)
			}
		}
	}
	return out, true
}

// SyncDocument uses a revision check to retry concurrent cloud writes. Backups
// hold both inputs before applying changes, and only a successful apply advances
// the common ancestor. New or completely cleared profiles import existing data.
func SyncDocument(store CollectionStore, root, repoID, name string, source DocumentSource) (DocumentResult, error) {
	var result DocumentResult
	if !filepath.IsLocal(name) || filepath.Base(name) != name {
		return result, fmt.Errorf("invalid sync document name")
	}
	path := "documents/" + name + ".json"
	repo := fmt.Sprintf("%x", sha256.Sum256([]byte(repoID)))
	checkpoint := filepath.Join(root, "sync-history", repo, "documents", name+".json")
	saved, err := os.ReadFile(checkpoint)
	if err != nil && !os.IsNotExist(err) {
		return result, err
	}
	base := Document{}
	if len(saved) > 0 {
		if err = json.Unmarshal(saved, &base); err != nil {
			return result, err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		raw, revision, err := store.ReadVersion(path)
		if err != nil {
			return result, err
		}
		remote := Document{}
		if raw != nil {
			if err = json.Unmarshal(raw, &remote); err != nil {
				return result, err
			}
		}
		local, err := source.SnapshotData()
		if err != nil {
			return result, err
		}
		if filter, ok := source.(interface {
			FilterData(Document) (Document, error)
		}); ok {
			base, err = filter.FilterData(base)
			if err != nil {
				return result, err
			}
			remote, err = filter.FilterData(remote)
			if err != nil {
				return result, err
			}
		}
		ancestor := base
		if len(local) == 0 || raw == nil {
			ancestor = Document{}
		}
		merged, conflicts, err := MergeDocument(ancestor, local, remote, false)
		if err != nil {
			return result, err
		}
		data, err := json.Marshal(merged)
		if err != nil {
			return result, err
		}
		before, _ := json.Marshal(local)
		result = DocumentResult{Changed: !bytes.Equal(before, data), Items: len(merged), Conflicts: conflicts}
		if result.Changed || conflicts > 0 {
			backup, _ := json.Marshal(struct{ Before, Remote, Accepted Document }{local, remote, merged})
			backupPath := filepath.Join(root, "sync-backups", repo, name, fmt.Sprintf("%x.json", sha256.Sum256(backup)))
			if err = writeAtomic(backupPath, backup); err != nil {
				return result, err
			}
		}
		if !bytes.Equal(raw, data) && (raw != nil || len(merged) > 0) {
			err = store.PutVersion(path, data, revision)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return result, err
			}
		}
		if result.Changed {
			if err = source.ApplyData(local, merged); err != nil {
				return result, err
			}
		}
		if !bytes.Equal(saved, data) {
			if err = writeAtomic(checkpoint, data); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	return result, ErrConflict
}
