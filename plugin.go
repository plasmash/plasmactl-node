// Package plasmactlnode implements a launchr plugin with node provisioning functionality
package plasmactlnode

import (
	"context"
	"embed"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/launchrctl/launchr/pkg/action"

	"github.com/plasmash/plasmactl-node/actions/add"
	"github.com/plasmash/plasmactl-node/actions/allocate"
	"github.com/plasmash/plasmactl-node/actions/deallocate"
	"github.com/plasmash/plasmactl-node/actions/destroy"
	"github.com/plasmash/plasmactl-node/actions/join"
	"github.com/plasmash/plasmactl-node/actions/list"
	"github.com/plasmash/plasmactl-node/actions/provision"
	"github.com/plasmash/plasmactl-node/actions/query"
	"github.com/plasmash/plasmactl-node/actions/show"
	"github.com/plasmash/plasmactl-node/actions/validate"
)

//go:embed actions/*/*.yaml
var actionYamlFS embed.FS

func init() {
	launchr.RegisterPlugin(&Plugin{})
}

// Plugin is [launchr.Plugin] plugin providing node provisioning functionality.
type Plugin struct {
	k   keyring.Keyring
	cfg launchr.Config
}

// PluginInfo implements [launchr.Plugin] interface.
func (p *Plugin) PluginInfo() launchr.PluginInfo {
	return launchr.PluginInfo{
		Weight: 10,
	}
}

// OnAppInit implements [launchr.Plugin] interface.
func (p *Plugin) OnAppInit(app launchr.App) error {
	app.Services().Get(&p.cfg)
	app.Services().Get(&p.k)
	return nil
}

// DiscoverActions implements [launchr.ActionDiscoveryPlugin] interface.
func (p *Plugin) DiscoverActions(_ context.Context) ([]*action.Action, error) {
	// node:add - Add a node to a platform (manual registration)
	addYaml, _ := actionYamlFS.ReadFile("actions/add/add.yaml")
	addAct := action.NewFromYAML("node:add", addYaml)
	addAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		add := &add.Add{
			Platform:  input.Arg("platform").(string),
			Hostname:  input.Opt("hostname").(string),
			PublicIP:  input.Opt("public-ip").(string),
			PrivateIP: input.Opt("private-ip").(string),
			Zones:     action.InputOptSlice[string](input, "zone"),
		}
		add.SetLogger(log)
		add.SetTerm(term)
		err := add.Execute()
		return add.Result(), err
	}))

	// node:provision - Provision infrastructure
	provisionYaml, _ := actionYamlFS.ReadFile("actions/provision/provision.yaml")
	provisionAct := action.NewFromYAML("node:provision", provisionYaml)
	provisionAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		provision := &provision.Provision{
			Keyring:     p.k,
			Name:        input.Arg("name").(string),
			DryRun:      input.Opt("dry-run").(bool),
			AutoApprove: input.Opt("auto-approve").(bool),
		}
		provision.SetLogger(log)
		provision.SetTerm(term)
		err := provision.Execute()
		return provision.Result(), err
	}))

	// node:list - List platforms and their nodes
	listYaml, _ := actionYamlFS.ReadFile("actions/list/list.yaml")
	listAct := action.NewFromYAML("node:list", listYaml)
	listAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		l := &list.List{
			Tree: input.Opt("tree").(bool),
		}
		l.SetLogger(log)
		l.SetTerm(term)
		err := l.Execute()
		return l.Result(), err
	}))

	// node:show - Show platform/node details
	showYaml, _ := actionYamlFS.ReadFile("actions/show/show.yaml")
	showAct := action.NewFromYAML("node:show", showYaml)
	showAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		s := &show.Show{
			Name: input.Arg("name").(string),
		}
		s.SetLogger(log)
		s.SetTerm(term)
		err := s.Execute()
		return s.Result(), err
	}))

	// node:destroy - Destroy infrastructure
	destroyYaml, _ := actionYamlFS.ReadFile("actions/destroy/destroy.yaml")
	destroyAct := action.NewFromYAML("node:destroy", destroyYaml)
	destroyAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		destroy := &destroy.Destroy{
			Keyring:   p.k,
			Name:      input.Arg("name").(string),
			Force:     input.Opt("force").(bool),
			KeepNodes: input.Opt("keep-nodes").(bool),
		}
		destroy.SetLogger(log)
		destroy.SetTerm(term)
		err := destroy.Execute()
		return destroy.Result(), err
	}))

	// node:join - Adopt an existing provider resource into a pool (no new infrastructure created)
	joinYaml, _ := actionYamlFS.ReadFile("actions/join/join.yaml")
	joinAct := action.NewFromYAML("node:join", joinYaml)
	joinAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		j := &join.Join{
			Keyring:     p.k,
			Platform:    input.Arg("platform").(string),
			ServerID:    input.Opt("server-id").(string),
			Pool:        input.Opt("pool").(string),
			Hostname:    input.Opt("hostname").(string),
			DryRun:      input.Opt("dry-run").(bool),
			AutoApprove: input.Opt("auto-approve").(bool),
		}
		j.SetLogger(log)
		j.SetTerm(term)
		err := j.Execute()
		return j.Result(), err
	}))

	// node:allocate - Allocate a node to one or more zones
	allocateYaml, _ := actionYamlFS.ReadFile("actions/allocate/allocate.yaml")
	allocateAct := action.NewFromYAML("node:allocate", allocateYaml)
	allocateAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		alloc := &allocate.Allocate{
			Hostname: input.Arg("hostname").(string),
			Zones:    action.InputArgSlice[string](input, "zones"),
			Platform: input.Opt("platform").(string),
		}
		alloc.SetLogger(log)
		alloc.SetTerm(term)
		err := alloc.Execute()
		return alloc.Result(), err
	}))

	// node:deallocate - Deallocate a node from one or more zones
	deallocateYaml, _ := actionYamlFS.ReadFile("actions/deallocate/deallocate.yaml")
	deallocateAct := action.NewFromYAML("node:deallocate", deallocateYaml)
	deallocateAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		dealloc := &deallocate.Deallocate{
			Hostname:  input.Arg("hostname").(string),
			Zones:     action.InputArgSlice[string](input, "zones"),
			Platform:  input.Opt("platform").(string),
			Recursive: input.Opt("recursive").(bool),
		}
		dealloc.SetLogger(log)
		dealloc.SetTerm(term)
		err := dealloc.Execute()
		return dealloc.Result(), err
	}))

	// node:query - Query nodes by zone, component, or package
	queryYaml, _ := actionYamlFS.ReadFile("actions/query/query.yaml")
	queryAct := action.NewFromYAML("node:query", queryYaml)
	queryAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		q := &query.Query{
			Identifier: input.Arg("identifier").(string),
			Platform:   input.Opt("platform").(string),
			Kind:       input.Opt("kind").(string),
		}
		q.SetLogger(log)
		q.SetTerm(term)
		err := q.Execute()
		return q.Result(), err
	}))

	// node:validate - Validate node configuration
	validateYaml, _ := actionYamlFS.ReadFile("actions/validate/validate.yaml")
	validateAct := action.NewFromYAML("node:validate", validateYaml)
	validateAct.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLogger(a)
		identifier, _ := input.Arg("identifier").(string)
		v := &validate.Validate{
			Identifier: identifier,
			Platform:   input.Opt("platform").(string),
		}
		v.SetLogger(log)
		v.SetTerm(term)
		err := v.Execute()
		return v.Result(), err
	}))

	return []*action.Action{
		addAct,
		provisionAct,
		listAct,
		showAct,
		destroyAct,
		joinAct,
		allocateAct,
		deallocateAct,
		queryAct,
		validateAct,
	}, nil
}

func getLogger(a *action.Action) (*launchr.Logger, *launchr.Terminal) {
	log := launchr.Log()
	if rt, ok := a.Runtime().(action.RuntimeLoggerAware); ok {
		log = rt.LogWith()
	}

	term := launchr.Term()
	if rt, ok := a.Runtime().(action.RuntimeTermAware); ok {
		term = rt.Term()
	}

	return log, term
}
