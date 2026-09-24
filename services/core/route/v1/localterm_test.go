package v1

import (
	"strings"
	"testing"
)

const fixturePasswd = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
# comment
+ldapuser::::::
svc:x:1500:1500::/home/svc:/usr/sbin/nologin
bob:x:1001:1001:Bob,,,:/home/bob:/bin/bash
alice:x:1000:1000:Alice:/home/alice:/bin/zsh
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
broken:line
`

func TestPickTerminalUser(t *testing.T) {
	entries := parsePasswd(strings.NewReader(fixturePasswd))

	e, reason := pickTerminalUser(entries, "")
	if e.Name != "alice" || !strings.Contains(reason, "lowest") {
		t.Fatalf("got %s (%s), want alice", e.Name, reason)
	}

	e, reason = pickTerminalUser(entries, "bob")
	if e.Name != "bob" || !strings.Contains(reason, "configured") {
		t.Fatalf("got %s (%s), want configured bob", e.Name, reason)
	}

	// configured but not a local account -> automatic choice
	if e, _ = pickTerminalUser(entries, "ldap-only"); e.Name != "alice" {
		t.Fatalf("got %s, want alice", e.Name)
	}

	onlySystem := parsePasswd(strings.NewReader("root:x:0:0:root:/root:/bin/bash\nsvc:x:1500:1500::/home/svc:/bin/false\n"))
	if e, reason = pickTerminalUser(onlySystem, ""); e.Name != "root" || !strings.Contains(reason, "root") {
		t.Fatalf("got %s (%s), want root", e.Name, reason)
	}
	if e, _ = pickTerminalUser(nil, ""); e.Name != "root" || e.UID != 0 {
		t.Fatalf("got %+v, want synthetic root", e)
	}
}
