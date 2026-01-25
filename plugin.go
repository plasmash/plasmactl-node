// Package plasmactlnode implements a launchr plugin with node provisioning functionality
package plasmactlnode

import (
	"context"
	_ "embed"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/launchrctl/launchr/pkg/action"
)

//go:embed action.add.yaml
var actionAddYaml []byte

//go:embed action.provision.yaml
var actionProvisionYaml []byte

//go:embed action.list.yaml
var actionListYaml []byte

//go:embed action.show.yaml
var actionShowYaml []byte

//go:embed action.destroy.yaml
var actionDestroyYaml []byte

//go:embed action.node.yaml
var actionNodeYaml []byte

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
	addAct := action.NewFromYAML("node:add", actionAddYaml)
	addAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		name := input.Arg("name").(string)
		provider := input.Opt("provider").(string)
		domain := input.Opt("domain").(string)

		log, term := getLogger(a)
		add := &addAction{
			name:     name,
			provider: provider,
			domain:   domain,
		}
		add.SetLogger(log)
		add.SetTerm(term)
		return add.Execute()
	}))

	// node:provision - Provision infrastructure
	provisionAct := action.NewFromYAML("node:provision", actionProvisionYaml)
	provisionAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		name := input.Arg("name").(string)
		chassis := input.Opt("chassis").([]string)
		dryRun := input.Opt("dry-run").(bool)
		autoApprove := input.Opt("auto-approve").(bool)

		log, term := getLogger(a)
		provision := &provisionAction{
			keyring:     p.k,
			name:        name,
			chassisSpec: chassis,
			dryRun:      dryRun,
			autoApprove: autoApprove,
		}
		provision.SetLogger(log)
		provision.SetTerm(term)
		return provision.Execute()
	}))

	// node:list - List platforms and their nodes
	listAct := action.NewFromYAML("node:list", actionListYaml)
	listAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		log, term := getLogger(a)
		list := &listAction{}
		list.SetLogger(log)
		list.SetTerm(term)
		return list.Execute()
	}))

	// node:show - Show platform/node details
	showAct := action.NewFromYAML("node:show", actionShowYaml)
	showAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		name := input.Arg("name").(string)

		log, term := getLogger(a)
		show := &showAction{name: name}
		show.SetLogger(log)
		show.SetTerm(term)
		return show.Execute()
	}))

	// node:destroy - Destroy infrastructure
	destroyAct := action.NewFromYAML("node:destroy", actionDestroyYaml)
	destroyAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		name := input.Arg("name").(string)
		force := input.Opt("force").(bool)
		keepNodes := input.Opt("keep-nodes").(bool)

		log, term := getLogger(a)
		destroy := &destroyAction{
			keyring:   p.k,
			name:      name,
			force:     force,
			keepNodes: keepNodes,
		}
		destroy.SetLogger(log)
		destroy.SetTerm(term)
		return destroy.Execute()
	}))

	// node:register - Manually register a node
	nodeAct := action.NewFromYAML("node:register", actionNodeYaml)
	nodeAct.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		envName := input.Arg("name").(string)
		hostname := input.Opt("hostname").(string)
		publicIP := input.Opt("public-ip").(string)
		privateIP := input.Opt("private-ip").(string)
		chassis := input.Opt("chassis").([]string)

		log, term := getLogger(a)
		node := &nodeAction{
			envName:   envName,
			hostname:  hostname,
			publicIP:  publicIP,
			privateIP: privateIP,
			chassis:   chassis,
		}
		node.SetLogger(log)
		node.SetTerm(term)
		return node.Execute()
	}))

	return []*action.Action{
		addAct,
		provisionAct,
		listAct,
		showAct,
		destroyAct,
		nodeAct,
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
