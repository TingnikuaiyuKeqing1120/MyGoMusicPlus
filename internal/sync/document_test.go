package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func document(t *testing.T, text string) Document {
	t.Helper()
	var out Document
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Unknown plugin schemas keep independent object edits, list members and large
// numeric identities, and propagate an explicit removal after the first sync.
func TestDocumentThreeWayMerge(t *testing.T) {
	base := document(t, `{"albums":[{"id":9007199254740993,"tracks":["a","b"],"name":"List"}],"nested":{"keep":1,"remove":2},"history":["a"]}`)
	local := document(t, `{"albums":[{"id":9007199254740993,"tracks":["a","local"],"name":"Renamed"}],"nested":{"keep":1},"history":["local","a"]}`)
	remote := document(t, `{"albums":[{"id":9007199254740993,"tracks":["a","b","remote"],"name":"List"}],"nested":{"keep":1,"remove":2,"new":3},"history":["remote","a"]}`)
	merged, conflicts, err := MergeDocument(base, local, remote, false)
	if err != nil || conflicts != 0 {
		t.Fatal(conflicts, err)
	}
	data, _ := json.Marshal(merged)
	for _, term := range []string{"9007199254740993", "Renamed", "local", "remote", "\"new\":3"} {
		if !bytes.Contains(data, []byte(term)) {
			t.Fatal(string(data))
		}
	}
	if bytes.Contains(data, []byte(`"remove"`)) || bytes.Contains(merged["albums"], []byte(`"b"`)) {
		t.Fatal(string(data))
	}
	stable, _, err := MergeDocument(merged, merged, merged, false)
	if err != nil || !reflect.DeepEqual(merged, stable) {
		t.Fatal("merge changed without an edit")
	}
}

type memoryDocument struct {
	data Document
	fail bool
}

func (m *memoryDocument) SnapshotData() (Document, error) { return m.data, nil }
func (m *memoryDocument) ApplyData(before, accepted Document) error {
	if m.fail {
		return errors.New("disk full")
	}
	var err error
	m.data, _, err = MergeDocument(before, m.data, accepted, true)
	return err
}

// A new empty device imports a populated document, including after a cloud
// revision race; failed local writes leave the common ancestor untouched.
func TestSyncDocumentRetryAndRecovery(t *testing.T) {
	store := &conflictStore{}
	root := t.TempDir()
	local := &memoryDocument{data: document(t, `{"one":1}`)}
	store.onPut = func() { store.Put("documents/plugin-new.json", []byte(`{"two":2}`)) }
	if _, err := SyncDocument(store, root, "user/repo", "plugin-new", local); err != nil {
		t.Fatal(err)
	}
	if len(local.data) != 2 {
		t.Fatal(local.data)
	}
	phoneRoot := t.TempDir()
	phone := &memoryDocument{data: Document{}, fail: true}
	if _, err := SyncDocument(store, phoneRoot, "user/repo", "plugin-new", phone); err == nil {
		t.Fatal("failed write succeeded")
	}
	var baselines int
	filepath.WalkDir(filepath.Join(phoneRoot, "sync-history"), func(_ string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			baselines++
		}
		return nil
	})
	if baselines != 0 {
		t.Fatal("failed write advanced baseline")
	}
	phone.fail = false
	if _, err := SyncDocument(store, phoneRoot, "user/repo", "plugin-new", phone); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(phone.data, local.data) {
		t.Fatal(phone.data, local.data)
	}
}

// An unresolved scalar conflict converges on the committed cloud value and
// leaves a local backup containing the listener's competing edit.
func TestDocumentConflictBackup(t *testing.T) {
	store := &fakeStore{}
	leftRoot, rightRoot := t.TempDir(), t.TempDir()
	left := &memoryDocument{data: document(t, `{"name":"original"}`)}
	right := &memoryDocument{data: Document{}}
	run := func(root string, d *memoryDocument) DocumentResult {
		t.Helper()
		r, err := SyncDocument(store, root, "u/r", "fixture", d)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	run(leftRoot, left)
	run(rightRoot, right)
	left.data = document(t, `{"name":"left"}`)
	right.data = document(t, `{"name":"right"}`)
	run(leftRoot, left)
	if r := run(rightRoot, right); r.Conflicts != 1 {
		t.Fatal(r)
	}
	if string(right.data["name"]) != `"left"` {
		t.Fatal(right.data)
	}
	found := false
	err := filepath.WalkDir(filepath.Join(rightRoot, "sync-backups"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			found = found || bytes.Contains(data, []byte(`"right"`))
		}
		return nil
	})
	if err != nil || !found {
		t.Fatal(found, err)
	}
}
