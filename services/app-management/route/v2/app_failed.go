package v2

import (
	"context"
	"regexp"
	"strconv"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
	client2 "github.com/moby/moby/client"
)

// Exit codes a container gets from a normal stop: 0 (clean exit), 130
// (SIGINT), 137 (SIGKILL after docker stop's timeout), 143 (SIGTERM).
var stoppedOnPurpose = map[int]bool{0: true, 130: true, 137: true, 143: true}

var exitedRe = regexp.MustCompile(`^Exited \((-?\d+)\)`)

// containerOutcome is what one container tells about its app.
type containerOutcome struct {
	failed   bool
	exitCode *int
}

// outcomeOf reads a container's state and docker's status text
// ("Exited (1) 3 hours ago"). Restarting and dead count as failed; an
// exit counts only with an error code.
func outcomeOf(state, status string) containerOutcome {
	switch state {
	case "restarting", "dead":
		return containerOutcome{failed: true}
	case "exited":
		m := exitedRe.FindStringSubmatch(status)
		if m == nil {
			return containerOutcome{}
		}
		code, err := strconv.Atoi(m[1])
		if err != nil {
			return containerOutcome{}
		}
		return containerOutcome{failed: !stoppedOnPurpose[code], exitCode: &code}
	}
	return containerOutcome{}
}

// markFailed sets Failed/ExitCode on app grid items that aren't running,
// so a client can tell an app that crashed from one someone stopped (the
// phone's Server health used to flag every stopped container). A compose
// app is matched by its project label, any other by container name.
func markFailed(items []codegen.WebAppGridItem, list []containerSummary) {
	byProject := map[string][]containerSummary{}
	byName := map[string]containerSummary{}
	for _, c := range list {
		if p := c.project; p != "" {
			byProject[p] = append(byProject[p], c)
		}
		byName[c.name] = c
	}
	for i := range items {
		it := &items[i]
		if it.Status != nil && *it.Status == "running" || it.Name == nil {
			continue
		}
		cs := byProject[*it.Name]
		if len(cs) == 0 {
			if c, ok := byName[*it.Name]; ok {
				cs = []containerSummary{c}
			}
		}
		failed := false
		var code *int
		for _, c := range cs {
			o := outcomeOf(c.state, c.status)
			failed = failed || o.failed
			if code == nil {
				code = o.exitCode
			}
		}
		if len(cs) > 0 {
			it.Failed = &failed
			it.ExitCode = code
		}
	}
}

type containerSummary struct{ name, project, state, status string }

// listContainers: every container, with what markFailed needs. Errors mean
// no extra fields - the grid is still served.
var listContainers = func(ctx context.Context) []containerSummary {
	cli, err := client2.New(client2.FromEnv)
	if err != nil {
		return nil
	}
	defer cli.Close()
	list, err := cli.ContainerList(ctx, client2.ContainerListOptions{All: true})
	if err != nil {
		return nil
	}
	out := make([]containerSummary, 0, len(list.Items))
	for _, c := range list.Items {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}
		out = append(out, containerSummary{name: name, project: c.Labels["com.docker.compose.project"], state: string(c.State), status: c.Status})
	}
	return out
}
