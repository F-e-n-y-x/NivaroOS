package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// A project like the owner's: a GitHub checkout built by a Portainer stack
// whose compose file lives under Portainer's /data.
func gitProject(t *testing.T) (labels map[string]string, toHost hostPathMapper, upstream, checkout string) {
	t.Helper()
	root := t.TempDir()
	upstream = filepath.Join(root, "upstream.git")
	checkout = filepath.Join(root, "app")
	run(t, root, "git", "init", "-q", "--bare", "-b", "main", upstream)
	run(t, root, "git", "clone", "-q", upstream, checkout)
	os.WriteFile(filepath.Join(checkout, "Dockerfile"), []byte("FROM busybox\n"), 0o644)
	run(t, checkout, "git", "add", ".")
	run(t, checkout, "git", "commit", "-qm", "init")
	run(t, checkout, "git", "push", "-q", "origin", "HEAD:main")
	run(t, checkout, "git", "branch", "-q", "--set-upstream-to=origin/main")

	portainerData := filepath.Join(root, "portainer")
	stack := filepath.Join(portainerData, "compose", "84")
	os.MkdirAll(stack, 0o755)
	os.WriteFile(filepath.Join(stack, "docker-compose.yml"), []byte("services:\n  web:\n    build:\n      context: "+checkout+"\n      dockerfile: Dockerfile\n  db:\n    image: postgres:16\n"), 0o644)
	os.WriteFile(filepath.Join(stack, "stack.env"), []byte("A=1\n"), 0o644)
	labels = map[string]string{
		"com.docker.compose.project":              "demo",
		"com.docker.compose.service":              "web",
		"com.docker.compose.project.config_files": "/data/compose/84/docker-compose.yml",
	}
	toHost = func(p string) string {
		if strings.HasPrefix(p, "/data/") {
			return filepath.Join(portainerData, strings.TrimPrefix(p, "/data/"))
		}
		return p
	}
	return
}

func TestAComposeBuiltGitCheckoutIsAGitSource(t *testing.T) {
	labels, toHost, _, checkout := gitProject(t)
	src := detectSource(labels, toHost)
	if src.Kind != "git" || src.Repo != checkout || src.Branch != "main" || src.Context != checkout {
		t.Fatalf("got %+v", src)
	}
	if !strings.HasSuffix(src.EnvFile, "stack.env") {
		t.Fatalf("stack env not found: %+v", src)
	}
	// The other service in the same stack uses a registry image.
	labels["com.docker.compose.service"] = "db"
	if s := detectSource(labels, toHost); s.Kind != "registry" {
		t.Fatalf("image service detected as %s", s.Kind)
	}
}

func TestAPlainFolderBuildIsLocal(t *testing.T) {
	labels, toHost, _, checkout := gitProject(t)
	os.RemoveAll(filepath.Join(checkout, ".git"))
	if s := detectSource(labels, toHost); s.Kind != "local" {
		t.Fatalf("got %+v", s)
	}
}

// "Update available" for a GitHub-built container = upstream has commits
// the checkout doesn't.
func TestBehindCountsNewUpstreamCommits(t *testing.T) {
	labels, toHost, upstream, _ := gitProject(t)
	src := detectSource(labels, toHost)
	if n, err := gitBehind(context.Background(), src); err != nil || n != 0 {
		t.Fatalf("fresh checkout: behind=%d err=%v", n, err)
	}
	other := filepath.Join(t.TempDir(), "other")
	run(t, "/", "git", "clone", "-q", upstream, other)
	os.WriteFile(filepath.Join(other, "new.txt"), []byte("x"), 0o644)
	run(t, other, "git", "add", ".")
	run(t, other, "git", "commit", "-qm", "feature")
	run(t, other, "git", "push", "-q", "origin", "HEAD:main")
	if n, err := gitBehind(context.Background(), src); err != nil || n != 1 {
		t.Fatalf("after a push upstream: behind=%d err=%v", n, err)
	}
}

func TestAStoppedContainerIsRebuiltButNotStarted(t *testing.T) {
	src := ContainerSource{Kind: "git", Project: "p", Service: "web", ComposeFile: "/c.yml", Repo: "/r", Branch: "main"}
	steps := rebuildSteps(src, false)
	last := strings.Join(steps[len(steps)-1], " ")
	if !strings.Contains(last, "--no-start") {
		t.Fatalf("stopped container would be started: %s", last)
	}
	if !strings.Contains(strings.Join(steps[0], " "), "pull --ff-only") {
		t.Fatalf("git source must fast-forward only: %v", steps[0])
	}
	if s := strings.Join(rebuildSteps(src, true)[2], " "); !strings.Contains(s, "up -d --no-deps web") {
		t.Fatalf("running container: %s", s)
	}
	if steps := rebuildSteps(ContainerSource{Kind: "local", Project: "p", Service: "web", ComposeFile: "/c.yml"}, true); len(steps) != 2 {
		t.Fatalf("local source should not git pull: %v", steps)
	}
}

func TestCredentialsAreNeverShown(t *testing.T) {
	if got := stripCredentials("https://user:ghp_secret@github.com/a/b.git"); strings.Contains(got, "secret") || strings.Contains(got, "user") {
		t.Fatalf("got %s", got)
	}
}
