package service

import "testing"

func TestShellsPresentAndChoose(t *testing.T) {
	fs := map[string]bool{"/bin/sh": true, "/usr/bin/bash": true, "/bin/ash": true, "/usr/bin/zsh": true}
	shells := shellsPresent(func(p string) bool { return fs[p] })
	want := []ContainerShell{{"bash", "/usr/bin/bash"}, {"zsh", "/usr/bin/zsh"}, {"ash", "/bin/ash"}, {"sh", "/bin/sh"}}
	if len(shells) != len(want) {
		t.Fatalf("got %v", shells)
	}
	for i := range want {
		if shells[i] != want[i] {
			t.Fatalf("got %v, want %v", shells, want)
		}
	}
	for name, path := range map[string]string{"": "/usr/bin/bash", "zsh": "/usr/bin/zsh", "sh": "/bin/sh"} {
		if got, err := chooseShell(shells, name); err != nil || got != path {
			t.Errorf("chooseShell(%q) = %q, %v", name, got, err)
		}
	}
	// Only known shells, and only ones the image has - never a path from the request.
	for _, bad := range []string{"fish", "/bin/sh", "../../bin/sh", "python"} {
		if _, err := chooseShell(shells, bad); err != ErrShellNotAvailable {
			t.Errorf("chooseShell(%q) should be refused, got %v", bad, err)
		}
	}
	// A distroless image with no known shell still tries /bin/sh by default.
	if got, _ := chooseShell(nil, ""); got != "/bin/sh" {
		t.Errorf("default without shells = %q", got)
	}
}
