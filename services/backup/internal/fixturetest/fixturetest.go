// Package fixturetest checks JSON contract fixtures against the Go types
// they describe: a fixture must decode strictly (no unknown fields) and
// re-encode to exactly the same JSON, so a fixture can neither drift from
// its type nor leave a field out.
package fixturetest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// CheckFile decodes the JSON file at path into target (a pointer) and
// compares its re-encoding with the file.
func CheckFile(t *testing.T, path string, target interface{}) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := CheckJSON(raw, target); err != nil {
		t.Errorf("%s: %v", path, err)
	}
}

// CheckJSON is CheckFile for bytes already read.
func CheckJSON(raw []byte, target interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("strict decode into %T: %w", target, err)
	}
	again, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("re-encode %T: %w", target, err)
	}
	var want, got interface{}
	if err := json.Unmarshal(raw, &want); err != nil {
		return err
	}
	if err := json.Unmarshal(again, &got); err != nil {
		return err
	}
	if !reflect.DeepEqual(want, got) {
		pretty, _ := json.MarshalIndent(got, "", "  ")
		return fmt.Errorf("fixture differs from its %T re-encoding (a field is missing, extra or has a different zero value); the type encodes it as:\n%s", target, pretty)
	}
	return nil
}
