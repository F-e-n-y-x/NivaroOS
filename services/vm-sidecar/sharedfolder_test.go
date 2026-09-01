package main

import (
	"strings"
	"testing"
)

func TestSharedFolder_XMLTemplate(t *testing.T) {
	spec := SharedFolderSpec{
		SourceDir: "/DATA/Media",
		TargetTag: "nivaroshare",
		ReadOnly:  false,
	}

	var buf strings.Builder
	if err := sharedFolderTemplate.Execute(&buf, spec); err != nil {
		t.Fatalf("template execution failed: %v", err)
	}

	xml := buf.String()
	if !strings.Contains(xml, "<driver type='virtiofs'/>") {
		t.Errorf("expected virtiofs driver, got %s", xml)
	}
	if !strings.Contains(xml, "<source dir='/DATA/Media'/>") {
		t.Errorf("expected source dir /DATA/Media, got %s", xml)
	}
	if !strings.Contains(xml, "<target dir='nivaroshare'/>") {
		t.Errorf("expected target tag nivaroshare, got %s", xml)
	}
}
