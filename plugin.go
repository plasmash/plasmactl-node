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
	"github.com/plasmash/plasmactl-node/actions/destroy"
	"github.com/plasmash/plasmactl-node/actions/list"
	"github.com/plasmash/plasmactl-node/actions/provision"
	"github.com/plasmash/plasmactl-node/actions/register"
	"github.com/plasmash/plasmactl-node/actions/show"
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
	// node:add - Create platform scaffold with nodes directory
	addYaml, _ := actionYamlFS.ReadFile("actions/add/add.yaml")
	addAct := action.NewFromYAML("node:add", addYaml)
	addAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLogger(a)
		add := &add.Add{
			Name:     input.Arg("name").(string),
			Provider: input.Opt("provider").(string),
			Domain:   input.Opt("domain").(string),
		}
		add.SetLogger(log)
		add.SetTerm(term)
		return add.Execute()
	}))

	// node:provision - Provision infrastructure
	provisionYaml, _ := actionYamlFS.ReadFile("actions/provision/provision.yaml")
	provisionAct := action.NewFromYAML("node:provision", provisionYaml)
	provisionAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLogger(a)
		provision := &provision.Provision{
			Keyring:     p.k,
			Name:        input.Arg("name").(string),
			ChassisSpec: action.InputOptSlice[string](input, "chassis"),
			DryRun:      input.Opt("dry-run").(bool),
			AutoApprove: input.Opt("auto-approve").(bool),
		}
		provision.SetLogger(log)
		provision.SetTerm(term)
		return provision.Execute()
	}))

	// node:list - List platforms and their nodes
	listYaml, _ := actionYamlFS.ReadFile("actions/list/list.yaml")
	listAct := action.NewFromYAML("node:list", listYaml)
	listAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		log, term := getLogger(a)
		list := &list.List{}
		list.SetLogger(log)
		list.SetTerm(term)
		return list.Execute()
	}))

	// node:show - Show platform/node details
	showYaml, _ := actionYamlFS.ReadFile("actions/show/show.yaml")
	showAct := action.NewFromYAML("node:show", showYaml)
	showAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLogger(a)
		show := &show.Show{
			Name: input.Arg("name").(string),
		}
		show.SetLogger(log)
		show.SetTerm(term)
		return show.Execute()
	}))

	// node:destroy - Destroy infrastructure
	destroyYaml, _ := actionYamlFS.ReadFile("actions/destroy/destroy.yaml")
	destroyAct := action.NewFromYAML("node:destroy", destroyYaml)
	destroyAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
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
		return destroy.Execute()
	}))

	// node:register - Manually register a node
	registerYaml, _ := actionYamlFS.ReadFile("actions/register/register.yaml")
	registerAct := action.NewFromYAML("node:register", registerYaml)
	registerAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLogger(a)
		register := &register.Register{
			EnvName:   input.Arg("name").(string),
			Hostname:  input.Opt("hostname").(string),
			PublicIP:  input.Opt("public-ip").(string),
			PrivateIP: input.Opt("private-ip").(string),
			Chassis:   action.InputOptSlice[string](input, "chassis"),
		}
		register.SetLogger(log)
		register.SetTerm(term)
		return register.Execute()
	}))

	// node:allocate - Allocate node to chassis sections (kubectl-style)
	allocateYaml, _ := actionYamlFS.ReadFile("actions/allocate/allocate.yaml")
	allocateAct := action.NewFromYAML("node:allocate", allocateYaml)
	allocateAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLogger(a)
		allocate := &allocate.Allocate{
			Hostname:   input.Arg("hostname").(string),
			Operations: action.InputArgSlice[string](input, "operations"),
			Env:        input.Opt("env").(string),
		}
		allocate.SetLogger(log)
		allocate.SetTerm(term)
		return allocate.Execute()
	}))

	return []*action.Action{
		addAct,
		provisionAct,
		listAct,
		showAct,
		destroyAct,
		registerAct,
		allocateAct,
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
