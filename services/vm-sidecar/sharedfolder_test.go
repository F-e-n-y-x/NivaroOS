package main

import (
	"strings"
	"testing"
)

func TestSharedFolder_XMLTemplate(t *testing.T) {
	data := struct{ Name string }{Name: "myvm"}

	var buf strings.Builder
	if err := rootSharedFolderTemplate.Execute(&buf, data); err != nil {
		t.Fatalf("template execution failed: %v", err)
	}

	xml := buf.String()
	if !strings.Contains(xml, "<driver type='virtiofs'/>") {
		t.Errorf("expected virtiofs driver, got %s", xml)
	}
	if !strings.Contains(xml, "<source dir='/DATA/VM-Shares/myvm'/>") {
		t.Errorf("expected source dir /DATA/VM-Shares/myvm, got %s", xml)
	}
	if !strings.Contains(xml, "<target dir='nivaroshare'/>") {
		t.Errorf("expected target tag nivaroshare, got %s", xml)
	}
}
