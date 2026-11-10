package riskregister

import (
	"reflect"
	"strings"
	"testing"
)

// The editor's form is built from its own list of fields, so a column added to
// the request has to be added there too or it can never be edited.
func TestEditorFormCoversEveryRequestField(t *testing.T) {
	page := riskRegisterEditorHTML()
	typ := reflect.TypeOf(upsertRiskRequest{})
	for i := range typ.NumField() {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if !strings.Contains(page, `{k:"`+tag+`"`) {
			t.Errorf("editor form has no field for %q", tag)
		}
	}
	if strings.Contains(page, `<table`) {
		t.Error("editor page still renders the register as a table of inputs")
	}
}
